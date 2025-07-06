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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"huatuo-bamai/internal/utils/parseutil"
)

const nanosecondsInSecond = 1000000000

var clockTicks = getClockTicks()

// NewCPUAcct new obj with rootfs
func NewCPUAcct(root string) *CPUAcct {
	return &CPUAcct{
		root: root,
	}
}

// NewCPUAcctDefault new obj with default rootfs
func NewCPUAcctDefault() *CPUAcct {
	return &CPUAcct{
		root: V1CpuPath(),
	}
}

// CPUAcct cgroup obj
type CPUAcct struct {
	root string
}

// Path join file path
func (c *CPUAcct) Path(path string) string {
	return filepath.Join(c.root, path)
}

// PercpuUsage return values in cpuacct.usage_percpu
func (c *CPUAcct) PercpuUsage(path string) ([]uint64, error) {
	var usage []uint64
	data, err := os.ReadFile(filepath.Join(c.Path(path), "cpuacct.usage_percpu"))
	if err != nil {
		return nil, err
	}
	for _, v := range strings.Fields(string(data)) {
		u, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, err
		}
		usage = append(usage, u)
	}
	return usage, nil
}

// Usage return value in cpuacct.usage
func (c *CPUAcct) Usage(path string) (uint64, error) {
	return parseutil.ReadUint(filepath.Join(c.Path(path), "cpuacct.usage"))
}

// Stat return user/kernel values in cpuacct.stat
func (c *CPUAcct) Stat(path string) (user, kernel uint64, err error) {
	statPath := filepath.Join(c.Path(path), "cpuacct.stat")
	f, err := os.Open(statPath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	var (
		raw = make(map[string]uint64)
		sc  = bufio.NewScanner(f)
	)
	for sc.Scan() {
		key, v, err := parseutil.ParseKV(sc.Text())
		if err != nil {
			return 0, 0, err
		}
		raw[key] = v
	}
	if err := sc.Err(); err != nil {
		return 0, 0, err
	}
	for _, t := range []struct {
		name  string
		value *uint64
	}{
		{
			name:  "user",
			value: &user,
		},
		{
			name:  "system",
			value: &kernel,
		},
	} {
		v, ok := raw[t.name]
		if !ok {
			return 0, 0, fmt.Errorf("expected field %q but not found in %q", t.name, statPath)
		}
		*t.value = v
	}
	return (user * nanosecondsInSecond) / clockTicks, (kernel * nanosecondsInSecond) / clockTicks, nil
}
