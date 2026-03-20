/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2024 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced test coverage for pod information handling

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
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	k8sfeature "k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
)

func TestGetPodResourceRequest(t *testing.T) {
	restartAlways := v1.ContainerRestartPolicyAlways
	tests := []struct {
		name                     string
		podLevelResourcesEnabled bool
		pod                      *v1.Pod
		expectedResource         *Resource
	}{
		{
			name:                     "get resource for pod without init containers",
			podLevelResourcesEnabled: false,
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
			name:                     "get resource for pod with init containers",
			podLevelResourcesEnabled: false,
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
		// test case with restartable containers, mainly derived from k8s.io/kubernetes/pkg/api/v1/resource/helpers_test.go#TestPodResourceRequests
		{
			name: "restartable init container",
			// restartable init + regular container
			expectedResource:         buildResource("2", "0", map[string]string{"pods": "1"}, 0),
			podLevelResourcesEnabled: false,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},

					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple restartable init containers",
			// max(5, restartable init containers(3+2+1) + regular(1)) = 7
			expectedResource:         buildResource("7", "0", map[string]string{"pods": "1"}, 0),
			podLevelResourcesEnabled: false,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("5"),
								},
							},
						},
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
						{
							Name:          "restartable-init-2",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("2"),
								},
							},
						},
						{
							Name:          "restartable-init-3",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("3"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple restartable and regular init containers",
			// init-2 requires 5 + the previously running restartable init
			// containers(1+2) = 8, the restartable init container that starts
			// after it doesn't count
			expectedResource:         buildResource("8", "0", map[string]string{"pods": "1"}, 0),
			podLevelResourcesEnabled: false,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name: "init-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("5"),
								},
							},
						},
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
						{
							Name:          "restartable-init-2",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("2"),
								},
							},
						},
						{
							Name: "init-2",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("5"),
								},
							},
						},
						{
							Name:          "restartable-init-3",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("3"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
		},
		{
			name:                     "restartable-init, init and regular",
			expectedResource:         buildResource("210", "0", map[string]string{"pods": "1"}, 0),
			podLevelResourcesEnabled: true,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					InitContainers: []v1.Container{
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("10"),
								},
							},
						},
						{
							Name: "init-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("200"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("100"),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "restartable-init, init and regular containers with pod-level resources",
			expectedResource: buildResource("250", "100Mi",
				map[string]string{"pods": "1", v1.ResourceHugePagesPrefix + "1Mi": "2Gi", v1.ResourceHugePagesPrefix + "1Ki": "2Gi"}, 0),
			podLevelResourcesEnabled: true,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:                     resource.MustParse("250"),
							v1.ResourceMemory:                  resource.MustParse("100Mi"),
							v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
							v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
						},
					},
					InitContainers: []v1.Container{
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("10"),
								},
							},
						},
						{
							Name: "init-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("200"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("100"),
								},
							},
						},
					},
				},
			},
		},
		{
			name:                     "restartable-init, init and regular containers with pod-level resources and overhead",
			expectedResource:         buildResource("500", "200Mi", map[string]string{"pods": "1"}, 0),
			podLevelResourcesEnabled: true,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Overhead: BuildResourceList("250", "100Mi"),
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("250", "100Mi"),
					},
					InitContainers: []v1.Container{
						{
							Name:          "restartable-init-1",
							RestartPolicy: &restartAlways,
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("10"),
								},
							},
						},
						{
							Name: "init-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("200"),
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("100"),
								},
							},
						},
					},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, test.podLevelResourcesEnabled)
			req := GetPodResourceRequest(test.pod)
			if !equality.Semantic.DeepEqual(req, test.expectedResource) {
				t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
					i, test.name, test.expectedResource, req)
			}
		})
	}
}

func TestGetPodResourceWithoutInitContainers(t *testing.T) {
	tests := []struct {
		name                     string
		podLevelResourcesEnabled bool
		pod                      *v1.Pod
		expectedResource         *Resource
	}{
		{
			name:                     "get resource for pod without init containers",
			podLevelResourcesEnabled: false,
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
			name:                     "get resource for pod with init containers",
			podLevelResourcesEnabled: false,
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
			name:                     "get resource for pod with overhead",
			podLevelResourcesEnabled: false,
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
		{
			name:                     "get resource for pod with pod-level resources greater than aggregate container resources",
			podLevelResourcesEnabled: true,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:                     resource.MustParse("4000m"),
							v1.ResourceMemory:                  resource.MustParse("4G"),
							v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
							v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
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
			expectedResource: NewResource(v1.ResourceList{
				v1.ResourceCPU:                     resource.MustParse("4000m"),
				v1.ResourceMemory:                  resource.MustParse("4G"),
				v1.ResourceHugePagesPrefix + "1Mi": resource.MustParse("2Gi"),
				v1.ResourceHugePagesPrefix + "1Ki": resource.MustParse("2Gi"),
			}),
		},
		{
			name:                     "get resource for pod with pod-level resources and overhead",
			podLevelResourcesEnabled: true,
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("4000m", ""),
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
					Overhead: BuildResourceList("1000m", "1G"),
				},
			},
			expectedResource: NewResource(BuildResourceList("5000m", "3G")),
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, test.podLevelResourcesEnabled)
			req := GetPodResourceWithoutInitContainers(test.pod)
			if !equality.Semantic.DeepEqual(req, test.expectedResource) {
				t.Errorf("case %d(%s) failed: \n expected %v, \n got: %v \n",
					i, test.name, test.expectedResource, req)
			}
		})
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

func TestGetPodResourceWithResize(t *testing.T) {
	// Mock feature gate
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.InPlacePodVerticalScaling, true)
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, k8sfeature.PodLevelResources, true)

	tests := []struct {
		name     string
		pod      *v1.Pod
		expected *Resource
	}{
		{
			name: "pod with no pending resize",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("2Gi"),
			}),
		},
		{
			name: "pod with pod-level resources and no pending resize",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("4", "4Gi"),
					},
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4Gi"),
			}),
		},
		{
			name: "pod with resize in infeasible",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizePending,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonInfeasible,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			}),
		},
		{
			name: "pod with pod-level resources and resize in infeasible",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("4", "4Gi"),
					},
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizePending,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonInfeasible,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4Gi"),
			}),
		},
		{
			name: "pod with resize in progress",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizeInProgress,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonInfeasible,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			}),
		}, {
			name: "pod with pod-level resources and resize in progress",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("4", "4Gi"),
					},
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizeInProgress,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonInfeasible,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4Gi"),
			}),
		},
		{
			name: "pod with resize in deferred",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizePending,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonDeferred,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("2"),
				v1.ResourceMemory: resource.MustParse("2Gi"),
			}),
		},
		{
			name: "pod with resize in deferred",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Resources: &v1.ResourceRequirements{
						Requests: BuildResourceList("4", "4Gi"),
					},
					Containers: []v1.Container{
						{
							Name: "c1",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{
						Type:   v1.PodResizePending,
						Status: v1.ConditionTrue,
						Reason: v1.PodReasonDeferred,
					}},
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "c1",
							Resources: &v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			expected: NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("4Gi"),
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetPodResourceWithoutInitContainers(tt.pod)
			if !got.Equal(tt.expected, Zero) {
				t.Errorf("GetPodResourceWithoutInitContainers() = %v, want %v", got, tt.expected)
			}
		})
	}
}
