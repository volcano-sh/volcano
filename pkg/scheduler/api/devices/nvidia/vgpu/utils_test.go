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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_resourcereqs(t *testing.T) {
	type args struct {
		pod *v1.Pod
	}
	tests := []struct {
		name string
		args args
		want []ContainerDeviceRequest
	}{
		{
			name: "only-memory",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"volcano.sh/vgpu-number": resource.MustParse("1"),
										"volcano.sh/vgpu-memory": resource.MustParse("1000"),
									},
								},
							},
						},
					},
				},
			},
			want: []ContainerDeviceRequest{
				{Nums: 1, Type: "NVIDIA", Memreq: 1000, Coresreq: 0, MemPercentagereq: 101},
			},
		},
		{
			name: "memory-and-cores",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"volcano.sh/vgpu-number": resource.MustParse("1"),
										"volcano.sh/vgpu-memory": resource.MustParse("1000"),
										"volcano.sh/vgpu-cores":  resource.MustParse("50"),
									},
								},
							},
						},
					},
				},
			},
			want: []ContainerDeviceRequest{
				{Nums: 1, Type: "NVIDIA", Memreq: 1000, Coresreq: 50, MemPercentagereq: 101},
			},
		},
		{
			name: "memoryprecentage-and-cores",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"volcano.sh/vgpu-number":            resource.MustParse("1"),
										"volcano.sh/vgpu-memory-percentage": resource.MustParse("20"),
										"volcano.sh/vgpu-cores":             resource.MustParse("50"),
									},
								},
							},
						},
					},
				},
			},
			want: []ContainerDeviceRequest{
				{Nums: 1, Type: "NVIDIA", Memreq: 0, Coresreq: 50, MemPercentagereq: 20},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resourcereqs(tt.args.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resourcereqs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_checkNodeGPUSharingPredicate(t *testing.T) {
	type args struct {
		pod       *v1.Pod
		gssnap    *GPUDevices
		replicate bool
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		want1   []ContainerDevices
		wantErr bool
	}{
		{
			name: "fit-empty-gpu",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("1"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("60"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 1000},
					},
				},
				replicate: false,
			},
			want: true,
			want1: []ContainerDevices{
				{{UUID: "UUID0", Type: "NVIDIA", Usedmem: 60, Usedcores: 50}},
			},
		},
		{
			name: "allocate-memory-percent",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber:           resource.MustParse("1"),
						VolcanoVGPUCores:            resource.MustParse("50"),
						VolcanoVGPUMemoryPercentage: resource.MustParse("88"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 1000},
					},
				},
				replicate: false,
			},
			want: true,
			want1: []ContainerDevices{
				{{UUID: "UUID0", Type: "NVIDIA", Usedmem: 880, Usedcores: 50}},
			},
		},
		{
			name: "allocate-memory-percent",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber:           resource.MustParse("1"),
						VolcanoVGPUCores:            resource.MustParse("50"),
						VolcanoVGPUMemoryPercentage: resource.MustParse("88"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 1000, UsedNum: 500},
					},
				},
				replicate: false,
			},
			want:    false,
			want1:   []ContainerDevices{},
			wantErr: true,
		},
		{
			name: "allocate-memory-percent-2card",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber:           resource.MustParse("1"),
						VolcanoVGPUCores:            resource.MustParse("50"),
						VolcanoVGPUMemoryPercentage: resource.MustParse("88"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 1000, UsedNum: 500},
						1: {ID: 1, UUID: "UUID1", Number: 4, Type: "NVIDIA", Memory: 1000},
					},
				},
				replicate: false,
			},
			want: true,
			want1: []ContainerDevices{
				{{UUID: "UUID1", Type: "NVIDIA", Usedmem: 880, Usedcores: 50}},
			},
		},
		{
			name: "allocate-memory-2card",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("1"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("2000"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 4000, UsedNum: 3000},
						1: {ID: 1, UUID: "UUID1", Number: 4, Type: "NVIDIA", Memory: 4000},
					},
				},
				replicate: false,
			},
			want: true,
			want1: []ContainerDevices{
				{{UUID: "UUID1", Type: "NVIDIA", Usedmem: 2000, Usedcores: 50}},
			},
			wantErr: false,
		},
		{
			name: "memory-not-enough",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("1"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("2000"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 4000, UsedNum: 3000},
						1: {ID: 1, UUID: "UUID1", Number: 4, Type: "NVIDIA", Memory: 4000, UsedMem: 3000},
					},
				},
				replicate: false,
			},
			want:    false,
			want1:   []ContainerDevices{},
			wantErr: true,
		},
		{
			name: "core-not-enough",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("1"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("2000"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 8000, UsedCore: 80},
					},
				},
				replicate: false,
			},
			want:    false,
			want1:   []ContainerDevices{},
			wantErr: true,
		},
		{
			name: "card-not-enough",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("2"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("2000"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "NVIDIA", Memory: 8000, UsedCore: 80},
					},
				},
				replicate: false,
			},
			want:    false,
			want1:   []ContainerDevices{},
			wantErr: true,
		},
		{
			name: "device-type-check",
			args: args{
				pod: &v1.Pod{
					Spec: v1.PodSpec{Containers: []v1.Container{{Resources: v1.ResourceRequirements{Limits: v1.ResourceList{
						VolcanoVGPUNumber: resource.MustParse("1"),
						VolcanoVGPUCores:  resource.MustParse("50"),
						VolcanoVGPUMemory: resource.MustParse("60"),
					}}}}},
				},
				gssnap: &GPUDevices{
					Device: map[int]*GPUDevice{
						0: {ID: 0, UUID: "UUID0", Number: 4, Type: "XXX", Memory: 1000},
					},
				},
				replicate: false,
			},
			want:    false,
			want1:   []ContainerDevices{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := checkNodeGPUSharingPredicate(tt.args.pod, tt.args.gssnap, tt.args.replicate)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkNodeGPUSharingPredicate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkNodeGPUSharingPredicate() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("checkNodeGPUSharingPredicate() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
