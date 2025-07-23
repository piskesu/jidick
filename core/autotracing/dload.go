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

package autotracing

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"huatuo-bamai/internal/cgroups/paths"
	"huatuo-bamai/internal/conf"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/pod"
	"huatuo-bamai/internal/storage"
	"huatuo-bamai/pkg/tracing"
	"huatuo-bamai/pkg/types"

	"github.com/google/cadvisor/utils/cpuload/netlink"
	"github.com/prometheus/procfs"
	"github.com/shirou/gopsutil/process"
)

func init() {
	tracing.RegisterEventTracing("dload", newDload)
}

func newDload() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &dloadTracing{},
		Internal:    30,
		Flag:        tracing.FlagTracing,
	}, nil
}

type containerDloadInfo struct {
	path      string
	name      string
	container *pod.Container
	avgnrun   [2]uint64
	load      [2]float64
	avgnuni   [2]uint64
	loaduni   [2]float64
	alive     bool
}

type DloadTracingData struct {
	Avg               float64 `json:"avg"`
	Threshold         float64 `json:"threshold"`
	NrSleeping        uint64  `json:"nr_sleeping"`
	NrRunning         uint64  `json:"nr_running"`
	NrStopped         uint64  `json:"nr_stopped"`
	NrUninterruptible uint64  `json:"nr_uninterruptible"`
	NrIoWait          uint64  `json:"nr_iowait"`
	LoadAvg           float64 `json:"load_avg"`
	DLoadAvg          float64 `json:"dload_avg"`
	KnowIssue         string  `json:"known_issue"`
	InKnownList       uint64  `json:"in_known_list"`
	Stack             string  `json:"stack"`
}

func getStack(targetPid int32) (string, error) {
	procStack := "/proc/" + strconv.Itoa(int(targetPid)) + "/stack"
	content, err := os.ReadFile(procStack)
	if err != nil {
		log.Infof("%v", err)
		return "", err
	}

	return string(content), nil
}

const (
	isHost = 1
	isCgrp = 2
)

func getUnTaskList(cgrpPath string, infoType int) ([]int32, error) {
	var pidList []int32
	var err error

	if infoType == isCgrp {
		taskPath := cgrpPath + "/tasks"

		tskfi, err := os.Open(taskPath)
		if err != nil {
			log.Infof("%v", err)
			return nil, err
		}

		r := bufio.NewReader(tskfi)

		for {
			lineBytes, err := r.ReadBytes('\n')
			line := strings.TrimSpace(string(lineBytes))
			if err != nil && err != io.EOF {
				log.Infof("fail to read tasklist: %v", err)
				break
			}
			if err == io.EOF {
				break
			}

			pid, _ := strconv.ParseInt(line, 10, 32)
			pidList = append(pidList, int32(pid))
		}
	} else {
		procs, err := procfs.AllProcs()
		if err != nil {
			log.Infof("%v", err)
			return nil, err
		}

		for _, p := range procs {
			pidList = append(pidList, int32(p.PID))
		}
	}

	return pidList, err
}

func dumpUnTaskStack(tskList []int32, dumpType int) (string, error) {
	var infoTitle string
	var getValidStackinfo bool = false
	var strResult string = ""

	stackInfo := new(bytes.Buffer)

	switch dumpType {
	case isHost:
		infoTitle = "\nbacktrace of D process in Host:\n"
	case isCgrp:
		infoTitle = "\nbacktrace of D process in Cgroup:\n"
	}

	for _, pid := range tskList {
		proc, err := process.NewProcess(pid)
		if err != nil {
			log.Debugf("fail to get process %d: %v", pid, err)
			continue
		}

		status, err := proc.Status()
		if err != nil {
			log.Debugf("fail to get status %d: %v", pid, err)
			continue
		}

		if status == "D" || status == "U" {
			comm, err := proc.Name()
			if err != nil {
				log.Infof("%v", err)
				continue
			}
			stack, err := getStack(pid)
			if err != nil {
				log.Infof("%v", err)
				continue
			}
			if stack == "" {
				continue
			}

			fmt.Fprintf(stackInfo, "Comm: %s\tPid: %d\n%s\n", comm, pid, stack)
			getValidStackinfo = true
		}
	}

	if getValidStackinfo {
		strResult = fmt.Sprintf("%s%s", infoTitle, stackInfo)
	}

	return strResult, nil
}

// dloadIDMap is the container information
type dloadIDMap map[string]*containerDloadInfo

var dloadIdMap = make(dloadIDMap)

func updateIDMap(m dloadIDMap) error {
	containers, err := pod.GetAllContainers()
	if err != nil {
		return fmt.Errorf("GetAllContainers: %w", err)
	}

	for _, container := range containers {
		if _, ok := m[container.ID]; ok {
			m[container.ID].name = container.CgroupSuffix
			m[container.ID].path = paths.Path("cpu", container.CgroupSuffix)
			m[container.ID].container = container
			m[container.ID].alive = true
			continue
		}

		m[container.ID] = &containerDloadInfo{
			path:      paths.Path("cpu", container.CgroupSuffix),
			name:      container.CgroupSuffix,
			container: container,
			alive:     true,
		}
	}

	return nil
}

const (
	fShift = 11
	fixed1 = 1 << fShift
	exp1   = 1884
	exp5   = 2014
	exp15  = 2037
)

func calcLoad(load, exp, active uint64) uint64 {
	var newload uint64

	newload = load*exp + active*(fixed1-exp)
	newload += 1 << (fShift - 1)

	return newload / fixed1
}

func calcLoadavg(avgnrun [2]uint64, active uint64) (avgnresult [2]uint64) {
	if active > 0 {
		active *= fixed1
	} else {
		active = 0
	}

	avgnresult[0] = calcLoad(avgnrun[0], exp1, active)
	avgnresult[1] = calcLoad(avgnrun[1], exp5, active)

	return avgnresult
}

func loadInt(x uint64) (r uint64) {
	r = x >> fShift
	return r
}

func loadFrac(x uint64) (r uint64) {
	r = loadInt((x & (fixed1 - 1)) * 100)
	return r
}

func getAvenrun(avgnrun [2]uint64, offset uint64, shift int) (loadavgNew [2]float64) {
	var loads [2]uint64

	loads[0] = (avgnrun[0] + offset) << shift
	loads[1] = (avgnrun[1] + offset) << shift

	loadavgNew[0] = float64(loadInt(loads[0])) +
		float64(loadFrac(loads[0]))/float64(100)

	loadavgNew[1] = float64(loadInt(loads[1])) +
		float64(loadFrac(loads[1]))/float64(100)

	return loadavgNew
}

func updateLoad(info *containerDloadInfo, nrRunning, nrUninterruptible uint64) {
	info.avgnrun = calcLoadavg(info.avgnrun, nrRunning+nrUninterruptible)
	info.load = getAvenrun(info.avgnrun, fixed1/200, 0)
	info.avgnuni = calcLoadavg(info.avgnuni, nrUninterruptible)
	info.loaduni = getAvenrun(info.avgnuni, fixed1/200, 0)
}

func detect(ctx context.Context) (*containerDloadInfo, string, *DloadTracingData, error) {
	var caseData DloadTracingData

	n, err := netlink.New()
	if err != nil {
		log.Infof("Failed to create cpu load util: %s", err)
		return nil, "", nil, err
	}
	defer n.Stop()

	dloadThresh := conf.Get().Tracing.Dload.ThresholdLoad
	monitorGap := conf.Get().Tracing.Dload.MonitorGap

	for {
		select {
		case <-ctx.Done():
			return nil, "", nil, types.ErrExitByCancelCtx
		default:
			if err := updateIDMap(dloadIdMap); err != nil {
				return nil, "", nil, err
			}
			for k, v := range dloadIdMap {
				if !v.alive {
					delete(dloadIdMap, k)
				} else {
					v.alive = false

					timeStartMonitor := v.container.StartedAt.Add(time.Second * time.Duration(monitorGap))

					if time.Now().Before(timeStartMonitor) {
						log.Debugf("%s were just started, we'll start monitoring it later.", v.container.Hostname)
						continue
					}

					stats, err := n.GetCpuLoad(v.name, v.path)
					if err != nil {
						log.Debugf("failed to get %s load, probably the container has been deleted: %s", v.container.Hostname, err)
						continue
					}

					updateLoad(v, stats.NrRunning, stats.NrUninterruptible)

					if v.loaduni[0] > dloadThresh {
						logTitle := fmt.Sprintf("Avg=%0.2f Threshold=%0.2f %+v ", v.loaduni[0], dloadThresh, stats)
						logBody := fmt.Sprintf("LoadAvg=%0.2f, DLoadAvg=%0.2f", v.load[0], v.loaduni[0])
						logLoad := fmt.Sprintf("%s%s", logTitle, logBody)

						log.Infof("dload event %s", logLoad)

						caseData.Avg = v.loaduni[0]
						caseData.Threshold = dloadThresh
						caseData.NrSleeping = stats.NrSleeping
						caseData.NrRunning = stats.NrRunning
						caseData.NrStopped = stats.NrStopped
						caseData.NrUninterruptible = stats.NrUninterruptible
						caseData.NrIoWait = stats.NrIoWait
						caseData.LoadAvg = v.load[0]
						caseData.DLoadAvg = v.loaduni[0]

						return v, logLoad, &caseData, err
					}
				}
			}
			time.Sleep(5 * time.Second)
		}
	}
}

func dumpInfo(info *containerDloadInfo, logLoad string, caseData *DloadTracingData) error {
	var tskList []int32
	var err error
	var stackCgrp string
	var stackHost string
	var containerHostNamespace string

	cgrpPath := info.path
	containerID := info.container.ID
	containerHostNamespace = info.container.LabelHostNamespace()

	tskList, err = getUnTaskList(cgrpPath, isCgrp)
	if err != nil {
		return fmt.Errorf("failed to get cgroup task list: %w", err)
	}

	stackCgrp, err = dumpUnTaskStack(tskList, isCgrp)
	if err != nil {
		return fmt.Errorf("failed to dump cgroup task backtrace: %w", err)
	}

	tskList, err = getUnTaskList("", isHost)
	if err != nil {
		return fmt.Errorf("failed to get host task list: %w", err)
	}

	stackHost, err = dumpUnTaskStack(tskList, isHost)
	if err != nil {
		return fmt.Errorf("failed to dump host task backtrace: %w", err)
	}

	// We'll not record it if got no cgroup stack info.
	if stackCgrp == "" {
		return nil
	}

	// Check if this is caused by known issues.
	knownIssue, inKnownList := conf.KnownIssueSearch(stackCgrp, containerHostNamespace, "")
	if knownIssue != "" {
		caseData.KnowIssue = knownIssue
		caseData.InKnownList = inKnownList
	} else {
		caseData.KnowIssue = "none"
		caseData.InKnownList = inKnownList
	}

	caseData.Stack = fmt.Sprintf("%s%s", stackCgrp, stackHost)
	storage.Save("dload", containerID, time.Now(), caseData)

	return nil
}

type dloadTracing struct{}

// Start detect work, monitor the load of containers
func (c *dloadTracing) Start(ctx context.Context) error {
	cntInfo, logLoad, caseData, err := detect(ctx)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		log.Infof("caller requests stop !!!")
	default:
		err = dumpInfo(cntInfo, logLoad, caseData)
		if err != nil {
			return fmt.Errorf("failed to dump info: %w", err)
		}
	}

	return err
}
