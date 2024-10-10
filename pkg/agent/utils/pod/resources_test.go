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
					ContainerID:     "111",
					SubPath:         "cpu.shares",
					Value:           512,
				},
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "111",
					SubPath:         "cpu.cfs_quota_us",
					Value:           1000,
				},

				// container-2
				{
					CgroupSubSystem: "cpu",
					ContainerID:     "222",
					SubPath:         "cpu.cfs_quota_us",
					Value:           100000,
				},
				{
					CgroupSubSystem: "memory",
					ContainerID:     "222",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateExtendResources(tt.pod); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CalculateExtendResources() = %v, want %v", got, tt.want)
			}
		})
	}
}
