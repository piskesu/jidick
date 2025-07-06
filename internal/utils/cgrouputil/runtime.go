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

package cgrouputil

import (
	"fmt"
	"os"

	"huatuo-bamai/internal/log"

	cgroups "github.com/containerd/cgroups/v3"
	"github.com/containerd/cgroups/v3/cgroup1"
	"github.com/containerd/cgroups/v3/cgroup2"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// RuntimeCgroup instance
type RuntimeCgroup struct {
	cgv1 cgroup1.Cgroup
	cgv2 *cgroup2.Manager
	mode cgroups.CGMode
}

var runtimeCgroupPeriod uint64 = 100000

func newRuntimeCgroupV1(cgPath string, cgResources *specs.LinuxResources) (*RuntimeCgroup, error) {
	cg, err := cgroup1.New(cgroup1.StaticPath(cgPath), cgResources)
	if err != nil {
		return nil, err
	}

	if err := cg.Add(cgroup1.Process{Pid: os.Getpid()}); err != nil {
		_ = cg.Delete()
		return nil, err
	}

	return &RuntimeCgroup{cgv1: cg, mode: cgroups.Legacy}, nil
}

func newRuntimeCgroupV2(cgPath string, cgResources *specs.LinuxResources) (*RuntimeCgroup, error) {
	m, err := cgroup2.NewSystemd("/", cgPath+".slice", -1, cgroup2.ToResources(cgResources))
	if err != nil {
		return nil, fmt.Errorf("cgroup2 new systemd: %w", err)
	}

	// enable cpu and memory cgroup controllers
	if err := m.ToggleControllers([]string{"cpu", "memory"}, cgroup2.Enable); err != nil {
		_ = m.DeleteSystemd()
		return nil, fmt.Errorf("cgroup2 enabling cpu and memory controllers: %w", err)
	}

	if err := m.AddProc(uint64(os.Getpid())); err != nil {
		_ = m.DeleteSystemd()
		return nil, fmt.Errorf("cgroup2 adding pids proc: %w", err)
	}

	log.Debugf("huatuo-bamai use cgroup path: %v", m)

	return &RuntimeCgroup{cgv2: m, mode: cgroups.Unified}, nil
}

func runtimeCgroupMode(mode cgroups.CGMode) string {
	switch mode {
	case cgroups.Legacy:
		return "legacy"
	case cgroups.Unified:
		return "unified"
	case cgroups.Hybrid:
		return "hybrid"
	}

	return "unavailable"
}

// NewRuntimeCgroup new instance
func NewRuntimeCgroup(cgPath string, cpu float64, mem int64) (*RuntimeCgroup, error) {
	quota := int64(cpu * float64(runtimeCgroupPeriod))

	cgResources := &specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Period: &runtimeCgroupPeriod,
			Quota:  &quota,
		},
		Memory: &specs.LinuxMemory{
			Limit: &mem,
		},
	}

	mode := cgroups.Mode()
	switch mode {
	case cgroups.Legacy:
		return newRuntimeCgroupV1(cgPath, cgResources)
	case cgroups.Unified:
		return newRuntimeCgroupV2(cgPath, cgResources)
	default:
		return nil, fmt.Errorf("cgroup type(%s) not supported", runtimeCgroupMode(mode))
	}
}

// Delete HostCgroup
func (host *RuntimeCgroup) Delete() {
	// 1. move pids to cgroup rootfs temporarily
	// 2. delete cgroups.
	switch host.mode {
	case cgroups.Legacy:
		rootfs, _ := cgroup1.Load(cgroup1.RootPath)
		_ = host.cgv1.MoveTo(rootfs)
		_ = host.cgv1.Delete()
	case cgroups.Unified:
		rootfs, _ := cgroup2.LoadSystemd("/", "")
		_ = host.cgv2.MoveTo(rootfs)
		_ = host.cgv2.Delete()
		_ = host.cgv2.DeleteSystemd()
	}
}

// UpdateCPU update resource
func (host *RuntimeCgroup) UpdateCPU(cpu float64) error {
	quota := int64(cpu * float64(runtimeCgroupPeriod))

	cgResources := &specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Period: &runtimeCgroupPeriod,
			Quota:  &quota,
		},
	}

	switch host.mode {
	case cgroups.Legacy:
		return host.cgv1.Update(cgResources)
	case cgroups.Unified:
		return host.cgv2.Update(cgroup2.ToResources(cgResources))
	default:
		return fmt.Errorf("cgroup type(%s) not supported", runtimeCgroupMode(host.mode))
	}
}
