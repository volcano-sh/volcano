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

package gpushare

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
									VolcanoGPUResource: resource.MustParse("1"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUResource: resource.MustParse("3"),
								},
							},
						},
					},
				},
			},
			want: 4,
		},
		{
			name: "GPUs required both in initContainers and Containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUResource: resource.MustParse("1"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUResource: resource.MustParse("3"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUResource: resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			want: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getGPUMemoryOfPod(tc.pod)
			if tc.want != got {
				t.Errorf("unexpected result, want: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestGetGPUNumberOfPod(t *testing.T) {
	testCases := []struct {
		name string
		pod  *v1.Pod
		want int
	}{
		{
			name: "GPUs required only in Containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUNumber: resource.MustParse("1"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUNumber: resource.MustParse("3"),
								},
							},
						},
					},
				},
			},
			want: 4,
		},
		{
			name: "GPUs required both in initContainers and Containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUNumber: resource.MustParse("1"),
								},
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUNumber: resource.MustParse("3"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									VolcanoGPUNumber: resource.MustParse("2"),
								},
							},
						},
					},
				},
			},
			want: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := getGPUNumberOfPod(tc.pod)
			if tc.want != got {
				t.Errorf("unexpected result, want: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestReleaseReturnsRemovedAnnotationKeys(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-0",
			Namespace: "default",
			UID:       "pod-uid",
			Annotations: map[string]string{
				PredicateTime: "1700000000",
				GPUIndex:      "0",
			},
		},
	}
	client := fake.NewSimpleClientset(pod)
	gs := &GPUDevices{
		Name: "node-a",
		Device: map[int]*GPUDevice{
			0: {
				ID:     0,
				PodMap: map[string]*v1.Pod{string(pod.UID): pod},
			},
		},
	}

	reservation, err := gs.Release(client, pod)
	if err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if _, ok := gs.Device[0].PodMap[string(pod.UID)]; ok {
		t.Fatalf("Release should remove pod from GPU device map")
	}
	if reservation == nil || reservation.Annotations == nil {
		t.Fatalf("Release should return annotation keys removed from TaskInfo.PodAnnotations")
	}
	if _, ok := reservation.Annotations[GPUIndex]; !ok {
		t.Fatalf("reservation should request deletion of annotation %s", GPUIndex)
	}
	if _, ok := reservation.Annotations[PredicateTime]; !ok {
		t.Fatalf("reservation should request deletion of annotation %s", PredicateTime)
	}
}
