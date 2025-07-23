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

	"huatuo-bamai/internal/cgroups/paths"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/pod"
	"huatuo-bamai/pkg/metric"
	"huatuo-bamai/pkg/tracing"

	"github.com/google/cadvisor/utils/cpuload/netlink"
	"github.com/prometheus/procfs"
)

type loadavgCollector struct {
	loadAvg []*metric.Data
}

func init() {
	tracing.RegisterEventTracing("loadavg", newLoadavg)
}

// NewLoadavgCollector returns a new Collector exposing load average stats.
func newLoadavg() (*tracing.EventTracingAttr, error) {
	collector := &loadavgCollector{
		// Load average of last 1, 5 & 15 minutes.
		// See linux kernel Documentation/filesystems/proc.rst
		loadAvg: []*metric.Data{
			metric.NewGaugeData("load1", 0, "1m load average", nil),
			metric.NewGaugeData("load5", 0, "5m load average", nil),
			metric.NewGaugeData("load15", 0, "15m load average", nil),
		},
	}

	return &tracing.EventTracingAttr{
		TracingData: collector, Flag: tracing.FlagMetric,
	}, nil
}

// Read loadavg from /proc.
func (c *loadavgCollector) hostLoadAvg() error {
	fs, err := procfs.NewDefaultFS()
	if err != nil {
		return err
	}

	load, err := fs.LoadAvg()
	if err != nil {
		return err
	}

	c.loadAvg[0].Value = load.Load1
	c.loadAvg[1].Value = load.Load5
	c.loadAvg[2].Value = load.Load15
	return nil
}

func (c *loadavgCollector) Update() ([]*metric.Data, error) {
	loadAvgMetrics := []*metric.Data{}

	n, err := netlink.New()
	if err != nil {
		log.Infof("Failed to create netlink: %s", err)
		return nil, err
	}
	defer n.Stop()

	containers, err := pod.GetContainersByType(pod.ContainerTypeNormal | pod.ContainerTypeSidecar)
	if err != nil {
		return nil, fmt.Errorf("GetContainersByType: %w", err)
	}

	for _, container := range containers {
		stats, err := n.GetCpuLoad(container.Hostname, paths.Path("cpu", container.CgroupSuffix))
		if err != nil {
			log.Debugf("failed to get %s load, %v", container, err)
			continue
		}

		loadAvgMetrics = append(loadAvgMetrics,
			metric.NewContainerGaugeData(container, "nr_running", float64(stats.NrRunning), "nr_running of container", nil),
			metric.NewContainerGaugeData(container, "nr_uninterruptible", float64(stats.NrUninterruptible), "nr_uninterruptible of container", nil))
	}

	if err := c.hostLoadAvg(); err != nil {
		return nil, err
	}

	loadAvgMetrics = append(loadAvgMetrics, c.loadAvg...)
	return loadAvgMetrics, nil
}
