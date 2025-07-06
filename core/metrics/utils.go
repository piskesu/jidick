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
	"bytes"
	"encoding/binary"
	"io"
	"os"

	"huatuo-bamai/internal/log"
)

// xfs_util maps superblocks of XFS devices to retrieve
// essential information from superblock.
const (
	XFS_SB_MAGIC = 0x58465342
	XFSLABEL_MAX = 12
)

// Construct the XFS superblock, hiding unused variables
type xfsSuperBlock struct {
	SbMagicnum  uint32
	SbBlocksize uint32
	_           [16]byte
	_           [7]uint64
	_           [4]uint32
	SbLogblocks uint32
	_           [6]uint16
	_           [XFSLABEL_MAX]byte
	_           [12]uint8
	_           [8]uint64
	_           [12]uint32
	_           [16]byte
}

func fileLineCounter(filePath string) (int, error) {
	count := 0
	buf := make([]byte, 8*20*4096)

	file, err := os.Open(filePath)
	if err != nil {
		return count, err
	}
	defer file.Close()

	r := io.Reader(file)

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], []byte("\n"))

		if err == io.EOF {
			break
		}

		if err != nil {
			return count, err
		}
	}

	return count, nil
}

// Calculate the Xlog size from superblock
func xfsLogSize(path string) (float64, error) {
	file, err := os.Open(path)
	if err != nil {
		log.Infof("open failed: %v", err)
		return -1, err
	}
	defer file.Close()

	var sb xfsSuperBlock
	err = binary.Read(file, binary.BigEndian, &sb)
	if err != nil {
		log.Infof("read superblock failed: err%v", err)
		return -1, err
	}

	// Check Magic Number of Super Block
	if sb.SbMagicnum != XFS_SB_MAGIC {
		log.Infof("Not a valid XFS superblock (Magic: 0x%x)", sb.SbMagicnum)
		return -1, err
	}

	xlogBytes := float64(sb.SbLogblocks * sb.SbBlocksize)
	return xlogBytes, nil
}
