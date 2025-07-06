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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
)

const (
	defaultESIndex = "huatuo_bamai"
)

type esClient struct {
	client  *elasticsearch.Client
	esIndex string
}

func newESClient(addr, username, password, index string) (*esClient, error) {
	cfg := elasticsearch.Config{
		Addresses: []string{addr},
		Username:  username,
		Password:  password,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: 10 * time.Second,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		},
	}

	client, err := elasticsearch.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("new client: %w", err)
	}

	// ping/check es server ...
	res, err := client.Info()
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch return statuscode: %d", res.StatusCode)
	}

	if index == "" {
		index = defaultESIndex
	}
	return &esClient{client: client, esIndex: index}, nil
}

// Write the data into ES.
func (e *esClient) Write(doc *document) error {
	data, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("json Marshal: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      e.esIndex,
		DocumentID: "",
		Body:       strings.NewReader(string(data)),
	}

	res, err := req.Do(context.Background(), e.client)
	if err != nil {
		return fmt.Errorf("index document: %w", err)
	}
	defer res.Body.Close()

	// Check the response status code
	if res.IsError() {
		body, _ := io.ReadAll(res.Body)
		return fmt.Errorf("index document failed with status: %s, response: %s; error: %s",
			res.Status(), res.String(), string(body))
	}

	var r map[string]any
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return fmt.Errorf("parse response body: %w", err)
	}

	return nil
}
