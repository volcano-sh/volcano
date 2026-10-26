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

package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/version"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	restclient "k8s.io/client-go/rest"
	_ "k8s.io/kubernetes/pkg/features"
)

func useTestFeatureGate(t *testing.T) {
	original := utilfeature.DefaultMutableFeatureGate
	utilfeature.DefaultMutableFeatureGate = original.DeepCopy()
	t.Cleanup(func() { utilfeature.DefaultMutableFeatureGate = original })
}

func TestAddFeatureGatesFlagKeepsRepeatedOverrides(t *testing.T) {
	useTestFeatureGate(t)
	var featureGates map[string]bool
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	AddFeatureGatesFlag(fs, &featureGates)

	err := fs.Parse([]string{"--feature-gates=DRAExtendedResource=false", "--feature-gates=UnknownGate=true"})

	assert.NoError(t, err)
	assert.Equal(t, map[string]bool{"DRAExtendedResource": false, "UnknownGate": true}, featureGates)
}

func TestSetupComponentGlobalsFeatureGates(t *testing.T) {
	tests := []struct {
		name         string
		minor        string
		featureGates map[string]bool
		wantErr      string
	}{
		{
			name:         "gate not locked on 1.36 is accepted",
			minor:        "36",
			featureGates: map[string]bool{"DRAExtendedResource": false},
		},
		{
			name:         "gate locked on 1.37 is rejected",
			minor:        "37",
			featureGates: map[string]bool{"DRAExtendedResource": false},
			wantErr:      "feature is locked to true",
		},
		{
			name:         "unknown gate is rejected",
			minor:        "37",
			featureGates: map[string]bool{"UnknownGate": true},
			wantErr:      "unrecognized feature gate: UnknownGate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			useTestFeatureGate(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(version.Info{Major: "1", Minor: test.minor, GitVersion: "v1." + test.minor + ".0"})
			}))
			defer server.Close()

			err := SetupComponentGlobals(&restclient.Config{Host: server.URL}, test.featureGates)

			if test.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantErr)
		})
	}
}
