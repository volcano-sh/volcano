/*
Copyright 2023 The Volcano Authors.

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

package vgpu

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

func TestGetGPUMemoryOfPod(t *testing.T) {
	testCases := []struct {
		name string
		pod  *v1.Pod
		want uint
	}{
		{
			name: "GPUs required only in Containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									config.VolcanoVGPUNumber: resource.MustParse("1"),
									config.VolcanoVGPUMemory: resource.MustParse("3000"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									config.VolcanoVGPUNumber: resource.MustParse("3"),
									config.VolcanoVGPUMemory: resource.MustParse("5000"),
								},
							},
						},
					},
				},
			},
			want: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := resourcereqs(tc.pod)
			if got[0].Nums != 1 || got[0].Memreq != 3000 || got[1].Nums != 3 || got[1].Memreq != 5000 {
				t.Errorf("unexpected result, got: %v", got)
			}
		})
	}
}
