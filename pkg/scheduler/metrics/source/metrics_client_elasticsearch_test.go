/*
 Copyright 2023 The Volcano Authors.

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

package source

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v7"
)

func TestElasticsearchMetricsClientDefaultIndexName(t *testing.T) {
	client, err := NewElasticsearchMetricsClient(map[string]string{"address": "http://localhost:9200"})
	if err != nil {
		t.Errorf("Failed to create client: %v", err)
	}
	if client.indexName != "metricbeat-*" {
		t.Errorf("Default index name should be metricbeat-*")
	}
}

func TestElasticsearchMetricsClientCustomIndexName(t *testing.T) {
	client, err := NewElasticsearchMetricsClient(map[string]string{"address": "http://localhost:9200", "elasticsearch.index": "custom-index"})
	if err != nil {
		t.Errorf("Failed to create client: %v", err)
	}
	if client.indexName != "custom-index" {
		t.Errorf("Custom index name should be custom-index")
	}
}

func TestElasticsearchMetricsClientNodeMetricsAvg_MaxBodySizeExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		w.Write([]byte(`{"took": 1, "timed_out": false, "_shards": {"total": 1, "successful": 1, "skipped": 0, "failed": 0}, "hits": {"total": {"value": 1, "relation": "eq"}, "max_score": null, "hits": []}, "aggregations": {"cpu": {"value": 0.35}, "mem": {"value": 0.45}}, `))
		largeData := make([]byte, maxBodySize)
		for i := range largeData {
			largeData[i] = 'a'
		}
		w.Write([]byte(`"large_field": "`))
		w.Write(largeData)
		w.Write([]byte(`"}"`))
	}))
	defer server.Close()

	client, err := NewElasticsearchMetricsClient(map[string]string{
		"address": server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("Failed to create ES client: %v", err)
	}
	client.es = esClient

	_, err = client.NodeMetricsAvg(context.Background(), "test-node")

	if err == nil {
		t.Error("Expected error due to response exceeding maxBodySize, but got nil")
	} else {
		if !strings.Contains(err.Error(), "body size limit") && !strings.Contains(err.Error(), "too large") {
			t.Errorf("Expected error about body size limit, got: %v", err)
		}
	}
}
