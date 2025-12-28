/*
Copyright 2021 The Volcano Authors.

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

package helpers

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestMax(t *testing.T) {
	l := &api.Resource{
		MilliCPU: 1,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"gpu":    1,
			"common": 4,
		},
	}
	r := &api.Resource{
		MilliCPU: 2,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"npu":    2,
			"common": 5,
		},
	}
	expected := &api.Resource{
		MilliCPU: 2,
		Memory:   1024,
		ScalarResources: map[v1.ResourceName]float64{
			"gpu":    1,
			"npu":    2,
			"common": 5,
		},
	}
	re := Max(l, r)
	if !equality.Semantic.DeepEqual(expected, re) {
		t.Errorf("expected: %#v, got: %#v", expected, re)
	}
}

func TestIsPodSpecMatch(t *testing.T) {
	cpu500m := resource.MustParse("500m")
	cpu250m := resource.MustParse("250m")
	mem128Mi := resource.MustParse("128Mi")

	tests := []struct {
		name        string
		specA       *v1.PodSpec
		specB       *v1.PodSpec
		expectMatch bool
	}{
		{
			name: "Same spec",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "c1",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: cpu500m,
							},
						},
					},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "c1",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: cpu500m,
							},
						},
					},
				},
			},
			expectMatch: true,
		},
		{
			name: "Different image",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{Name: "c1", Image: "busybox"},
				},
			},
			expectMatch: false,
		},
		{
			name: "Container Resources different",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "c1",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: cpu500m,
							},
						},
					},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "c1",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: cpu250m,
							},
						},
					},
				},
			},
			expectMatch: false,
		},
		{
			name: "The order of containers is different",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "a",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceMemory: mem128Mi,
							},
						},
					},
					{
						Name:  "b",
						Image: "busybox",
					},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{
						Name:  "b",
						Image: "busybox",
					},
					{
						Name:  "a",
						Image: "nginx",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceMemory: mem128Mi,
							},
						},
					},
				},
			},
			expectMatch: true,
		},
		{
			name: "Different schedulerName",
			specA: &v1.PodSpec{
				SchedulerName: "custom-scheduler",
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			expectMatch: false,
		},
		{
			name: "Different tolerations",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Tolerations: []v1.Toleration{
					{Key: "key1", Operator: v1.TolerationOpExists},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Tolerations: []v1.Toleration{
					{Key: "key2", Operator: v1.TolerationOpExists},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			expectMatch: false,
		},
		{
			name: "Different affinity",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Affinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{Key: "zone", Operator: v1.NodeSelectorOpIn, Values: []string{"zoneA"}},
									},
								},
							},
						},
					},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Affinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{Key: "zone", Operator: v1.NodeSelectorOpIn, Values: []string{"zoneB"}},
									},
								},
							},
						},
					},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			expectMatch: false,
		},
		{
			name: "Tolerations and Affinity are same",
			specA: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Tolerations: []v1.Toleration{
					{Key: "key1", Operator: v1.TolerationOpExists},
				},
				Affinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{Key: "zone", Operator: v1.NodeSelectorOpIn, Values: []string{"zoneA"}},
									},
								},
							},
						},
					},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			specB: &v1.PodSpec{
				SchedulerName: "default-scheduler",
				Tolerations: []v1.Toleration{
					{Key: "key1", Operator: v1.TolerationOpExists},
				},
				Affinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{Key: "zone", Operator: v1.NodeSelectorOpIn, Values: []string{"zoneA"}},
									},
								},
							},
						},
					},
				},
				Containers: []v1.Container{
					{Name: "c1", Image: "nginx"},
				},
			},
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := IsPodSpecMatch(tt.specA, tt.specB)
			if match != tt.expectMatch {
				t.Errorf("Expected match = %v, got %v", tt.expectMatch, match)
			}
		})
	}
}
