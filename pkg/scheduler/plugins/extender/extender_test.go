/*
Copyright 2025 The Volcano Authors.

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

package extender

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestMaxBodySizeLimit2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := strings.Repeat("a", maxBodySize+1)
		w.Write([]byte(`{"padding":"` + response + `"}`))
	}))
	defer server.Close()

	plugin := &extenderPlugin{
		client: http.Client{},
		config: &extenderConfig{
			urlPrefix: server.URL,
		},
	}

	var result map[string]interface{}
	err := plugin.send("test", &PredicateRequest{Task: &api.TaskInfo{}, Node: &api.NodeInfo{}}, &result)

	if err == nil {
		t.Error("Expected error due to request body size limit, but got nil")
	} else if !strings.Contains(err.Error(), "http: request body too large") {
		t.Errorf("Expected 'http: request body too large' error, got: %v", err)
	}
}
