/*
Copyright 2026 The Volcano Authors.

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

package mutate

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	devices "volcano.sh/volcano/pkg/scheduler/api/devices"
	devconfig "volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

func TestAscendMutator_Name(t *testing.T) {
	mutator := newAscendMutator()
	if mutator.Name() != "ascend" {
		t.Errorf("expected name 'ascend', got '%s'", mutator.Name())
	}
}

func TestAscendMutator_MutateAdmission(t *testing.T) {
	const (
		knownGeometriesCMName      = "test-cm"
		knownGeometriesCMNamespace = "test-ns"
		deviceConfigDataKey        = "device-config.yaml"
	)

	deviceConfigYAML := "vnpus:\n" +
		"  hamiVnpuCore: true\n" +
		"  configs:\n" +
		"  - chipName: cm-310P3\n" +
		"    commonWord: Ascend310P\n" +
		"    resourceName: huawei.com/Ascend310P\n" +
		"    resourceMemoryName: huawei.com/Ascend310P-memory\n" +
		"    memoryAllocatable: 21527\n" +
		"    memoryCapacity: 24576\n" +
		"    aiCore: 8\n" +
		"    aiCPU: 7\n" +
		"    templates:\n" +
		"    - name: vir01\n" +
		"      memory: 3072\n" +
		"      aiCore: 1\n" +
		"      aiCPU: 1\n" +
		"  - chipName: cm-910A\n" +
		"    commonWord: Ascend910A\n" +
		"    resourceName: huawei.com/Ascend910A\n" +
		"    resourceMemoryName: huawei.com/Ascend910A-memory\n" +
		"    memoryAllocatable: 32768\n" +
		"    memoryCapacity: 32768\n" +
		"    aiCore: 30\n" +
		"    templates:\n" +
		"    - name: vir02\n" +
		"      memory: 2184\n" +
		"      aiCore: 2\n"

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      knownGeometriesCMName,
			Namespace: knownGeometriesCMNamespace,
		},
		Data: map[string]string{
			deviceConfigDataKey: deviceConfigYAML,
		},
	}

	fakeClient := fake.NewSimpleClientset(configMap)
	devices.SetClient(fakeClient)
	defer devices.SetClient(nil)
	devconfig.InitDevicesConfig(knownGeometriesCMName, knownGeometriesCMNamespace)
	tests := []struct {
		name               string
		pod                *v1.Pod
		expectedPatchCount int
		expectedPatchPaths []string
		expectedPatchOps   []string
	}{
		{
			name:               "nil pod returns nil",
			pod:                nil,
			expectedPatchCount: 0,
		},
		{
			name: "pod without annotations returns nil",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 0,
		},
		{
			name: "pod without hami-core annotation returns nil",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "other-mode",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 0,
		},
		{
			name: "container without ascend resource returns nil",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"cpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 0,
		},
		{
			name: "container with lifecycle postStart already exists skip",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
							Lifecycle: &v1.Lifecycle{
								PostStart: &v1.LifecycleHandler{
									Exec: &v1.ExecAction{
										Command: []string{"echo", "hello"},
									},
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 0,
		},
		{
			name: "successfully add lifecycle to container without lifecycle",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 1,
			expectedPatchPaths: []string{"/spec/containers/0/lifecycle"},
			expectedPatchOps:   []string{"add"},
		},
		{
			name: "successfully add postStart to container with lifecycle but no postStart",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
							Lifecycle: &v1.Lifecycle{
								PreStop: &v1.LifecycleHandler{
									Exec: &v1.ExecAction{
										Command: []string{"echo", "goodbye"},
									},
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 1,
			expectedPatchPaths: []string{"/spec/containers/0/lifecycle/postStart"},
			expectedPatchOps:   []string{"add"},
		},
		{
			name: "multiple containers with ascend resource",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "container-1",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
						{
							Name: "container-2",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("2"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
						{
							Name: "container-3",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"cpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 2,
			expectedPatchPaths: []string{
				"/spec/containers/0/lifecycle",
				"/spec/containers/1/lifecycle",
			},
			expectedPatchOps: []string{"add", "add"},
		},
		{
			name: "container with ascend resource in requests",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 1,
			expectedPatchPaths: []string{"/spec/containers/0/lifecycle"},
			expectedPatchOps:   []string{"add"},
		},
		{
			name: "multiple resource types in config",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						"huawei.com/vnpu-mode": "hami-core",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name: "test-container",
							Resources: v1.ResourceRequirements{
								Limits: v1.ResourceList{
									"huawei.com/Ascend310P":        resource.MustParse("1"),
									"huawei.com/Ascend310P-memory": resource.MustParse("1024"),
									"huawei.com/Ascend310P-core":   resource.MustParse("10"),
								},
							},
						},
					},
				},
			},
			expectedPatchCount: 1,
			expectedPatchPaths: []string{"/spec/containers/0/lifecycle"},
			expectedPatchOps:   []string{"add"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutator := newAscendMutator()
			patches := mutator.MutateAdmission(tt.pod)

			if len(patches) != tt.expectedPatchCount {
				t.Errorf("expected %d patches, got %d", tt.expectedPatchCount, len(patches))
			}

			for i, patch := range patches {
				if i < len(tt.expectedPatchPaths) && patch.Path != tt.expectedPatchPaths[i] {
					t.Errorf("expected patch path %s, got %s", tt.expectedPatchPaths[i], patch.Path)
				}
				if i < len(tt.expectedPatchOps) && patch.Op != tt.expectedPatchOps[i] {
					t.Errorf("expected patch op %s, got %s", tt.expectedPatchOps[i], patch.Op)
				}
				if patch.Op == jsonPatchOpAdd {
					if patch.Path == "/spec/containers/0/lifecycle" || patch.Path == "/spec/containers/1/lifecycle" {
						lifecycle, ok := patch.Value.(v1.Lifecycle)
						if !ok {
							t.Errorf("patch value is not v1.Lifecycle")
						}
						if lifecycle.PostStart == nil {
							t.Errorf("PostStart handler is nil")
						}
						if lifecycle.PostStart.Exec == nil {
							t.Errorf("Exec action is nil")
						}
						if len(lifecycle.PostStart.Exec.Command) != 3 {
							t.Errorf("expected command length 3, got %d", len(lifecycle.PostStart.Exec.Command))
						}
					}
				}
			}
		})
	}

	config := devconfig.GetConfig()
	if config == nil {
		t.Fatalf("expected devices config loaded from configmap, got nil")
	}
	if len(config.VNPUs.Configs) != 2 {
		t.Fatalf("expected 2 vnpu configs from configmap, got %d", len(config.VNPUs.Configs))
	}
	if config.VNPUs.Configs[0].ChipName != "cm-310P3" {
		t.Fatalf("expected chipName loaded from configmap to be cm-310P3, got %s", config.VNPUs.Configs[0].ChipName)
	}
}

// TestAscendMutator_RegisteredDevicesMutators verifies that AscendMutator is registered
func TestAscendMutator_RegisteredDevicesMutators(t *testing.T) {
	found := false
	for _, mutator := range registeredDeviceMutators {
		if mutator.Name() == "ascend" {
			found = true
			break
		}
	}
	if !found {
		t.Error("AscendMutator not found in registeredDeviceMutators")
	}
}
