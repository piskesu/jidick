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

package collector

import (
	"huatuo-bamai/pkg/metric"
)

const (
	softirqHi = iota
	softirqTime
	softirqNetTx
	softirqNetRx
	softirqBlock
	softirqIrqPoll
	softirqTasklet
	softirqSched
	softirqHrtimer
	sofirqRcu
	softirqMax
)

const (
	latZONE0 = iota // 0 ~ 10us
	latZONE1        // 10us ~ 100us
	latZONE2        // 100us ~ 1ms
	latZONE3        // 1ms ~ inf
	latZoneMax
)

const (
	// HI:0x1
	// TIMER:0x2
	// NET_TX:0x4
	// NET_RX:0x8
	// BLOCK:0x10
	// IRQ_POLL:0x20
	// TASKLET:0x40
	// SCHED:0x80
	// HRTIMER:0x100
	// RCU:0x200
	// fullmask => 0x2ff
	defaultSiTypeMask = 0x0c // default: only report NET_TX and NET_RX so far

	// Because bpf access array is strictly checked,
	// the size of the array must be aligned in order
	// of 2, so we should not use softirqMax, but
	// use softirqArrayMax as the size of the array
	softirqArrayMax = 16 // must be 2^order
)

var monTracerIsRunning bool

func latZoneName(latZone int) string {
	switch latZone {
	case latZONE0: // 0 ~ 10us
		return "0~10 us"
	case latZONE1: // 10us ~ 100us
		return "10us ~ 100us"
	case latZONE2: // 100us ~ 1ms
		return "100us ~ 1ms"
	case latZONE3: // 1ms ~ inf
		return "1ms ~ inf"
	default:
		return "ERR_ZONE"
	}
}

func siTypeName(siType int) string {
	switch siType {
	case softirqHi:
		return "HI"
	case softirqTime:
		return "TIMER"
	case softirqNetTx:
		return "NET_TX"
	case softirqNetRx:
		return "NET_RX"
	case softirqBlock:
		return "BLOCK"
	case softirqIrqPoll:
		return "IRQ_POLL"
	case softirqTasklet:
		return "TASKLET"
	case softirqSched:
		return "SCHED"
	case softirqHrtimer:
		return "HRTIMER"
	case sofirqRcu:
		return "RCU"
	default:
		return "ERR_TYPE"
	}
}

func getMonsoftirqInfo() ([]*metric.Data, error) {
	siLabel := make(map[string]string)
	monsoftirqMetric := []*metric.Data{}

	for siType, lats := range &monsoftirqData.SoftirqLat {
		if (1<<siType)&defaultSiTypeMask == 0 {
			continue
		}
		siLabel["softirqType"] = siTypeName(siType)

		for zone, count := range lats {
			siLabel["zone"] = latZoneName(zone)
			monsoftirqMetric = append(monsoftirqMetric, metric.NewGaugeData("latency", float64(count), "softirq latency", siLabel))
		}
	}

	return monsoftirqMetric, nil
}

func (c *monsoftirqTracing) Update() ([]*metric.Data, error) {
	if !monTracerIsRunning {
		return nil, nil
	}
	monsoftirqMetric, err := getMonsoftirqInfo()
	if err != nil {
		return nil, err
	}
	return monsoftirqMetric, nil
}
