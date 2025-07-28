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

package extender

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestMaxBodySizeLimit2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := strings.Repeat("a", maxBodySize+1)
		w.Write([]byte(`{"padding":"` + response + `"}`))
	}))
	defer server.Close()

	plugin := &extenderPlugin{
		client: http.Client{},
		config: &extenderConfig{
			urlPrefix: server.URL,
		},
	}

	var result map[string]interface{}
	err := plugin.send("test", &PredicateRequest{Task: &api.TaskInfo{}, Node: &api.NodeInfo{}}, &result)

	if err == nil {
		t.Error("Expected error due to request body size limit, but got nil")
	} else if !strings.Contains(err.Error(), "http: request body too large") {
		t.Errorf("Expected 'http: request body too large' error, got: %v", err)
	}
}

func TestIsInterested(t *testing.T) {
	tests := []struct {
		name             string
		managedResources []string
		pod              *corev1.Pod
		expected         bool
	}{
		{
			name:             "empty managed resources - should return true",
			managedResources: []string{},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu": resource.MustParse("100m"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "container has managed resource in requests",
			managedResources: []string{"nvidia.com/gpu"},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":            resource.MustParse("100m"),
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "container has managed resource in limits",
			managedResources: []string{"nvidia.com/gpu"},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "init container has managed resource",
			managedResources: []string{"nvidia.com/gpu"},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu": resource.MustParse("100m"),
								},
							},
						},
					},
					InitContainers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "no managed resources in containers",
			managedResources: []string{"nvidia.com/gpu"},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse("100m"),
									"memory": resource.MustParse("128Mi"),
								},
							},
						},
					},
				},
			},
			expected: false,
		},
		{
			name:             "multiple managed resources - one match",
			managedResources: []string{"nvidia.com/gpu", "amd.com/gpu"},
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":         resource.MustParse("100m"),
									"amd.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &extenderPlugin{
				config: &extenderConfig{
					managedResources: sets.New[string](tt.managedResources...),
				},
			}

			task := &api.TaskInfo{
				Pod: tt.pod,
			}

			result := plugin.IsInterested(task)
			if result != tt.expected {
				t.Errorf("IsInterested() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHasManagedResources(t *testing.T) {
	tests := []struct {
		name             string
		managedResources []string
		containers       []corev1.Container
		expected         bool
	}{
		{
			name:             "no containers",
			managedResources: []string{"nvidia.com/gpu"},
			containers:       []corev1.Container{},
			expected:         false,
		},
		{
			name:             "container with managed resource in requests",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "container with managed resource in limits",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "container with managed resource in both requests and limits",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
						Limits: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "multiple containers - one has managed resource",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu": resource.MustParse("100m"),
						},
					},
				},
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name:             "container with no managed resources",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("100m"),
							"memory": resource.MustParse("128Mi"),
						},
					},
				},
			},
			expected: false,
		},
		{
			name:             "empty resource requirements",
			managedResources: []string{"nvidia.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{},
				},
			},
			expected: false,
		},
		{
			name:             "multiple managed resources - partial match",
			managedResources: []string{"nvidia.com/gpu", "amd.com/gpu"},
			containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("1"),
							"intel.com/gpu":  resource.MustParse("1"), // not managed
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &extenderPlugin{
				config: &extenderConfig{
					managedResources: sets.New[string](tt.managedResources...),
				},
			}

			result := plugin.hasManagedResources(tt.containers)
			if result != tt.expected {
				t.Errorf("hasManagedResources() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestHasResourcesInList(t *testing.T) {
	tests := []struct {
		name             string
		managedResources []string
		resources        corev1.ResourceList
		expected         bool
	}{
		{
			name:             "empty resource list",
			managedResources: []string{"nvidia.com/gpu"},
			resources:        corev1.ResourceList{},
			expected:         false,
		},
		{
			name:             "nil resource list",
			managedResources: []string{"nvidia.com/gpu"},
			resources:        nil,
			expected:         false,
		},
		{
			name:             "resource list contains managed resource",
			managedResources: []string{"nvidia.com/gpu"},
			resources: corev1.ResourceList{
				"cpu":            resource.MustParse("100m"),
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			expected: true,
		},
		{
			name:             "resource list contains no managed resources",
			managedResources: []string{"nvidia.com/gpu"},
			resources: corev1.ResourceList{
				"cpu":    resource.MustParse("100m"),
				"memory": resource.MustParse("128Mi"),
			},
			expected: false,
		},
		{
			name:             "resource list contains multiple managed resources",
			managedResources: []string{"nvidia.com/gpu", "amd.com/gpu"},
			resources: corev1.ResourceList{
				"cpu":            resource.MustParse("100m"),
				"nvidia.com/gpu": resource.MustParse("1"),
				"amd.com/gpu":    resource.MustParse("1"),
			},
			expected: true,
		},
		{
			name:             "resource list contains one of multiple managed resources",
			managedResources: []string{"nvidia.com/gpu", "amd.com/gpu"},
			resources: corev1.ResourceList{
				"cpu":         resource.MustParse("100m"),
				"amd.com/gpu": resource.MustParse("1"),
			},
			expected: true,
		},
		{
			name:             "empty managed resources - should return false",
			managedResources: []string{},
			resources: corev1.ResourceList{
				"cpu":            resource.MustParse("100m"),
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &extenderPlugin{
				config: &extenderConfig{
					managedResources: sets.New[string](tt.managedResources...),
				},
			}

			result := plugin.hasResourcesInList(tt.resources)
			if result != tt.expected {
				t.Errorf("hasResourcesInList() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
