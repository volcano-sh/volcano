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
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePrometheusResult(w http.ResponseWriter, r *http.Request) {
	value := "25"
	if strings.Contains(r.FormValue("query"), "node_memory") {
		value = "50"
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1700000000,%q]}]}}`, value)
}

func TestPrometheusMetricsConnections(t *testing.T) {
	tests := []struct {
		name          string
		tls           bool
		singleNode    bool
		queryError    bool
		cancelRequest bool
	}{
		{name: "HTTP collection"},
		{name: "HTTPS collection", tls: true},
		{name: "single node", singleNode: true},
		{name: "query error", queryError: true},
		{name: "cancelled collection", cancelRequest: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var connections atomic.Int32
			var requests atomic.Int32
			closed := make(chan struct{}, 16)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				request := requests.Add(1)
				if tt.cancelRequest && request == 3 {
					cancel()
					return
				}
				if tt.queryError && request >= 3 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnprocessableEntity)
					fmt.Fprint(w, `{"status":"error","errorType":"bad_data","error":"invalid query"}`)
					return
				}
				writePrometheusResult(w, r)
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				switch state {
				case http.StateNew:
					connections.Add(1)
				case http.StateClosed:
					closed <- struct{}{}
				}
			}
			if tt.tls {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			client, err := NewPrometheusMetricsClient(map[string]string{
				"address":                server.URL,
				"tls.insecureSkipVerify": strconv.FormatBool(tt.tls),
			})
			require.NoError(t, err)
			if tt.singleNode {
				metrics, err := client.NodeMetricsAvg(ctx, "node-0")
				require.NoError(t, err)
				assert.Equal(t, float64(25), metrics.CPU)
				assert.Equal(t, float64(50), metrics.Memory)
			} else {
				metrics := map[string]*NodeMetrics{"node-0": {}, "node-1": {}, "node-2": {}, "node-3": {}}
				err := client.NodesMetricsAvg(ctx, metrics)
				if !tt.queryError && !tt.cancelRequest {
					require.NoError(t, err)
					for _, metric := range metrics {
						assert.Equal(t, float64(25), metric.CPU)
						assert.Equal(t, float64(50), metric.Memory)
						assert.False(t, metric.MetricsTime.IsZero())
					}
					assert.Equal(t, int32(8), requests.Load())
				}
			}
			require.Equal(t, int32(1), connections.Load(), "queries should reuse one connection")
			// The server observes the client's close asynchronously.
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("connection remained open after metrics collection returned")
			}
		})
	}
}

func TestPrometheusMetricsCollectionInitialization(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		nodes     map[string]*NodeMetrics
		wantError bool
	}{
		{name: "empty collection", address: ":invalid"},
		{name: "invalid address", address: ":invalid", nodes: map[string]*NodeMetrics{"node": {}}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewPrometheusMetricsClient(map[string]string{"address": tt.address})
			require.NoError(t, err)
			err = client.NodesMetricsAvg(context.Background(), tt.nodes)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkPrometheusMetricsCollection(b *testing.B) {
	for _, nodeCount := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("nodes=%d", nodeCount), func(b *testing.B) {
			server := httptest.NewTLSServer(http.HandlerFunc(writePrometheusResult))
			defer server.Close()
			client, err := NewPrometheusMetricsClient(map[string]string{"address": server.URL, "tls.insecureSkipVerify": "true"})
			require.NoError(b, err)
			metrics := make(map[string]*NodeMetrics, nodeCount)
			for i := 0; i < nodeCount; i++ {
				metrics[fmt.Sprintf("node-%d", i)] = &NodeMetrics{}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := client.NodesMetricsAvg(context.Background(), metrics); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
