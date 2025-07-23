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
	"time"

	"huatuo-bamai/internal/cgroups"
	"huatuo-bamai/internal/log"
	"huatuo-bamai/internal/utils/parseutil"
	"huatuo-bamai/pkg/metric"
	"huatuo-bamai/pkg/tracing"

	"github.com/prometheus/procfs"
)

const (
	// CLK_TCK is a constant on Linux for all architectures except alpha and ia64.
	// See e.g.
	// https://git.musl-libc.org/cgit/musl/tree/src/conf/sysconf.c#n30
	// https://github.com/containerd/cgroups/pull/12
	// https://lore.kernel.org/lkml/agtlq6$iht$1@penguin.transmeta.com/
	userHZ int64 = 100
)

type runtimeCollector struct {
	oldStat *procfs.ProcStat
	oldTs   int64
}

func init() {
	tracing.RegisterEventTracing("runtime", newQosCollector)
}

func newQosCollector() (*tracing.EventTracingAttr, error) {
	return &tracing.EventTracingAttr{
		TracingData: &runtimeCollector{},
		Flag:        tracing.FlagMetric,
	}, nil
}

func (c *runtimeCollector) Update() ([]*metric.Data, error) {
	runtimeMetric := make([]*metric.Data, 0)

	p, err := procfs.Self()
	if err != nil {
		return nil, err
	}

	runtimeMetric = append(runtimeMetric, getCPUMetric(c, &p)...)
	runtimeMetric = append(runtimeMetric, getMemoryMetric(&p)...)

	return runtimeMetric, nil
}

func getCPUMetric(c *runtimeCollector, p *procfs.Proc) []*metric.Data {
	stat, err := p.Stat()
	if err != nil {
		log.Warnf("not get process stat: %v", err)
		return nil
	}
	ts := time.Now().Unix()

	if c.oldStat == nil {
		c.oldStat = &stat
	}

	if c.oldTs == 0 {
		c.oldTs = ts
		return nil
	}

	data := make([]*metric.Data, 2)
	duration := ts - c.oldTs

	// huatuo-bamai.cpu.user(*100)
	user := float64(stat.UTime-c.oldStat.UTime) / float64(userHZ*duration)
	data[0] = metric.NewGaugeData("cpu_user", user*100, "user cpu", nil)

	// huatuo-bamai.cpu.sys(*100)
	sys := float64(stat.STime-c.oldStat.STime) / float64(userHZ*duration)
	data[1] = metric.NewGaugeData("cpu_sys", sys*100, "sys cpu", nil)

	// save stat
	c.oldStat = &stat
	c.oldTs = ts

	return data
}

func getMemoryMetric(p *procfs.Proc) []*metric.Data {
	data := make([]*metric.Data, 3)
	status, err := p.NewStatus()
	if err != nil {
		log.Warnf("not get process status: %v", err)
		return nil
	}

	data[0] = metric.NewGaugeData("memory_vss", float64(status.VmSize)/1024, "memory vss", nil)
	data[1] = metric.NewGaugeData("memory_rss", float64(status.VmRSS)/1024, "memory rss", nil)

	rssI, err := parseutil.ReadUint(cgroups.RootFsFilePath("memory") + "/huatuo-bamai/memory.usage_in_bytes")
	if err != nil {
		log.Warnf("can't ParseUint, err: %v", err)
		return nil
	}
	data[2] = metric.NewGaugeData("memory_cgroup_rss", float64(rssI)/1024, "memory cgroup rss", nil)

	return data
}
