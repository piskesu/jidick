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
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"huatuo-bamai/internal/bpf"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/utils/bpfutil"
	"huatuo-bamai/pkg/tracing"
)

func init() {
	tracing.RegisterEventTracing("monsoftirq", newSoftirqCollector)
}

func newSoftirqCollector() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &monsoftirqTracing{},
		Internal:    10,
		Flag:        tracing.FlagTracing | tracing.FlagMetric,
	}, nil
}

//go:generate $BPF_COMPILE $BPF_INCLUDE -s $BPF_DIR/monsoftirq_tracing.c -o $BPF_DIR/monsoftirq_tracing.o

type monsoftirqBpfData struct {
	SoftirqLat [softirqArrayMax][latZoneMax]uint64
}

type monsoftirqTracing struct{}

var monsoftirqData monsoftirqBpfData

// Start monsoftirq work, load bpf and wait data form perfevent
func (c *monsoftirqTracing) Start(ctx context.Context) error {
	// load bpf.
	b, err := bpf.LoadBpf(bpfutil.ThisBpfOBJ(), nil)
	if err != nil {
		return fmt.Errorf("failed to LoadBpf, err: %w", err)
	}
	defer b.Close()

	if err = b.Attach(); err != nil {
		return err
	}

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	b.WaitDetachByBreaker(childCtx, cancel)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	monTracerIsRunning = true
	defer func() { monTracerIsRunning = false }()

	for {
		select {
		case <-childCtx.Done():
			return nil
		case <-ticker.C:
			item, err := b.ReadMap(b.MapIDByName("softirq_lats"), []byte{0, 0, 0, 0})
			if err != nil {
				return fmt.Errorf("failed to read softirq_lats: %w", err)
			}
			buf := bytes.NewReader(item)
			if err = binary.Read(buf, binary.LittleEndian, &monsoftirqData); err != nil {
				log.Errorf("can't read softirq_lats: %v", err)
				return err
			}
		}
	}
}
