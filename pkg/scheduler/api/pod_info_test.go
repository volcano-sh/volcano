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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
								Requests: buildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(buildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(buildResourceList("3000m", "5G")),
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
								Requests: buildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(buildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with init containers",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "5G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
				},
			},
			expectedResource: NewResource(buildResourceList("3000m", "2G")),
		},
		{
			name: "get resource for pod with overhead",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("1000m", "1G"),
							},
						},
						{
							Resources: v1.ResourceRequirements{
								Requests: buildResourceList("2000m", "1G"),
							},
						},
					},
					Overhead: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("500m"),
						v1.ResourceMemory: resource.MustParse("1G"),
					},
				},
			},
			expectedResource: NewResource(buildResourceList("3500m", "3G")),
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
