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

package pidutil

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"syscall"
)

var defaultDirPath = "/var/run"

func fullPath(name string) string {
	return fmt.Sprintf("%s/%s.pid", defaultDirPath, name)
}

// LockPidFile lock runtime pidfile
func LockPidFile(name string) error {
	fd, err := syscall.Open(fullPath(name), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return err
	}

	lock := syscall.Flock_t{Type: syscall.F_WRLCK}
	if err := syscall.FcntlFlock(uintptr(fd), syscall.F_SETLK, &lock); err != nil {
		return err
	}

	if _, err := syscall.Write(fd, []byte(strconv.Itoa(os.Getpid()))); err != nil {
		return err
	}
	return nil
}

// RemovePidFile aremove runtime pidfile
func RemovePidFile(name string) {
	os.Remove(fullPath(name))
}

// Read reads the "PID file" at path, and returns the PID if it contains a
// valid PID of a running process, or 0 otherwise. It returns an error when
// failing to read the file, or if the file doesn't exist, but malformed content
// is ignored. Consumers should therefore check if the returned PID is a non-zero
// value before use.
func Read(path string) (int, error) {
	pidByte, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(string(bytes.TrimSpace(pidByte)))
}
