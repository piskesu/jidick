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
	"path/filepath"

	"huatuo-bamai/internal/utils/parseutil"
)

// NewMemory new cpu obj with default rootfs
func NewMemory() *Memory {
	return &Memory{
		root: V1MemoryPath(),
	}
}

// Memory cgroup obj
type Memory struct {
	root string
}

// Path join path with cgroup v1 rootfs
func (c *Memory) Path(path string) string {
	return filepath.Join(c.root, path)
}

// EventsRaw return kv slice in memory.events
func (c *Memory) EventsRaw(path string) (map[string]uint64, error) {
	return parseutil.ParseRawKV(filepath.Join(c.Path(path), "memory.events"))
}
