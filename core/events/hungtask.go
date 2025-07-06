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
	"strings"
	"time"

	"huatuo-bamai/internal/bpf"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/storage"
	"huatuo-bamai/internal/utils/bpfutil"
	"huatuo-bamai/internal/utils/kmsgutil"
	"huatuo-bamai/pkg/metric"
	"huatuo-bamai/pkg/tracing"
)

//go:generate $BPF_COMPILE $BPF_INCLUDE -s $BPF_DIR/hungtask.c -o $BPF_DIR/hungtask.o

type hungTaskPerfEventData struct {
	Pid  int32
	Comm [bpfutil.TaskCommLen]byte
}

// HungTaskTracerData is the full data structure.
type HungTaskTracerData struct {
	Pid                   int32  `json:"pid"`
	Comm                  string `json:"comm"`
	CPUsStack             string `json:"cpus_stack"`
	BlockedProcessesStack string `json:"blocked_processes_stack"`
}

type hungTaskTracing struct {
	hungtaskMetric []*metric.Data
}

func init() {
	tracing.RegisterEventTracing("hungtask", newHungTask)
}

func newHungTask() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &hungTaskTracing{
			hungtaskMetric: []*metric.Data{
				metric.NewGaugeData("happened", 0, "hungtask happened", nil),
			},
		},
		Internal: 10,
		Flag:     tracing.FlagMetric | tracing.FlagTracing,
	}, nil
}

var hungtaskCounter float64

func (c *hungTaskTracing) Update() ([]*metric.Data, error) {
	c.hungtaskMetric[0].Value = hungtaskCounter
	hungtaskCounter = 0
	return c.hungtaskMetric, nil
}

func (c *hungTaskTracing) Start(ctx context.Context) error {
	b, err := bpf.LoadBpf(bpfutil.ThisBpfOBJ(), nil)
	if err != nil {
		log.Infof("failed to LoadBpf, err: %v", err)
		return err
	}
	defer b.Close()

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	reader, err := b.AttachAndEventPipe(childCtx, "hungtask_perf_events", 8192)
	if err != nil {
		log.Infof("failed to AttachAndEventPipe, err: %v", err)
		return err
	}
	defer reader.Close()

	b.WaitDetachByBreaker(childCtx, cancel)

	for {
		select {
		case <-childCtx.Done():
			return nil
		default:
			var data hungTaskPerfEventData
			if err := reader.ReadInto(&data); err != nil {
				return fmt.Errorf("ReadFromPerfEvent fail: %w", err)
			}

			cpusBT, err := kmsgutil.GetAllCPUsBT()
			if err != nil {
				cpusBT = err.Error()
			}
			blockedProcessesBT, err := kmsgutil.GetBlockedProcessesBT()
			if err != nil {
				blockedProcessesBT = err.Error()
			}

			caseData := &HungTaskTracerData{
				Pid:                   data.Pid,
				Comm:                  strings.TrimRight(string(data.Comm[:]), "\x00"),
				CPUsStack:             cpusBT,
				BlockedProcessesStack: blockedProcessesBT,
			}
			hungtaskCounter++

			// save storage
			storage.Save("hungtask", "", time.Now(), caseData)
		}
	}
}
