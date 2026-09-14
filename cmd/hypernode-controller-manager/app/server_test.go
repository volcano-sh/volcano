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

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/rest"
)

func TestPrepareController(t *testing.T) {
	if _, err := prepareController(nil); err == nil {
		t.Fatal("prepareController() accepted nil config")
	}
	run, err := prepareController(&rest.Config{Host: "https://127.0.0.1"})
	if err != nil {
		t.Fatalf("prepareController() returned an error: %v", err)
	}
	if run == nil {
		t.Fatal("prepareController() returned a nil runner")
	}
}

func TestHealthzHandler(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		ready      bool
		statusCode int
	}{
		{name: "health is independent of readiness", path: "/healthz", statusCode: http.StatusOK},
		{name: "not ready", path: "/readyz", statusCode: http.StatusServiceUnavailable},
		{name: "ready", path: "/readyz", ready: true, statusCode: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processReady.Store(test.ready)
			t.Cleanup(func() { processReady.Store(false) })
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			healthzHandler().ServeHTTP(response, request)
			if response.Code != test.statusCode {
				t.Fatalf("status code = %d, want %d", response.Code, test.statusCode)
			}
		})
	}
}
