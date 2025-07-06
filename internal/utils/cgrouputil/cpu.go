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

/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package cgrouputil

import (
	"fmt"
	"path/filepath"

	"huatuo-bamai/internal/utils/parseutil"
)

// NewCPU new cpu obj with defalut rootfs
func NewCPU() *CPU {
	return &CPU{
		root: V1CpuPath(),
	}
}

// CPU cgroup obj
type CPU struct {
	root string
}

// Path join path with cgroup v1 rootfs
func (c *CPU) Path(path string) string {
	return filepath.Join(c.root, path)
}

// StatRaw return kv slice in cpu.stat
func (c *CPU) StatRaw(path string) (map[string]uint64, error) {
	return parseutil.ParseRawKV(filepath.Join(c.Path(path), "cpu.stat"))
}

// CPUCount return cgroup v1 cpu num
func (c *CPU) CPUNum(path string) (int, error) {
	period, err := parseutil.ReadInt(filepath.Join(c.Path(path), "cpu.cfs_period_us"))
	if err != nil {
		return 0, err
	}

	if period == -1 {
		return 0, fmt.Errorf("no limited")
	}

	quota, err := parseutil.ReadUint(filepath.Join(c.Path(path), "cpu.cfs_quota_us"))
	if err != nil {
		return 0, err
	}

	return int(quota / uint64(period)), nil
}
