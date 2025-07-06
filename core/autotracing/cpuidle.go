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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"huatuo-bamai/internal/conf"
	"huatuo-bamai/internal/flamegraph"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/pod"
	"huatuo-bamai/internal/storage"
	"huatuo-bamai/internal/utils/cgrouputil"
	"huatuo-bamai/pkg/tracing"
	"huatuo-bamai/pkg/types"
)

func init() {
	tracing.RegisterEventTracing("cpuidle", newCPUIdle)
}

func newCPUIdle() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &cpuIdleTracing{},
		Internal:    20,
		Flag:        tracing.FlagTracing,
	}, nil
}

// GetCPUCoresInCgroup function returns the number of cgroup cores
func GetCPUCoresInCgroup(cgroupPath string) (uint64, error) {
	periodPath := cgroupPath + "/cpu.cfs_period_us"
	quotaPath := cgroupPath + "/cpu.cfs_quota_us"

	period, err := readIntFromFile(periodPath)
	if err != nil {
		return 0, err
	}

	quota, err := readIntFromFile(quotaPath)
	if err != nil {
		return 0, err
	}

	if quota == -1 {
		return uint64(runtime.NumCPU()), nil
	}

	if period == 0 {
		return 0, fmt.Errorf("period not zero")
	}

	return uint64(quota / period), nil
}

func readIntFromFile(filePath string) (int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}

	str := strings.TrimSpace(string(data))
	value, err := strconv.Atoi(str)
	if err != nil {
		return 0, err
	}

	return value, nil
}

func readCPUUsage(path string) (map[string]uint64, error) {
	cpuacctPath := path + "/cpuacct.stat"
	output, err := os.ReadFile(cpuacctPath)
	if err != nil {
		return nil, err
	}

	cpuUsage := make(map[string]uint64)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		valueStr := parts[1]
		value, err := strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			return nil, err
		}

		cpuUsage[key] = value
	}
	cpuUsage["total"] = uint64(time.Now().UnixNano())
	return cpuUsage, nil
}

// UserHZtons because kernel USER_HZ = 100, the default value set to 10,000,000
const (
	UserHZtons = 10000000
	USERHZ     = 100
)

func calculateCPUUsage(info *containerCPUInfo, currUsage map[string]uint64) error {
	deltaTotal := currUsage["total"] - info.prevUsage["total"]
	deltaUser := currUsage["user"] - info.prevUsage["user"]
	deltaSys := currUsage["system"] - info.prevUsage["system"]

	cpuCores, err := GetCPUCoresInCgroup(info.path)
	if err != nil {
		return fmt.Errorf("get cgroup cpu err")
	}

	if cpuCores == 0 || deltaTotal == 0 {
		return fmt.Errorf("division by zero error")
	}

	log.Debugf("cpuidle calculate core %v currUsage %v prevUsage %v", cpuCores, currUsage, info.prevUsage)
	info.nowUsageP["cpuUser"] = deltaUser * UserHZtons * USERHZ / deltaTotal / cpuCores
	info.nowUsageP["cpuSys"] = deltaSys * UserHZtons * USERHZ / deltaTotal / cpuCores
	return nil
}

type containerCPUInfo struct {
	prevUsage  map[string]uint64
	prevUsageP map[string]uint64
	nowUsageP  map[string]uint64
	deltaUser  int64
	deltaSys   int64
	timestamp  int64
	path       string
	alive      bool
}

// cpuIdleIDMap is the container information
type cpuIdleIDMap map[string]*containerCPUInfo

func updateCPUIdleIDMap(m cpuIdleIDMap) error {
	containers, err := pod.GetNormalContainers()
	if err != nil {
		return fmt.Errorf("GetNormalContainers: %w", err)
	}

	for _, container := range containers {
		_, ok := m[container.ID]
		if ok {
			m[container.ID].path = filepath.Join(cgrouputil.V1CpuPath(), container.CgroupSuffix)
			m[container.ID].alive = true
		} else {
			temp := &containerCPUInfo{
				prevUsage: map[string]uint64{
					"user":   0,
					"system": 0,
					"total":  0,
				},
				prevUsageP: map[string]uint64{
					"cpuUser": 0,
					"cpuSys":  0,
				},
				nowUsageP: map[string]uint64{
					"cpuUser": 0,
					"cpuSys":  0,
				},
				deltaUser: 0,
				deltaSys:  0,
				timestamp: 0,
				path:      filepath.Join(cgrouputil.V1CpuPath(), container.CgroupSuffix),
				alive:     true,
			}
			m[container.ID] = temp
		}
	}
	return nil
}

var cpuIdleIdMap = make(cpuIdleIDMap)

func cpuIdleDetect(ctx context.Context) (string, error) {
	// get config info
	userth := conf.Get().Tracing.Cpuidle.CgUserth
	deltauserth := conf.Get().Tracing.Cpuidle.CgDeltaUserth
	systh := conf.Get().Tracing.Cpuidle.CgSysth
	deltasysth := conf.Get().Tracing.Cpuidle.CgDeltaSysth
	usageth := conf.Get().Tracing.Cpuidle.CgUsageth
	deltausageth := conf.Get().Tracing.Cpuidle.CgDeltaUsageth
	step := conf.Get().Tracing.Cpuidle.CgStep
	graceth := conf.Get().Tracing.Cpuidle.CgGrace

	for {
		select {
		case <-ctx.Done():
			return "", types.ErrExitByCancelCtx
		case <-time.After(time.Duration(step) * time.Second):
			if err := updateCPUIdleIDMap(cpuIdleIdMap); err != nil {
				return "", err
			}
			for containerID, v := range cpuIdleIdMap {
				if !v.alive {
					delete(cpuIdleIdMap, containerID)
				} else {
					v.alive = false
					currUsage, err := readCPUUsage(v.path)
					if err != nil {
						log.Debugf("cpuidle failed to read %s CPU usage: %s", v.path, err)
						continue
					}

					if v.prevUsage["user"] == 0 && v.prevUsage["system"] == 0 && v.prevUsage["total"] == 0 {
						v.prevUsage = currUsage
						continue
					}

					err = calculateCPUUsage(v, currUsage)
					if err != nil {
						log.Debugf("cpuidle calculate err %s", err)
						continue
					}

					v.deltaUser = int64(v.nowUsageP["cpuUser"] - v.prevUsageP["cpuUser"])
					v.deltaSys = int64(v.nowUsageP["cpuSys"] - v.prevUsageP["cpuSys"])
					v.prevUsageP["cpuUser"] = v.nowUsageP["cpuUser"]
					v.prevUsageP["cpuSys"] = v.nowUsageP["cpuSys"]
					v.prevUsage = currUsage
					nowtime := time.Now().Unix()
					gracetime := nowtime - v.timestamp
					nowUsage := v.nowUsageP["cpuUser"] + v.nowUsageP["cpuSys"]
					nowDeltaUsage := v.deltaUser + v.deltaSys

					log.Debugf("cpuidle ctID %v user %v deltauser %v sys %v deltasys %v usage %v deltausage %v grace %v graceth %v",
						containerID, v.nowUsageP["cpuUser"], v.deltaUser, v.nowUsageP["cpuSys"], v.deltaSys, nowUsage, nowDeltaUsage, gracetime, graceth)

					if gracetime > graceth {
						if (v.nowUsageP["cpuUser"] > userth && v.deltaUser > deltauserth) ||
							(v.nowUsageP["cpuSys"] > systh && v.deltaSys > deltasysth) ||
							(nowUsage > usageth && nowDeltaUsage > deltausageth) {
							v.timestamp = nowtime
							for key := range v.prevUsage {
								v.prevUsage[key] = 0
							}
							return containerID, nil
						}
					}
				}
			}
		}
	}
}

type cpuIdleTracing struct{}

// Cpuidle is an instance of cpuIdleTracer
var (
	tracerTime time.Time
)

type CPUIdleTracingData struct {
	NowUser        uint64                 `json:"nowuser"`
	UserThreshold  uint64                 `json:"userthreshold"`
	DeltaUser      int64                  `json:"deltauser"`
	DeltaUserTH    int64                  `json:"deltauserth"`
	NowSys         uint64                 `json:"nowsys"`
	SysThreshold   uint64                 `json:"systhreshold"`
	DeltaSys       int64                  `json:"deltasys"`
	DeltaSysTH     int64                  `json:"deltasysth"`
	NowUsage       uint64                 `json:"nowusage"`
	UsageThreshold uint64                 `json:"usagethreshold"`
	DeltaUsage     int64                  `json:"deltausage"`
	DeltaUsageTH   int64                  `json:"deltausageth"`
	FlameData      []flamegraph.FrameData `json:"flamedata"`
}

// Start detect work, load bpf and wait data form perfevent
func (c *cpuIdleTracing) Start(ctx context.Context) error {
	// TODO: Verify the conditions for startup.
	containerID, err := cpuIdleDetect(ctx)
	if err != nil {
		return err
	}

	tracerTime = time.Now()
	dur := conf.Get().Tracing.Cpuidle.CgUsageToolduration
	durstr := strconv.FormatInt(dur, 10)

	// exec tracerperf
	cmdctx, cancel := context.WithTimeout(ctx, time.Duration(dur+30)*time.Second)
	defer cancel()

	log.Infof("cpuidle exec tracerperf ctid %v dur %v", containerID, durstr)
	cmd := exec.CommandContext(cmdctx, "./tracer/perf.bin", "--casename", "cpuidle.o", "--container-id", containerID, "--dur", durstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("cpuidle cmd output %v", strings.TrimSuffix(string(output), "\n"))
		return fmt.Errorf("cpuidle tracerperf exec err: %w", err)
	}

	// parse json
	log.Infof("cpuidle parse json")
	tracerData := CPUIdleTracingData{}
	err = json.Unmarshal(output, &tracerData.FlameData)
	if err != nil {
		return fmt.Errorf("parse JSON err: %w", err)
	}

	// save
	log.Infof("cpuidle upload ES")
	log.Debugf("cpuidle FlameData %v", tracerData.FlameData)
	tracerData.NowUser = cpuIdleIdMap[containerID].nowUsageP["cpuUser"]
	tracerData.UserThreshold = conf.Get().Tracing.Cpuidle.CgUserth
	tracerData.DeltaUser = cpuIdleIdMap[containerID].deltaUser
	tracerData.DeltaUserTH = conf.Get().Tracing.Cpuidle.CgDeltaUserth
	tracerData.NowSys = cpuIdleIdMap[containerID].nowUsageP["cpuSys"]
	tracerData.SysThreshold = conf.Get().Tracing.Cpuidle.CgSysth
	tracerData.DeltaSys = cpuIdleIdMap[containerID].deltaSys
	tracerData.DeltaSysTH = conf.Get().Tracing.Cpuidle.CgDeltaSysth
	tracerData.NowUsage = cpuIdleIdMap[containerID].nowUsageP["cpuSys"] + cpuIdleIdMap[containerID].nowUsageP["cpuUser"]
	tracerData.UsageThreshold = conf.Get().Tracing.Cpuidle.CgUsageth
	tracerData.DeltaUsage = cpuIdleIdMap[containerID].deltaUser + cpuIdleIdMap[containerID].deltaSys
	tracerData.DeltaUsageTH = conf.Get().Tracing.Cpuidle.CgDeltaUsageth
	storage.Save("cpuidle", containerID, tracerTime, &tracerData)
	log.Infof("cpuidle upload ES end")
	return err
}
