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
	"fmt"
	"path/filepath"

	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/pod"
	"huatuo-bamai/internal/utils/cgrouputil"
	"huatuo-bamai/internal/utils/parseutil"
	"huatuo-bamai/pkg/metric"
	"huatuo-bamai/pkg/tracing"
)

type memOthersCollector struct{}

func init() {
	// only for didicloud
	tracing.RegisterEventTracing("memory_others", newMemOthersCollector)
}

func newMemOthersCollector() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &memOthersCollector{},
		Flag:        tracing.FlagMetric,
	}, nil
}

func parseValueWithKey(path, key string) (uint64, error) {
	filePath := filepath.Join(cgrouputil.V1MemoryPath(), path)
	if key == "" {
		return parseutil.ReadUint(filePath)
	}

	raw, err := parseutil.ParseRawKV(filePath)
	if err != nil {
		return 0, err
	}

	return raw[key], nil
}

func (c *memOthersCollector) Update() ([]*metric.Data, error) {
	containers, err := pod.GetNormalContainers()
	if err != nil {
		return nil, fmt.Errorf("Can't get normal container: %w", err)
	}

	metrics := []*metric.Data{}

	for _, container := range containers {
		for _, t := range []struct {
			path string
			key  string
			name string
		}{
			{
				path: "memory.directstall_stat",
				key:  "directstall_time",
				name: "directstall_time",
			},
			{
				path: "memory.asynreclaim_stat",
				key:  "asyncreclaim_time",
				name: "asyncreclaim_time",
			},
			{
				path: "memory.local_direct_reclaim_time",
				key:  "",
				name: "local_direct_reclaim_time",
			},
		} {
			path := filepath.Join(container.CgroupSuffix, t.path)
			value, err := parseValueWithKey(path, t.key)
			if err != nil {
				// FIXME: os maynot support this metric
				log.Debugf("parse %s: %s", path, err)
				continue
			}

			metrics = append(metrics,
				metric.NewContainerGaugeData(container, t.name, float64(value), fmt.Sprintf("memory cgroup %s", t.name), nil))
		}
	}

	return metrics, nil
}
