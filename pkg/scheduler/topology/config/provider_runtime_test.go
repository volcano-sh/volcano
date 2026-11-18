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
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestNewProviderRuntime(t *testing.T) {
	validConfiguration := &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{{
		ID:                "node-annotation-inventory",
		Type:              conf.DeviceTopologyProviderTypeAnnotation,
		IdentityNamespace: "nvidia-gpu-uuid-v1",
		Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
		MaxAge:            5 * time.Minute,
	}}}

	tests := []struct {
		name       string
		config     *conf.DeviceTopologyConfiguration
		wantErr    string
		wantSource bool
	}{
		{
			name:       "disabled without configuration",
			wantSource: false,
		},
		{
			name:       "builds annotation provider",
			config:     validConfiguration,
			wantSource: true,
		},
		{
			name: "rejects unsupported source type",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{{
				ID:                "unsupported",
				Type:              "unsupported",
				IdentityNamespace: "example-v1",
				Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
			}}},
			wantErr: "unsupported type",
		},
		{
			name: "rejects duplicate resource ownership",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{
				{
					ID:                "first",
					Type:              conf.DeviceTopologyProviderTypeAnnotation,
					IdentityNamespace: "first-v1",
					Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
				},
				{
					ID:                "second",
					Type:              conf.DeviceTopologyProviderTypeAnnotation,
					IdentityNamespace: "second-v1",
					Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
				},
			}},
			wantErr: "assigned to both topology providers",
		},
		{
			name: "rejects negative max age",
			config: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{{
				ID:                "negative-age",
				Type:              conf.DeviceTopologyProviderTypeAnnotation,
				IdentityNamespace: "example-v1",
				Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
				MaxAge:            -time.Minute,
			}}},
			wantErr: "negative max age",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime, err := NewProviderRuntime(test.config)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("NewProviderRuntime() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewProviderRuntime() error = %v", err)
			}
			if test.wantSource {
				if runtime.Registry == nil || len(runtime.Sources) != 1 || runtime.Sources[0].ID() != "node-annotation-inventory" {
					t.Fatalf("runtime = %#v, want registry and annotation source", runtime)
				}
				update, err := runtime.Sources[0].SyncNode(context.Background(), &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: types.UID("node-a-uid"), ResourceVersion: "1"}})
				if err != nil {
					t.Fatalf("SyncNode() error = %v", err)
				}
				if err := runtime.Registry.ValidateOwnership(update); err != nil {
					t.Fatalf("ValidateOwnership() error = %v", err)
				}
				if maxAge, _ := runtime.Registry.MaxAge("node-annotation-inventory"); maxAge != 5*time.Minute {
					t.Fatalf("Registry.MaxAge() = %s, want the configured 5m", maxAge)
				}
				return
			}
			if runtime.Registry != nil || len(runtime.Sources) != 0 {
				t.Fatalf("runtime = %#v, want disabled topology sources", runtime)
			}
		})
	}
}
