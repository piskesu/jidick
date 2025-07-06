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
	"strconv"
	"strings"
	"time"

	"huatuo-bamai/internal/conf"
	"huatuo-bamai/internal/flamegraph"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/storage"
	"huatuo-bamai/pkg/tracing"
	"huatuo-bamai/pkg/types"
)

func init() {
	tracing.RegisterEventTracing("cpusys", newCpuSys)
}

func newCpuSys() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &cpuSysTracing{},
		Internal:    20,
		Flag:        tracing.FlagTracing,
	}, nil
}

// CPUStats structure that records cpu usage
type CPUStats struct {
	system uint64
	total  uint64
}

func CpuSysDetect(ctx context.Context) (uint64, int64, error) {
	var (
		percpuStats CPUStats
		pervSys     uint64
		deltaSys    int64
		err         error
	)
	sysdelta := conf.Get().Tracing.Cpusys.CPUSysDelta
	sysstep := conf.Get().Tracing.Cpusys.CPUSysStep
	systh := conf.Get().Tracing.Cpusys.CPUSysth
	for {
		select {
		case <-ctx.Done():
			return 0, 0, types.ErrExitByCancelCtx
		case <-time.After(time.Duration(sysstep) * time.Second):
			if percpuStats.total == 0 {
				percpuStats, err = getCPUStats()
				if err != nil {
					return 0, 0, fmt.Errorf("get cpuStats err %w", err)
				}
				time.Sleep(1 * time.Second)
				continue
			}
			cpuStats, err := getCPUStats()
			if err != nil {
				return 0, 0, err
			}
			systotal := cpuStats.total - percpuStats.total
			if systotal == 0 {
				return 0, 0, fmt.Errorf("systotal is ZERO")
			}
			sys := (cpuStats.system - percpuStats.system) * 100 / systotal
			if pervSys != 0 {
				deltaSys = int64(sys - pervSys)
			}

			log.Debugf("cpusys alarm sys %v pervsys %v deltasys %v", sys, pervSys, deltaSys)
			pervSys = sys
			percpuStats = cpuStats

			if sys > systh || deltaSys > sysdelta {
				return sys, deltaSys, nil
			}
		}
	}
}

func getCPUStats() (CPUStats, error) {
	statData, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUStats{}, err
	}

	lines := strings.Split(string(statData), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		if fields[0] == "cpu" {
			var cpuStats CPUStats
			for i := 1; i < len(fields); i++ {
				value, err := strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					return CPUStats{}, err
				}
				cpuStats.total += value
				if i == 3 {
					cpuStats.system = value
				}
			}
			return cpuStats, nil
		}
	}
	return CPUStats{}, fmt.Errorf("failed to parse /proc/stat")
}

type cpuSysTracing struct{}

type CpuSysTracingData struct {
	NowSys       string                 `json:"now_sys"`
	SysThreshold string                 `json:"sys_threshold"`
	DeltaSys     string                 `json:"delta_sys"`
	DeltaSysTh   string                 `json:"delta_sys_th"`
	FlameData    []flamegraph.FrameData `json:"flamedata"`
}

// Start the tcpconnlat task.
func (c *cpuSysTracing) Start(ctx context.Context) error {
	// TODO: Verify the conditions for startup.
	cpuSys, delta, err := CpuSysDetect(ctx)
	if err != nil {
		return err
	}

	tracerTime := time.Now()
	dur := conf.Get().Tracing.Cpusys.CPUSysToolduration
	durstr := strconv.FormatInt(dur, 10)

	// exec tracerperf
	cmdctx, cancel := context.WithTimeout(ctx, time.Duration(dur+30)*time.Second)
	defer cancel()

	log.Infof("cpusys exec tracerperf dur %v", durstr)
	cmd := exec.CommandContext(cmdctx, "./tracer/perf.bin", "--casename", "cpusys.o", "--dur", durstr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("cpusys cmd output %v", strings.TrimSuffix(string(output), "\n"))
		return fmt.Errorf("cpusys tracerperf exec err: %w", err)
	}

	// parse json
	log.Infof("cpusys parse json")
	tracerData := CpuSysTracingData{}
	err = json.Unmarshal(output, &tracerData.FlameData)
	if err != nil {
		return fmt.Errorf("parse JSON err: %w", err)
	}

	// save
	log.Infof("cpusys upload ES")
	tracerData.NowSys = fmt.Sprintf("%d", cpuSys)
	tracerData.SysThreshold = fmt.Sprintf("%d", conf.Get().Tracing.Cpusys.CPUSysth)
	tracerData.DeltaSys = fmt.Sprintf("%d", delta)
	tracerData.DeltaSysTh = fmt.Sprintf("%d", conf.Get().Tracing.Cpusys.CPUSysDelta)
	storage.Save("cpusys", "", tracerTime, &tracerData)
	log.Infof("cpusys upload ES end")
	return err
}
