/*
Copyright 2024 The Volcano Authors.

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

package loadaware

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/descheduler/pkg/api"
)

func TestSetDefaults_LowNodeUtilizationArgs(t *testing.T) {
	tests := []struct {
		name string
		in   runtime.Object
		want runtime.Object
	}{
		{
			name: "LowNodeUtilizationArgs empty",
			in:   &LoadAwareUtilizationArgs{},
			want: &LoadAwareUtilizationArgs{
				UseDeviationThresholds: false,
				Thresholds:             nil,
				TargetThresholds:       nil,
				NumberOfNodes:          0,
			},
		},
		{
			name: "LowNodeUtilizationArgs with value",
			in: &LoadAwareUtilizationArgs{
				UseDeviationThresholds: true,
				Thresholds: api.ResourceThresholds{
					v1.ResourceCPU:    20,
					v1.ResourceMemory: 120,
				},
				TargetThresholds: api.ResourceThresholds{
					v1.ResourceCPU:    80,
					v1.ResourceMemory: 80,
				},
				NumberOfNodes: 10,
			},
			want: &LoadAwareUtilizationArgs{
				UseDeviationThresholds: true,
				Thresholds: api.ResourceThresholds{
					v1.ResourceCPU:    20,
					v1.ResourceMemory: 120,
				},
				TargetThresholds: api.ResourceThresholds{
					v1.ResourceCPU:    80,
					v1.ResourceMemory: 80,
				},
				NumberOfNodes: 10,
			},
		},
	}
	for _, tc := range tests {
		scheme := runtime.NewScheme()
		utilruntime.Must(AddToScheme(scheme))
		t.Run(tc.name, func(t *testing.T) {
			scheme.Default(tc.in)
			if diff := cmp.Diff(tc.in, tc.want); diff != "" {
				t.Errorf("Got unexpected defaults (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestSetDefaults_HighNodeUtilizationArgs(t *testing.T) {
	tests := []struct {
		name string
		in   runtime.Object
		want runtime.Object
	}{
		{
			name: "HighNodeUtilizationArgs empty",
			in:   &LoadAwareUtilizationArgs{},
			want: &LoadAwareUtilizationArgs{
				Thresholds:    nil,
				NumberOfNodes: 0,
			},
		},
		{
			name: "HighNodeUtilizationArgs with value",
			in: &LoadAwareUtilizationArgs{
				Thresholds: api.ResourceThresholds{
					v1.ResourceCPU:    20,
					v1.ResourceMemory: 120,
				},
				NumberOfNodes: 10,
			},
			want: &LoadAwareUtilizationArgs{
				Thresholds: api.ResourceThresholds{
					v1.ResourceCPU:    20,
					v1.ResourceMemory: 120,
				},
				NumberOfNodes: 10,
			},
		},
	}
	for _, tc := range tests {
		scheme := runtime.NewScheme()
		utilruntime.Must(AddToScheme(scheme))
		t.Run(tc.name, func(t *testing.T) {
			scheme.Default(tc.in)
			if diff := cmp.Diff(tc.in, tc.want); diff != "" {
				t.Errorf("Got unexpected defaults (-want, +got):\n%s", diff)
			}
		})
	}
}
