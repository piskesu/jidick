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
	"os"

	cgroups "github.com/containerd/cgroups/v3/cgroup1"
	"github.com/opencontainers/runtime-spec/specs-go"
)

// RuntimeCgroup instance
type RuntimeCgroup struct {
	cg cgroups.Cgroup
}

var runtimeCgroupPeriod uint64 = 100000

// NewRuntimeCgroup new instance
func NewRuntimeCgroup(cgPath string, cpu float64, mem int64) (*RuntimeCgroup, error) {
	quota := int64(cpu * float64(runtimeCgroupPeriod))

	cg, err := cgroups.New(cgroups.StaticPath(cgPath), &specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Period: &runtimeCgroupPeriod,
			Quota:  &quota,
		},
		Memory: &specs.LinuxMemory{
			Limit: &mem,
		},
	})
	if err != nil {
		return nil, err
	}

	if err := cg.Add(cgroups.Process{Pid: os.Getpid()}); err != nil {
		_ = cg.Delete()
		return nil, err
	}

	return &RuntimeCgroup{cg: cg}, nil
}

// Delete HostCgroup
func (host *RuntimeCgroup) Delete() {
	// move pids to cgroup rootfs temporarily, make sure we can remove cgroup dir
	rootfs, _ := cgroups.Load(cgroups.RootPath)
	_ = host.cg.MoveTo(rootfs)
	_ = host.cg.Delete()
}

// UpdateCPU update resource
func (host *RuntimeCgroup) UpdateCPU(cpu float64) error {
	quota := int64(cpu * float64(runtimeCgroupPeriod))
	return host.cg.Update(&specs.LinuxResources{
		CPU: &specs.LinuxCPU{
			Period: &runtimeCgroupPeriod,
			Quota:  &quota,
		},
	})
}
