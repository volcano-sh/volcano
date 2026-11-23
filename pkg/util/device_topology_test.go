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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestDecodeDeviceTopologyAnnotation(t *testing.T) {
	tests := []struct {
		name         string
		rawPolicy    string
		want         *schedulingv1beta1.DeviceTopologySpec
		wantContains string
	}{
		{
			name: "empty value has no policy",
		},
		{
			name:      "hard local and soft fabric policy",
			rawPolicy: `{"requirements":[{"resource":{"extendedResourceName":"nvidia.com/gpu"},"mode":"hard"}],"fabric":{"mode":"soft"}}`,
			want: &schedulingv1beta1.DeviceTopologySpec{
				Requirements: []schedulingv1beta1.DeviceTopologyRequirement{{
					Resource: schedulingv1beta1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
					Mode:     schedulingv1beta1.HardDeviceTopologyMode,
				}},
				Fabric: &schedulingv1beta1.DeviceFabricAffinity{Mode: schedulingv1beta1.SoftDeviceTopologyMode},
			},
		},
		{
			name:         "value that is not JSON",
			rawPolicy:    "hard",
			wantContains: "must contain a valid device topology policy",
		},
		{
			name:         "unknown policy field",
			rawPolicy:    `{"requirements":[],"allocationStrategy":"Compact"}`,
			wantContains: "must contain a valid device topology policy",
		},
		{
			name:         "two policies",
			rawPolicy:    `{"requirements":[]} {"requirements":[]}`,
			wantContains: "must contain exactly one device topology policy",
		},
		{
			name:         "policy followed by text",
			rawPolicy:    `{"requirements":[]} hard`,
			wantContains: "must contain exactly one device topology policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeDeviceTopologyAnnotation(tt.rawPolicy)
			if tt.wantContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantContains) {
					t.Fatalf("expected error containing %q, got %v", tt.wantContains, err)
				}
				if got != nil {
					t.Fatalf("expected no policy with an error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if !equality.Semantic.DeepEqual(got, tt.want) {
				t.Fatalf("expected policy %+v, got %+v", tt.want, got)
			}
		})
	}
}
