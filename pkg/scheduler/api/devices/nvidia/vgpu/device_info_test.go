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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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

func TestAddResource(t *testing.T) {
	testCases := []struct {
		name        string
		annotations map[string]string
		pod         *v1.Pod
		devices     map[int]*GPUDevice
		sharing     SharingFactory
		wantAdded   bool
	}{
		{
			name: "Pod assigned to known GPU UUID",
			annotations: map[string]string{
				AssignedIDsAnnotations: "GPU-1234,NVIDIA,16384,0",
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:  types.UID("pod-abc"),
					Name: "test-pod",
				},
			},
			devices: map[int]*GPUDevice{
				0: {
					UUID:   "GPU-1234",
					PodMap: map[string]*GPUUsage{},
				},
			},
			sharing:   sharingRegistry["hami-core"],
			wantAdded: true,
		},
		{
			name: "Pod assigned to unknown GPU UUID",
			annotations: map[string]string{
				AssignedIDsAnnotations: "GPU-5678,NVIDIA,16384,0",
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:  types.UID("pod-def"),
					Name: "test-pod2",
				},
			},
			devices: map[int]*GPUDevice{
				0: {
					UUID:   "GPU-1234",
					PodMap: map[string]*GPUUsage{},
				},
			},
			sharing:   sharingRegistry["hami-core"],
			wantAdded: false,
		},
		{
			name:        "Pod without AssignedIDsAnnotations",
			annotations: map[string]string{},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:  types.UID("pod-xyz"),
					Name: "test-pod3",
				},
			},
			devices: map[int]*GPUDevice{
				0: {
					UUID:   "GPU-1234",
					PodMap: map[string]*GPUUsage{},
				},
			},
			sharing:   sharingRegistry["hami-core"],
			wantAdded: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := &GPUDevices{
				Device:  tc.devices,
				Sharing: tc.sharing,
			}

			gs.addResource(tc.annotations, tc.pod)

			found := false
			for _, d := range gs.Device {
				if _, ok := d.PodMap[string(tc.pod.UID)]; ok {
					found = true
					break
				}
			}

			if found != tc.wantAdded {
				t.Errorf("expected pod added: %v, got: %v", tc.wantAdded, found)
			}
		})
	}
}
