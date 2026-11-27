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

package healthcheck

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/networkqos"
)

// fakeNetworkQoSManager only implements HealthCheck; the other methods are
// not used by the health checker.
type fakeNetworkQoSManager struct {
	healthErr error
}

func (f *fakeNetworkQoSManager) Init() error                            { return nil }
func (f *fakeNetworkQoSManager) HealthCheck() error                     { return f.healthErr }
func (f *fakeNetworkQoSManager) EnableNetworkQoS(*api.NetworkQos) error { return nil }
func (f *fakeNetworkQoSManager) DisableNetworkQoS() error               { return nil }

func TestHealthCheck(t *testing.T) {
	const qosErrMsg = "network qos is not ready"

	tests := []struct {
		name       string
		qosManager networkqos.NetworkQoSManager
		wantCode   int
		wantBody   string
	}{
		{
			name:       "network qos disabled",
			qosManager: nil,
			wantCode:   http.StatusOK,
			wantBody:   "ok",
		},
		{
			name:       "network qos healthy",
			qosManager: &fakeNetworkQoSManager{},
			wantCode:   http.StatusOK,
			wantBody:   "ok",
		},
		{
			name:       "network qos unhealthy",
			qosManager: &fakeNetworkQoSManager{healthErr: errors.New(qosErrMsg)},
			wantCode:   http.StatusInternalServerError,
			wantBody:   qosErrMsg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewHealthChecker(tt.qosManager)
			rec := httptest.NewRecorder()

			checker.HealthCheck(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Equal(t, tt.wantBody, rec.Body.String())
		})
	}
}
