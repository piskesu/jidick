package sysfsutil

import (
	"fmt"
	"sync"

	"github.com/prometheus/procfs/sysfs"
)

var (
	fs    sysfs.FS
	errFs error
	once  sync.Once
)

func DefaultFS() (sysfs.FS, error) {
	once.Do(func() {
		fs, errFs = sysfs.NewDefaultFS()
	})
	return fs, errFs
}

func DefaultNetClass() (sysfs.NetClass, error) {
	fs, err := DefaultFS()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize FS: %w", err)
	}

	return fs.NetClass()
}

func DefaultNetClassDevices() ([]string, error) {
	fs, err := DefaultFS()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize FS: %w", err)
	}

	return fs.NetClassDevices()
}
