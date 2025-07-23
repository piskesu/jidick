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

//go:build !didi

package pod

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"huatuo-bamai/internal/bpf"
	"huatuo-bamai/internal/cgroups"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/pkg/types"

	mapset "github.com/deckarep/golang-set"
)

// XXX go:generate go run -mod=mod github.com/cilium/ebpf/cmd/bpf2go -target amd64 cgroupCssGather $BPF_DIR/cgroup_css_gather.c -- $BPF_INCLUDE
// use the huatuo bpf framework:
//
//go:generate $BPF_COMPILE $BPF_INCLUDE -s $BPF_DIR/cgroup_css_gather.c -o $BPF_DIR/cgroup_css_gather.o
//go:generate $BPF_COMPILE $BPF_INCLUDE -s $BPF_DIR/cgroup_css_events.c -o $BPF_DIR/cgroup_css_events.o

func parseContainerCSS(containerID string) (map[string]uint64, error) {
	msg := make(map[string]uint64)
	cssList := cgroupListCssDataByKnode(containerID)
	for _, css := range cssList {
		msg[css.SubSys] = css.CSS
	}

	return msg, nil
}

const (
	cgroupSubsysCount             = 13
	kubeletContainerIDKnodeMaxlen = 64
)

var (
	// FIXME:
	// 1. cgroupv supported only
	// 2. cgroup dir name is containerID
	kubeletContainerIDRegexp  = regexp.MustCompile(`[^a-zA-Z0-9]+`)
	cgroupv1SubSysName        = []string{"cpu", "cpuacct", "cpuset", "memory", "blkio"}
	cgroupv1NotifyCgroupFile  = "cgroup.clone_children"
	cgroupCssID2SubSysNameMap = map[int]string{}
	cgroupCssMetaDataMap      sync.Map

	// avoid GC
	_cgroupCssBpfInternal *bpf.BPF
)

func isValidKnodeName(name string) bool {
	return !kubeletContainerIDRegexp.MatchString(name)
}

type containerCssMetaData struct {
	CSS         uint64
	SubSys      string
	Cgroup      uint64
	CgroupRoot  int32
	CgroupLevel int32
	ContainerID string
}

type containerCssPerfEvent struct {
	Cgroup      uint64
	OpsType     uint64
	CgroupRoot  int32
	CgroupLevel int32
	CSS         [cgroupSubsysCount]uint64
	KnodeName   [kubeletContainerIDKnodeMaxlen + 2]byte
}

func cgroupListCssDataByKnode(containerID string) []*containerCssMetaData {
	res := []*containerCssMetaData{}
	cgroupCssMetaDataMap.Range(func(k, v any) bool {
		if m, ok := v.(*containerCssMetaData); ok {
			if m.ContainerID == containerID {
				res = append(res, m)
			}
		}
		return true
	})
	return res
}

func cgroupUpdateOrCreateCssData(data *containerCssPerfEvent) error {
	knodeName := strings.TrimRight(string(data.KnodeName[:]), "\x00")
	if !isValidKnodeName(knodeName) {
		return fmt.Errorf("knode name is not containterID")
	}

	for index, css := range data.CSS {
		if css == 0 {
			continue
		}

		if sysName, ok := cgroupCssID2SubSysNameMap[index]; ok {
			m := &containerCssMetaData{
				CSS:         css,
				Cgroup:      data.Cgroup,
				CgroupRoot:  data.CgroupRoot,
				CgroupLevel: data.CgroupLevel,
				ContainerID: knodeName,
				SubSys:      sysName,
			}
			log.Debugf("update container css data: %+v", m)
			cgroupCssMetaDataMap.Store(css, m)
		}
	}

	return nil
}

func cgroupDeleteCssData(data *containerCssPerfEvent) error {
	knodeName := strings.TrimRight(string(data.KnodeName[:]), "\x00")
	if !isValidKnodeName(knodeName) {
		return fmt.Errorf("knode name is not containterID")
	}

	for index, css := range data.CSS {
		if css == 0 {
			continue
		}

		if _, ok := cgroupCssID2SubSysNameMap[index]; ok {
			m, loaded := cgroupCssMetaDataMap.LoadAndDelete(css)
			if loaded {
				log.Debugf("delete container css data: %+v", m)
			}
		}
	}

	return nil
}

func cgroupCssEventSync(ctx context.Context, reader bpf.PerfEventReader) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				var data containerCssPerfEvent
				if err := reader.ReadInto(&data); err != nil {
					if !errors.Is(err, types.ErrExitByCancelCtx) {
						log.Errorf("cgroup css sync read events: %v", err)
					}
					return
				}

				log.Debugf("sync container css data: %+v", data)

				switch data.OpsType {
				case 0: // mkdir cgroup
					_ = cgroupUpdateOrCreateCssData(&data)
				case 1: // rmdir cgroup
					_ = cgroupDeleteCssData(&data)
				default:
					log.Errorf("css event opstype not supported: %+v", data)
				}
			}
		}
	}()
}

func cgroupCssNotify() {
	rootSet := mapset.NewSet()

	for _, subsys := range cgroupv1SubSysName {
		root := cgroups.RootFsFilePath(subsys)
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}

		if rootSet.Contains(realRoot) {
			continue
		}

		rootSet.Add(realRoot)

		if err := filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if !d.IsDir() || len(d.Name()) != kubeletContainerIDKnodeMaxlen {
				return nil
			}

			notifyPath := filepath.Join(path, cgroupv1NotifyCgroupFile)
			_, _ = os.ReadFile(notifyPath)

			log.Debugf("read cgroup path: %s", notifyPath)
			return filepath.SkipDir
		}); err != nil {
			var e *os.PathError
			if errors.As(err, &e) && errors.Is(e.Err, syscall.ENOENT) {
				continue
			}

			return
		}
	}
}

func cgroupInitSubSysIDs() error {
	file, err := os.Open("/proc/cgroups")
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	// skip frst head
	scanner.Scan()

	ssid := 0
	for scanner.Scan() {
		arr := strings.SplitN(scanner.Text(), "\t", 2)
		cgroupCssID2SubSysNameMap[ssid] = arr[0]
		ssid++
	}

	return nil
}

func cgroupInitEventCssWithoutCleanup() error {
	cssBpf, err := bpf.LoadBpf("cgroup_css_events.o", nil)
	if err != nil {
		return fmt.Errorf("LoadBpf: %w", err)
	}
	_cgroupCssBpfInternal = &cssBpf

	childCtx := context.Background()
	reader, err := cssBpf.AttachAndEventPipe(childCtx, "cgroup_perf_events", 8192)
	if err != nil {
		log.Infof("AttachAndEventPipe: %v", err)
		return err
	}

	cgroupCssEventSync(childCtx, reader)
	return nil
}

func cgroupInitGatherCss() error {
	cssBpf, err := bpf.LoadBpf("cgroup_css_gather.o", nil)
	if err != nil {
		return fmt.Errorf("LoadBpf: %w", err)
	}
	defer cssBpf.Close()

	childCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader, err := cssBpf.AttachAndEventPipe(childCtx, "cgroup_perf_events", 8192)
	if err != nil {
		log.Infof("AttachAndEventPipe: %v", err)
		return err
	}
	defer reader.Close()

	cgroupCssEventSync(childCtx, reader)
	time.Sleep(100 * time.Millisecond)

	cgroupCssNotify()

	// wait sync
	time.Sleep(1 * time.Second)
	return nil
}

func ContainerCgroupCssInit() error {
	if err := cgroupInitSubSysIDs(); err != nil {
		panic("only support cgroupv1 now")
	}

	if err := cgroupInitGatherCss(); err != nil {
		return err
	}
	if err := cgroupInitEventCssWithoutCleanup(); err != nil {
		return err
	}

	return nil
}
