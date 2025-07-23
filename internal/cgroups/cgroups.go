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

package cgroups

import (
	"fmt"
	"path/filepath"

	"huatuo-bamai/internal/cgroups/paths"
	"huatuo-bamai/internal/cgroups/stats"
	v1 "huatuo-bamai/internal/cgroups/v1"
	v2 "huatuo-bamai/internal/cgroups/v2"

	extcgroups "github.com/containerd/cgroups/v3"
	"github.com/opencontainers/runtime-spec/specs-go"
)

var cpuPeriod uint64 = 100000

type Cgroup interface {
	// Name returns the cgroup name.
	Name() string
	// New a runtime config instance.
	NewRuntime(path string, spec *specs.LinuxResources) error
	// Delete a runtime config
	DeleteRuntime() error
	// Update a runtime config
	UpdateRuntime(spec *specs.LinuxResources) error
	// Add pids to cgroup.procs
	AddProc(pid uint64) error
	// CpuUsage return cgroups user/system and total usage.
	CpuUsage(path string) (*stats.CpuUsage, error)
	// CpuStatRaw return cpu.stat raw data
	CpuStatRaw(path string) (map[string]uint64, error)
	// CpuQuotaAndPeriod cgroup quota and period
	CpuQuotaAndPeriod(path string) (*stats.CpuQuota, error)
	// MemoryStatRaw memory.stat
	MemoryStatRaw(path string) (map[string]uint64, error)
	// MemoryEventRaw memory.stat
	MemoryEventRaw(path string) (map[string]uint64, error)
}

func NewCgroupManager() (Cgroup, error) {
	switch extcgroups.Mode() {
	case extcgroups.Legacy:
		return v1.New()
	case extcgroups.Hybrid, extcgroups.Unified:
		return v2.New()
	default:
		return nil, fmt.Errorf("not supported")
	}
}

func ToSpec(cpu float64, memory int64) *specs.LinuxResources {
	spec := &specs.LinuxResources{}

	if cpu != 0 {
		quota := int64(cpu * float64(cpuPeriod))
		spec.CPU = &specs.LinuxCPU{
			Period: &cpuPeriod,
			Quota:  &quota,
		}
	}

	if memory != 0 {
		spec.Memory = &specs.LinuxMemory{Limit: &memory}
	}

	return spec
}

func RootFsFilePath(subsys string) string {
	return filepath.Join(paths.RootfsDefaultPath, subsys)
}
