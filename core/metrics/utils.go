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
	"io"
	"os"
)

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
