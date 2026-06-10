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

const gpuResource = v1.ResourceName("nvidia.com/gpu")

func newJobWithMinResources(minResources v1.ResourceList) *api.JobInfo {
	return &api.JobInfo{
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					MinResources: &minResources,
				},
			},
		},
	}
}

// TestGetInqueueResource verifies that reserved (in-queue) resources are computed
// using the canonical unit model. In particular, extended/scalar resources such as
// GPUs are stored in milli-units (see api.NewResource), so MinResources for those
// must be converted to milli-units to be consistent with job.Allocated. The
// previous implementation used Quantity.Value() for scalars, which under-counted
// reserved GPUs by a factor of 1000 once any pod had been allocated.
func TestGetInqueueResource(t *testing.T) {
	tests := []struct {
		name      string
		minRes    v1.ResourceList
		allocated *api.Resource
		expected  *api.Resource
	}{
		{
			name: "nothing allocated yet: full request reserved",
			minRes: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4096"),
				gpuResource:       resource.MustParse("8"),
			},
			allocated: api.EmptyResource(),
			expected: &api.Resource{
				MilliCPU: 4000,
				Memory:   4096,
				// 8 GPUs == 8000 milli-units, matching NewResource.
				ScalarResources: map[v1.ResourceName]float64{gpuResource: 8000},
			},
		},
		{
			name: "partial GPU allocation: remainder reserved in milli-units",
			minRes: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4096"),
				gpuResource:       resource.MustParse("8"),
			},
			// allocated is built like job.Allocated (via NewResource), i.e. milli-units.
			allocated: &api.Resource{
				MilliCPU:        1000,
				Memory:          1024,
				ScalarResources: map[v1.ResourceName]float64{gpuResource: 4000},
			},
			expected: &api.Resource{
				MilliCPU: 3000,
				Memory:   3072,
				// 8000 - 4000 == 4000 milli (4 GPUs) still reserved.
				ScalarResources: map[v1.ResourceName]float64{gpuResource: 4000},
			},
		},
		{
			name: "GPU fully allocated: no scalar reserved",
			minRes: v1.ResourceList{
				gpuResource: resource.MustParse("2"),
			},
			allocated: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{gpuResource: 2000},
			},
			expected: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{},
			},
		},
		{
			// "pods" is a scalar resource stored in whole units (not milli-units),
			// so its reservation must be computed without any 1000x conversion:
			// 10 requested - 4 allocated == 6 reserved.
			name: "partial pods allocation: remainder reserved in whole units",
			minRes: v1.ResourceList{
				v1.ResourceCPU:  resource.MustParse("4"),
				v1.ResourcePods: resource.MustParse("10"),
			},
			allocated: &api.Resource{
				MilliCPU:        1000,
				ScalarResources: map[v1.ResourceName]float64{v1.ResourcePods: 4},
			},
			expected: &api.Resource{
				MilliCPU:        3000,
				ScalarResources: map[v1.ResourceName]float64{v1.ResourcePods: 6},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := newJobWithMinResources(test.minRes)
			got := GetInqueueResource(job, test.allocated)

			if got.MilliCPU != test.expected.MilliCPU {
				t.Errorf("MilliCPU = %v, expected %v", got.MilliCPU, test.expected.MilliCPU)
			}
			if got.Memory != test.expected.Memory {
				t.Errorf("Memory = %v, expected %v", got.Memory, test.expected.Memory)
			}
			for name, want := range test.expected.ScalarResources {
				if got.ScalarResources[name] != want {
					t.Errorf("ScalarResources[%s] = %v, expected %v", name, got.ScalarResources[name], want)
				}
			}
		})
	}
}
