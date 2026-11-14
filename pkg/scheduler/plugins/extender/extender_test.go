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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
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

// TestPredicateFn opens a session with the extender plugin pointed at a local
// HTTP server and checks how the extender's predicate response, an HTTP
// failure, the ignorable option and the managed resources decide the result.
func TestPredicateFn(t *testing.T) {
	const (
		predicateVerb = "predicate"
		gpuResource   = "nvidia.com/gpu"
	)

	tests := []struct {
		name             string
		ignorable        bool
		managedResources []interface{}
		requestGPU       bool
		statusCode       int
		responseBody     string
		wantCalls        int
		wantErr          bool
		wantCode         int
		wantReason       string
	}{
		{
			name:         "extender accepts the node",
			statusCode:   http.StatusOK,
			responseBody: `{}`,
			wantCalls:    1,
		},
		{
			name:         "extender rejects the node without a code",
			statusCode:   http.StatusOK,
			responseBody: `{"status":"node is reserved"}`,
			wantCalls:    1,
			wantErr:      true,
			wantCode:     api.Error,
			wantReason:   "node is reserved",
		},
		{
			name:         "extender rejects the node with a code",
			statusCode:   http.StatusOK,
			responseBody: fmt.Sprintf(`{"status":"node is reserved","code":%d}`, api.UnschedulableAndUnresolvable),
			wantCalls:    1,
			wantErr:      true,
			wantCode:     api.UnschedulableAndUnresolvable,
			wantReason:   "node is reserved",
		},
		{
			name:         "extender failure rejects the node when not ignorable",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{}`,
			wantCalls:    1,
			wantErr:      true,
			wantCode:     api.Error,
			wantReason:   "failed predicate with extender",
		},
		{
			name:         "extender failure is ignored when ignorable",
			ignorable:    true,
			statusCode:   http.StatusInternalServerError,
			responseBody: `{}`,
			wantCalls:    1,
		},
		{
			name:             "extender is not called for a task without managed resources",
			managedResources: []interface{}{gpuResource},
			statusCode:       http.StatusOK,
			responseBody:     `{"status":"node is reserved"}`,
		},
		{
			name:             "extender is called for a task requesting a managed resource",
			managedResources: []interface{}{gpuResource},
			requestGPU:       true,
			statusCode:       http.StatusOK,
			responseBody:     `{"status":"node is reserved"}`,
			wantCalls:        1,
			wantErr:          true,
			wantCode:         api.Error,
			wantReason:       "node is reserved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/"+predicateVerb {
					t.Errorf("extender got request for path %q, want %q", r.URL.Path, "/"+predicateVerb)
				}
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			arguments := framework.Arguments{
				ExtenderURLPrefix:     server.URL,
				ExtenderPredicateVerb: predicateVerb,
				ExtenderIgnorable:     tt.ignorable,
			}
			if tt.managedResources != nil {
				arguments[ExtenderManagedResources] = tt.managedResources
			}

			resources := api.BuildResourceList("1", "1Gi")
			if tt.requestGPU {
				resources = api.BuildResourceList("1", "1Gi", api.ScalarResource{Name: gpuResource, Value: "1"})
			}

			trueValue := true
			test := uthelper.TestCommonStruct{
				Name:    tt.name,
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, resources, "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: gpuResource, Value: "4"}), make(map[string]string)),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, vcapisv1.PodGroupInqueue),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			}
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{Name: PluginName, EnabledPredicate: &trueValue, Arguments: arguments},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			var task *api.TaskInfo
			for _, job := range ssn.Jobs {
				for _, jobTask := range job.Tasks {
					task = jobTask
				}
			}
			if task == nil {
				t.Fatal("task for pod ns1/p1 not found in session")
			}

			err := ssn.PredicateFn(task, ssn.Nodes["n1"])

			if got := int(calls.Load()); got != tt.wantCalls {
				t.Errorf("extender was called %d times, want %d", got, tt.wantCalls)
			}
			if !tt.wantErr {
				if err != nil {
					t.Errorf("unexpected predicate error: %v", err)
				}
				return
			}

			var fitErr *api.FitError
			if !errors.As(err, &fitErr) {
				t.Fatalf("predicate error = %v, want a *api.FitError", err)
			}
			if len(fitErr.Status) != 1 {
				t.Fatalf("predicate error has %d statuses, want 1", len(fitErr.Status))
			}
			if fitErr.Status[0].Code != tt.wantCode {
				t.Errorf("predicate status code = %d, want %d", fitErr.Status[0].Code, tt.wantCode)
			}
			if !strings.Contains(fitErr.Status[0].Reason, tt.wantReason) {
				t.Errorf("predicate status reason = %q, want it to contain %q", fitErr.Status[0].Reason, tt.wantReason)
			}
		})
	}
}
