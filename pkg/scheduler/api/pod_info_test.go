/*
Copyright 2019 The Kubernetes Authors.

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

package api

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
)

func TestGetPodResourceRequest(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		expectedResource *Resource
	}{
		{
			name: "get resource for pod without init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: buildResource("3000m", "2G", map[string]string{"pods": "1"}, 0),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: buildResource("3000m", "5G", map[string]string{"pods": "1"}, 0),
		},
	}

	for i, test := range tests {
		req := GetPodResourceRequest(test.pod)
		if !reflect.DeepEqual(req, test.expectedResource) {
			t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
				i, test.name, test.expectedResource, req)
		}
	}
}

func TestGetPodResourceWithoutInitContainers(t *testing.T) {
	tests := []struct {
		name             string
		pod              *v1.Pod
		expectedResource *Resource
	}{
		{
			name: "get resource for pod without init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(BuildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(BuildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with overhead",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: BuildResourceList("2000m", "1G"),
							},
						},
					},
					Overhead: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("500m"),
						v1.ResourceMemory: resource.MustParse("1G"),
					},
				},
			},
			expectedResource: NewResource(BuildResourceList("3500m", "3G")),
		},
	}

	for i, test := range tests {
		req := GetPodResourceWithoutInitContainers(test.pod)
		if !reflect.DeepEqual(req, test.expectedResource) {
			t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
				i, test.name, test.expectedResource, req)
		}
	}
}

func TestGetGPUIndex(t *testing.T) {
	testCases := []struct {
		name string
		pod  *v1.Pod
		want []int
	}{
		{
			name: "pod without GPUIndex annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			want: nil,
		},
		{
			name: "pod with empty GPUIndex annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{GPUIndex: ""},
				},
			},
			want: nil,
		},
		{
			name: "pod with single GPUIndex annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{GPUIndex: "0"},
				},
			},
			want: []int{0},
		},
		{
			name: "pod with multiple GPUIndexes annotation",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{GPUIndex: "0,1,3"},
				},
			},
			want: []int{0, 1, 3},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := gpushare.GetGPUIndex(tc.pod)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Unexpected result (-want +got):\n%s", diff)
			}
		})
	}
}
