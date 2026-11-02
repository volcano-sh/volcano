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

package pod

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestCalculateExtendResources(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want []Resources
	}{
		{
			name: "one container without cpu request, one container without memory limit",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(5, resource.DecimalSI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(500, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(100, resource.BinarySI),
								},
							},
						},
						{
							Name: "container-2",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(200, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-memory": *resource.NewQuantity(100, resource.BinarySI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{
					{
						Name:        "container-1",
						ContainerID: fmt.Sprintf("containerd://%s", "111"),
					},
					{
						Name:        "container-2",
						ContainerID: fmt.Sprintf("docker://%s", "222"),
					},
				}},
			},
			want: []Resources{
				// container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.shares",
					Value:           512,
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.cfs_quota_us",
					Value:           1000,
				},

				// container-2
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://222",
					SubPath:         "cpu.cfs_quota_us",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "docker://222",
					SubPath:         "memory.limit_in_bytes",
					Value:           200,
				},

				// pod
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.shares",
					Value:           512,
				},
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.cfs_quota_us",
					Value:           101000,
				},
			},
		},
		{
			name: "no extend resources",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"cpu": *resource.NewQuantity(5, resource.DecimalSI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"cpu":    *resource.NewQuantity(500, resource.DecimalSI),
									"memory": *resource.NewQuantity(100, resource.BinarySI),
								},
							},
						},
						{
							Name: "container-2",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"memory": *resource.NewQuantity(200, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"memory": *resource.NewQuantity(100, resource.BinarySI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{ContainerStatuses: []v1.ContainerStatus{
					{
						Name:        "container-1",
						ContainerID: fmt.Sprintf("containerd://%s", "111"),
					},
					{
						Name:        "container-2",
						ContainerID: fmt.Sprintf("docker://%s", "222"),
					},
				}},
			},
			want: []Resources{},
		},
		{
			name: "both init and app containers with custom resources",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(2000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(400, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(1000, resource.DecimalSI),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(200, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(500, resource.DecimalSI),
								},
							},
						},
						{
							Name: "container-2",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(500, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(200, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(300, resource.DecimalSI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "container-1",
							ContainerID: "containerd://111",
						},
						{
							Name:        "container-2",
							ContainerID: "docker://222",
						},
					},
					InitContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "init-container-1",
							ContainerID: "docker://000",
						},
					},
				},
			},
			want: []Resources{
				// init-container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://000",
					SubPath:         "cpu.shares",
					Value:           1024,
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://000",
					SubPath:         "cpu.cfs_quota_us",
					Value:           200000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "docker://000",
					SubPath:         "memory.limit_in_bytes",
					Value:           400,
				},
				// container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.shares",
					Value:           512,
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.cfs_quota_us",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "containerd://111",
					SubPath:         "memory.limit_in_bytes",
					Value:           200,
				},
				// container-2
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://222",
					SubPath:         "cpu.shares",
					Value:           307,
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://222",
					SubPath:         "cpu.cfs_quota_us",
					Value:           50000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "docker://222",
					SubPath:         "memory.limit_in_bytes",
					Value:           200,
				},
				// pod level
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.shares",
					Value:           1024, // max(512+307, 1024) = 1024
				},
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.cfs_quota_us",
					Value:           200000, // max(100000+50000, 200000) = 200000
				},
				{
					CgroupSubSystem: "memory",
					SubPath:         "memory.limit_in_bytes",
					Value:           400, // max(200+200, 400) = 400
				},
			},
		},
		{
			name: "init container ID found in InitContainerStatuses",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(100, resource.BinarySI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					InitContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "init-container-1",
							ContainerID: "containerd://init-id-123",
						},
					},
				},
			},
			want: []Resources{
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://init-id-123",
					SubPath:         "cpu.cfs_quota_us",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "containerd://init-id-123",
					SubPath:         "memory.limit_in_bytes",
					Value:           100,
				},
				// pod level (no app containers, so pod level equals init max)
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.shares",
					Value:           2, // minShares
				},
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.cfs_quota_us",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					SubPath:         "memory.limit_in_bytes",
					Value:           100,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateExtendResources(tt.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CalculateExtendResources() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateExtendResourcesV2(t *testing.T) {
	tests := []struct {
		name string
		pod  *v1.Pod
		want []Resources
	}{
		{
			name: "both init and app containers with custom resources",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(2000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(400, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(1000, resource.DecimalSI),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(200, resource.BinarySI),
								},
								Requests: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu": *resource.NewQuantity(500, resource.DecimalSI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "container-1",
							ContainerID: "containerd://111",
						},
					},
					InitContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "init-container-1",
							ContainerID: "docker://000",
						},
					},
				},
			},
			want: []Resources{
				// init container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://000",
					SubPath:         "cpu.weight",
					Value:           100, // 1000 / 10
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "docker://000",
					SubPath:         "cpu.max",
					Value:           200000, // 2000 * 100000 / 1000
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "docker://000",
					SubPath:         "memory.max",
					Value:           400,
				},
				// container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.weight",
					Value:           50, // 500 / 10
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.max",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "containerd://111",
					SubPath:         "memory.max",
					Value:           200,
				},
				// pod level
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.weight",
					Value:           100, // max(50, 100)
				},
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.max",
					Value:           200000, // max(100000, 200000)
				},
				{
					CgroupSubSystem: "memory",
					SubPath:         "memory.max",
					Value:           400, // max(200, 400)
				},
			},
		},
		{
			name: "init container with unlimited CPU and memory limits (value 0)",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-container-unlimited",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(0, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(0, resource.BinarySI),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: map[v1.ResourceName]resource.Quantity{
									"kubernetes.io/batch-cpu":    *resource.NewQuantity(1000, resource.DecimalSI),
									"kubernetes.io/batch-memory": *resource.NewQuantity(200, resource.BinarySI),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "container-1",
							ContainerID: "containerd://111",
						},
					},
					InitContainerStatuses: []v1.ContainerStatus{
						{
							Name:        "init-container-unlimited",
							ContainerID: "containerd://init-unlimited",
						},
					},
				},
			},
			want: []Resources{
				// init-container-unlimited
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://init-unlimited",
					SubPath:         "cpu.max",
					Value:           -1,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "containerd://init-unlimited",
					SubPath:         "memory.max",
					Value:           -1,
				},
				// container-1
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "containerd://111",
					SubPath:         "cpu.max",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "containerd://111",
					SubPath:         "memory.max",
					Value:           200,
				},
				// pod level
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.weight",
					Value:           100, // minWeight
				},
				{
					CgroupSubSystem: "cpu",
					SubPath:         "cpu.max",
					Value:           -1, // init is unlimited (-1) so pod-level is unlimited (-1)
				},
				{
					CgroupSubSystem: "memory",
					SubPath:         "memory.max",
					Value:           -1, // init is unlimited (-1) so pod-level is unlimited (-1)
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateExtendResourcesV2(tt.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CalculateExtendResourcesV2() = %v, want %v", got, tt.want)
			}
		})
	}
}
