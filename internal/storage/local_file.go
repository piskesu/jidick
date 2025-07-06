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

package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"huatuo-bamai/internal/rotator"
)

type localFileStorage struct {
	fileLock          sync.Mutex
	files             map[string]io.Writer
	localPath         string
	localRotationSize int
	localMaxRotation  int
}

var fileWriterMap sync.Map

func newLocalFileStorage(path string, maxRotation, rotationSize int) (*localFileStorage, error) {
	return &localFileStorage{
		localPath:         path,
		localMaxRotation:  maxRotation,
		localRotationSize: rotationSize,
		files:             make(map[string]io.Writer),
	}, nil
}

// newFileWriter create a file rotator
func (f *localFileStorage) newFileWriter(filename string) io.Writer {
	filepath := path.Join(f.localPath, filename)

	writer, ok := fileWriterMap.Load(filepath)
	if !ok {
		writer = rotator.NewSizeRotator(filepath, f.localMaxRotation, f.localRotationSize)
		fileWriterMap.Store(filepath, writer)
	}

	f.files[filename] = writer.(io.Writer)
	return f.files[filename]
}

func (f *localFileStorage) fileWriter(tracerName string) io.Writer {
	if writer, ok := f.files[tracerName]; ok {
		return writer
	}

	f.fileLock.Lock()
	defer f.fileLock.Unlock()

	if _, err := os.Stat(f.localPath); os.IsNotExist(err) {
		_ = os.MkdirAll(f.localPath, 0o755)
	}

	return f.newFileWriter(tracerName)
}

func (f *localFileStorage) write(tracerName string, content []byte) error {
	_, err := f.fileWriter(tracerName).Write(content)
	return err
}

// writeTitle writes the title into the tracerName file.
func (f *localFileStorage) writeTitle(tracerName string, doc *document) error {
	str := fmt.Sprintf("%s Host=%s Region=%s ", time.Now().Format("2006-01-02 15:04:05"), doc.Hostname, doc.Region)

	// container
	if doc.ContainerID != "" {
		str += fmt.Sprintf("ContainerHost=%s ContainerID=%s ContainerType=%s ContainerLevel=%s ", doc.ContainerHostname,
			doc.ContainerID, doc.ContainerType, doc.ContainerQos)
	}

	return f.write(tracerName, []byte(str+"\n"))
}

// writeDocument writes the details into the tracerName file.
func (f *localFileStorage) writeDocument(tracerName string, doc *document) error {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)

	// disable escapeHTML
	encoder.SetEscapeHTML(false)
	// indent
	encoder.SetIndent("", "  ")

	// encode
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("json Marshal by %s: %w", tracerName, err)
	}

	// write
	return f.write(tracerName, buffer.Bytes())
}

// Write the data into local file.
//
// The datas format:
//
//	<title>
//	<document>
//
// The title format:
//
//	<Time> Host=<Host> Region=<Region> ContainerHost=<Container Hostname> ContainerID=<Container ID>
//		ContainerType=<Container Type> ContainerLevel=<Container Level>
func (f *localFileStorage) Write(doc *document) error {
	tracerName := doc.TracerName

	// write title.
	if err := f.writeTitle(tracerName, doc); err != nil {
		return err
	}

	// write document.
	return f.writeDocument(tracerName, doc)
}
