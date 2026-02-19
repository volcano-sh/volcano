/*
Copyright 2025 The Volcano Authors.

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

package rescheduling

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestSortPods(t *testing.T) {
	createTimeOld := time.Now()
	createTime := createTimeOld.Add(time.Second)

	tests := []struct {
		name  string
		pods  []*corev1.Pod
		wants []*corev1.Pod
	}{
		{
			name:  "pods list empty",
			pods:  []*corev1.Pod{},
			wants: []*corev1.Pod{},
		},
		{
			name: "pods sort by pod priority",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(1)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(2)),
					},
				},
			},
			wants: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(1)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(2)),
					},
				},
			},
		},
		{
			name: "pods sort by pod priority nil",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(1)),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{
						Priority: nil,
					},
				},
			},
			wants: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Priority: ptr.To(int32(1)),
					},
				},
			},
		},
		{
			name: "pods sort by pod Qos",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "C",
					},
					Spec: corev1.PodSpec{},
				},
			},
			wants: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "C",
					},
					Spec: corev1.PodSpec{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "B",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "A",
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "pods sort by pod CreatTime",
			pods: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "A",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "AA",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "B",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "BB",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "C",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "CC",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{},
				},
			},
			wants: []*corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "CC",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "C",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "BB",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "B",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("2500M"),
										corev1.ResourceCPU:    resource.MustParse("250m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "AA",
						CreationTimestamp: metav1.Time{Time: createTime},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "A",
						CreationTimestamp: metav1.Time{Time: createTimeOld},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Resources: corev1.ResourceRequirements{
									Limits: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
									Requests: corev1.ResourceList{
										corev1.ResourceMemory: resource.MustParse("250M"),
										corev1.ResourceCPU:    resource.MustParse("25m"),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortPods(tt.pods)
			assert.Equal(t, tt.wants, tt.pods, "sortPods")
		})
	}
}
