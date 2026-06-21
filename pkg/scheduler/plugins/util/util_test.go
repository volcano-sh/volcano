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

package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const gpuResourceName = v1.ResourceName("nvidia.com/gpu")

func newJobWithMinResources(rl v1.ResourceList) *api.JobInfo {
	return &api.JobInfo{
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					MinResources: &rl,
				},
			},
		},
	}
}

// TestGetInqueueResourceScalarUnits guards against a unit-mismatch regression:
// api.Resource stores scalar resources (e.g. GPU, hugepages) in milli-units
// (NewResource uses MilliValue()), and allocated.ScalarResources is therefore in
// milli-units. GetInqueueResource must produce scalar values in the same milli-unit
// scale; otherwise queue inqueue accounting for scalar resources is off by 1000x.
func TestGetInqueueResourceScalarUnits(t *testing.T) {
	tests := []struct {
		name        string
		minRes      v1.ResourceList
		allocated   *api.Resource
		wantCPU     float64
		wantMem     float64
		wantScalars map[v1.ResourceName]float64
	}{
		{
			name: "gpu with nothing allocated is reported in milli-units",
			minRes: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("8Gi"),
				gpuResourceName:   resource.MustParse("8"),
			},
			allocated: api.EmptyResource(),
			wantCPU:   4000,
			wantMem:   float64(8 * 1024 * 1024 * 1024),
			wantScalars: map[v1.ResourceName]float64{
				// 8 GPUs == 8000 milli-units, matching api.NewResource.
				gpuResourceName: 8000,
			},
		},
		{
			name: "gpu partially allocated subtracts in consistent milli-units",
			minRes: v1.ResourceList{
				gpuResourceName: resource.MustParse("8"),
			},
			// 2 GPUs already allocated == 2000 milli-units.
			allocated: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{
					gpuResourceName: 2000,
				},
			},
			wantScalars: map[v1.ResourceName]float64{
				// (8000 - 2000) milli-units still reserved.
				gpuResourceName: 6000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := newJobWithMinResources(tt.minRes)
			got := GetInqueueResource(job, tt.allocated)

			if got.MilliCPU != tt.wantCPU {
				t.Errorf("MilliCPU = %v, want %v", got.MilliCPU, tt.wantCPU)
			}
			if tt.wantMem != 0 && got.Memory != tt.wantMem {
				t.Errorf("Memory = %v, want %v", got.Memory, tt.wantMem)
			}
			for name, want := range tt.wantScalars {
				if gotVal := got.ScalarResources[name]; gotVal != want {
					t.Errorf("ScalarResources[%s] = %v, want %v", name, gotVal, want)
				}
			}
		})
	}
}
