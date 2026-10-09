/*
 Copyright 2026 The Volcano Authors.

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
	"testing"
)

func promServer(handler http.HandlerFunc) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/query", handler)
	return httptest.NewServer(mux)
}

func TestPrometheusMetricsClient_NodeMetricsAvg_QueryError(t *testing.T) {
	server := promServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "prometheus unavailable", http.StatusInternalServerError)
	})
	defer server.Close()

	client, err := NewPrometheusMetricsClient(map[string]string{"address": server.URL})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsClient: %v", err)
	}

	metrics, err := client.NodeMetricsAvg(context.Background(), "test-node")
	if err == nil {
		t.Fatalf("expected error when prometheus returns 500, got nil (metrics=%+v)", metrics)
	}
}

func TestPrometheusMetricsClient_NodeMetricsAvg_UnreachableEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := server.URL
	server.Close()

	client, err := NewPrometheusMetricsClient(map[string]string{"address": addr})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsClient: %v", err)
	}

	metrics, err := client.NodeMetricsAvg(context.Background(), "test-node")
	if err == nil {
		t.Fatalf("expected error when prometheus is unreachable, got nil (metrics=%+v)", metrics)
	}
}

func TestPrometheusMetricsClient_NodeMetricsAvg_EmptyVector(t *testing.T) {
	server := promServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	})
	defer server.Close()

	client, err := NewPrometheusMetricsClient(map[string]string{"address": server.URL})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsClient: %v", err)
	}

	metrics, err := client.NodeMetricsAvg(context.Background(), "test-node")
	if err == nil {
		t.Fatalf("expected error when prometheus returns empty vector for both metrics, got nil (metrics=%+v)", metrics)
	}
}

func TestPrometheusMetricsClient_NodeMetricsAvg_WrongResultType(t *testing.T) {
	server := promServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"scalar","result":[1700000000,"1"]}}`))
	})
	defer server.Close()

	client, err := NewPrometheusMetricsClient(map[string]string{"address": server.URL})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsClient: %v", err)
	}

	metrics, err := client.NodeMetricsAvg(context.Background(), "test-node")
	if err == nil {
		t.Fatalf("expected error when prometheus returns non-Vector type for both metrics, got nil (metrics=%+v)", metrics)
	}
}

func TestPrometheusMetricsClient_NodeMetricsAvg_MalformedVectorRow(t *testing.T) {
	server := promServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "status": "success",
  "data": {
    "resultType": "vector",
    "result": [
      {"metric": {"instance": "test-node"}, "value": [1700000000, "not-a-number"]}
    ]
  }
}`))
	})
	defer server.Close()

	client, err := NewPrometheusMetricsClient(map[string]string{"address": server.URL})
	if err != nil {
		t.Fatalf("NewPrometheusMetricsClient: %v", err)
	}

	metrics, err := client.NodeMetricsAvg(context.Background(), "test-node")
	if err == nil {
		t.Fatalf("expected error for malformed vector value, got nil (metrics=%+v)", metrics)
	}
}
