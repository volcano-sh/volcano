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

package config

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestNewAdapterRegistry(t *testing.T) {
	provider := conf.DeviceTopologyProviderConfiguration{
		ID:                "node-annotation-inventory",
		Type:              conf.DeviceTopologyProviderTypeAnnotation,
		IdentityNamespace: "nvidia-gpu-uuid-v1",
		Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
	}
	mockAdapter := conf.DeviceTopologyAdapterConfiguration{
		Type:     conf.DeviceTopologyAdapterTypeMock,
		Provider: "node-annotation-inventory",
		Resource: "nvidia.com/gpu",
	}

	tests := []struct {
		name         string
		config       *conf.DeviceTopologyConfiguration
		wantErr      string
		wantRegistry bool
	}{
		{
			name: "disabled without configuration",
		},
		{
			name:   "disabled without adapters",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}},
		},
		{
			name:         "builds mock adapter with the provider identity",
			config:       &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: []conf.DeviceTopologyAdapterConfiguration{mockAdapter}},
			wantRegistry: true,
		},
		{
			name: "rejects unknown provider",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: []conf.DeviceTopologyAdapterConfiguration{{
				Type: conf.DeviceTopologyAdapterTypeMock, Provider: "missing", Resource: "nvidia.com/gpu",
			}}},
			wantErr: "unknown provider",
		},
		{
			name: "rejects resource the provider does not publish",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: []conf.DeviceTopologyAdapterConfiguration{{
				Type: conf.DeviceTopologyAdapterTypeMock, Provider: "node-annotation-inventory", Resource: "amd.com/gpu",
			}}},
			wantErr: "does not publish adapter resource",
		},
		{
			name: "rejects unsupported adapter type",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: []conf.DeviceTopologyAdapterConfiguration{{
				Type: "unsupported", Provider: "node-annotation-inventory", Resource: "nvidia.com/gpu",
			}}},
			wantErr: "unsupported type",
		},
		{
			name:    "rejects two adapters for one resource",
			config:  &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: []conf.DeviceTopologyAdapterConfiguration{mockAdapter, mockAdapter}},
			wantErr: "assigned to both allocation adapters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewAdapterRegistry(test.config)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("NewAdapterRegistry() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewAdapterRegistry() error = %v", err)
			}
			if !test.wantRegistry {
				if registry != nil {
					t.Fatalf("NewAdapterRegistry() = %#v, want no registry", registry)
				}
				return
			}
			if _, found := registry.HardTopologyAdapter(provider.ID, "nvidia.com/gpu", provider.IdentityNamespace); !found {
				t.Fatal("HardTopologyAdapter() found = false, want the mock adapter to match the provider identity")
			}
		})
	}
}
