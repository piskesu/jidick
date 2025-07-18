// Copyright 2025 The HuaTuo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package events

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"huatuo-bamai/internal/conf"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/storage"
	"huatuo-bamai/pkg/metric"
	"huatuo-bamai/pkg/tracing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type linkStatusType uint8

const (
	linkStatusUnknown linkStatusType = iota
	linkStatusAdminUp
	linkStatusAdminDown
	linkStatusCarrierUp
	linkStatusCarrierDown
	maxLinkStatus
)

func (l linkStatusType) String() string {
	return [...]string{"linkstatus_unknown", "linkstatus_adminup", "linkstatus_admindown", "linkstatus_carrierup", "linkstatus_carrierdown"}[l]
}

func flags2status(flags, change uint32) []linkStatusType {
	var status []linkStatusType

	if change&unix.IFF_UP != 0 {
		if flags&unix.IFF_UP != 0 {
			status = append(status, linkStatusAdminUp)
		} else {
			status = append(status, linkStatusAdminDown)
		}
	}

	if change&unix.IFF_LOWER_UP != 0 {
		if flags&unix.IFF_LOWER_UP != 0 {
			status = append(status, linkStatusCarrierUp)
		} else {
			status = append(status, linkStatusCarrierDown)
		}
	}

	return status
}

type netdevTracing struct {
	name                      string
	linkUpdateCh              chan netlink.LinkUpdate
	linkDoneCh                chan struct{}
	mu                        sync.Mutex
	ifFlagsMap                map[string]uint32                 // [ifname]ifinfomsg::if_flags
	metricsLinkStatusCountMap map[linkStatusType]map[string]int // [netdevEventType][ifname]count
}

type netdevEventData struct {
	linkFlags   uint32
	flagsChange uint32
	Ifname      string `json:"ifname"`
	Index       int    `json:"index"`
	LinkStatus  string `json:"linkstatus"`
	Mac         string `json:"mac"`
	AtStart     bool   `json:"start"` // true: be scanned at start, false: event trigger
}

func init() {
	tracing.RegisterEventTracing("netdev_events", newNetdevTracing)
}

func newNetdevTracing() (*tracing.EventTracingAttr, error) {
	initMap := make(map[linkStatusType]map[string]int)
	for i := linkStatusUnknown; i < maxLinkStatus; i++ {
		initMap[i] = make(map[string]int)
	}

	return &tracing.EventTracingAttr{
		TracingData: &netdevTracing{
			linkUpdateCh:              make(chan netlink.LinkUpdate),
			linkDoneCh:                make(chan struct{}),
			ifFlagsMap:                make(map[string]uint32),
			metricsLinkStatusCountMap: initMap,
			name:                      "netdev_events",
		},
		Internal: 10,
		Flag:     tracing.FlagTracing | tracing.FlagMetric,
	}, nil
}

func (nt *netdevTracing) Start(ctx context.Context) (err error) {
	if err := nt.checkLinkStatus(); err != nil {
		return err
	}

	if err := netlink.LinkSubscribe(nt.linkUpdateCh, nt.linkDoneCh); err != nil {
		return err
	}
	defer nt.close()

	for {
		update, ok := <-nt.linkUpdateCh
		if !ok {
			return nil
		}
		switch update.Header.Type {
		case unix.NLMSG_ERROR:
			return fmt.Errorf("NLMSG_ERROR")
		case unix.RTM_NEWLINK:
			ifname := update.Link.Attrs().Name
			if _, ok := nt.ifFlagsMap[ifname]; !ok {
				// new interface
				continue
			}
			nt.handleEvent(&update)
		}
	}
}

// Update implement Collector
func (nt *netdevTracing) Update() ([]*metric.Data, error) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	var metrics []*metric.Data

	for typ, value := range nt.metricsLinkStatusCountMap {
		for ifname, count := range value {
			metrics = append(metrics, metric.NewGaugeData(
				typ.String(), float64(count), typ.String(), map[string]string{"device": ifname}))
		}
	}

	return metrics, nil
}

func (nt *netdevTracing) checkLinkStatus() error {
	links, err := netlink.LinkList()
	if err != nil {
		return err
	}

	for _, link := range links {
		ifname := link.Attrs().Name
		if !slices.Contains(conf.Get().Tracing.Netdev.Whitelist,
			ifname) {
			continue
		}

		flags := link.Attrs().RawFlags
		nt.ifFlagsMap[ifname] = flags

		data := &netdevEventData{
			linkFlags: flags,
			Ifname:    ifname,
			Index:     link.Attrs().Index,
			Mac:       link.Attrs().HardwareAddr.String(),
			AtStart:   true,
		}
		nt.record(data)
	}

	return nil
}

func (nt *netdevTracing) record(data *netdevEventData) {
	for _, status := range flags2status(data.linkFlags, data.flagsChange) {
		nt.mu.Lock()
		nt.metricsLinkStatusCountMap[status][data.Ifname]++
		nt.mu.Unlock()

		if data.LinkStatus == "" {
			data.LinkStatus = status.String()
		} else {
			data.LinkStatus = data.LinkStatus + ", " + status.String()
		}
	}

	if !data.AtStart && data.LinkStatus != "" {
		log.Infof("%s %+v", data.LinkStatus, data)
		storage.Save(nt.name, "", time.Now(), data)
	}
}

func (nt *netdevTracing) handleEvent(ev *netlink.LinkUpdate) {
	ifname := ev.Link.Attrs().Name

	currFlags := ev.Attrs().RawFlags
	lastFlags := nt.ifFlagsMap[ifname]
	change := currFlags ^ lastFlags
	nt.ifFlagsMap[ifname] = currFlags

	data := &netdevEventData{
		linkFlags:   currFlags,
		flagsChange: change,
		Ifname:      ifname,
		Index:       ev.Link.Attrs().Index,
		Mac:         ev.Link.Attrs().HardwareAddr.String(),
		AtStart:     false,
	}
	nt.record(data)
}

func (nt *netdevTracing) close() {
	close(nt.linkDoneCh)
	close(nt.linkUpdateCh)
}
