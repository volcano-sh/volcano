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

package cpuquota

import (
	"regexp"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// TestNew tests the New function to ensure it creates a plugin instance correctly
func TestNew(t *testing.T) {
	arguments := framework.Arguments{
		GPUResourceNames:   "nvidia.com/gpu,amd.com/gpu",
		CPUQuota:           "4",
		CPUQuotaPercentage: 50.0,
	}

	plugin := New(arguments)

	if plugin == nil {
		t.Fatal("Expected plugin to be created, but got nil")
	}

	if plugin.Name() != PluginName {
		t.Errorf("Expected plugin name to be %s, but got %s", PluginName, plugin.Name())
	}
}

// TestCpuQuotaPlugin_Name tests the Name method
func TestCpuQuotaPlugin_Name(t *testing.T) {
	plugin := &cpuQuotaPlugin{}

	name := plugin.Name()
	if name != PluginName {
		t.Errorf("Expected plugin name to be %s, but got %s", PluginName, name)
	}
}

// TestParseArguments tests the parseArguments method
func TestParseArguments(t *testing.T) {
	testCases := []struct {
		name                    string
		arguments               framework.Arguments
		expectedGPUPatterns     int
		expectedCPUQuota        float64
		expectedCPUQuotaPercent float64
	}{
		{
			name: "Valid GPU patterns and CPU quota",
			arguments: framework.Arguments{
				GPUResourceNames:   "nvidia.com/gpu,amd.com/gpu",
				CPUQuota:           4.0,
				CPUQuotaPercentage: 50.0,
			},
			expectedGPUPatterns:     2,
			expectedCPUQuota:        4.0,
			expectedCPUQuotaPercent: 0, // Should be reset when quota is specified
		},
		{
			name: "Only GPU patterns",
			arguments: framework.Arguments{
				GPUResourceNames: "nvidia.com/gpu",
			},
			expectedGPUPatterns:     1,
			expectedCPUQuota:        0.0,
			expectedCPUQuotaPercent: 0,
		},
		{
			name: "Only CPU quota percentage",
			arguments: framework.Arguments{
				CPUQuotaPercentage: 75.0,
			},
			expectedGPUPatterns:     0,
			expectedCPUQuota:        0.0,
			expectedCPUQuotaPercent: 75.0,
		},
		{
			name: "Invalid GPU pattern",
			arguments: framework.Arguments{
				GPUResourceNames: "[invalid",
			},
			expectedGPUPatterns:     0,
			expectedCPUQuota:        0.0,
			expectedCPUQuotaPercent: 0,
		},
		{
			name:                    "Empty arguments",
			arguments:               framework.Arguments{},
			expectedGPUPatterns:     0,
			expectedCPUQuota:        0.0,
			expectedCPUQuotaPercent: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &cpuQuotaPlugin{
				pluginArguments: tc.arguments,
			}

			plugin.parseArguments()

			if len(plugin.gpuResourcePatterns) != tc.expectedGPUPatterns {
				t.Errorf("Expected %d GPU patterns, but got %d", tc.expectedGPUPatterns, len(plugin.gpuResourcePatterns))
			}

			if plugin.cpuQuota != tc.expectedCPUQuota {
				t.Errorf("Expected CPU quota %f, but got %f", tc.expectedCPUQuota, plugin.cpuQuota)
			}

			if plugin.cpuQuotaPercentage != tc.expectedCPUQuotaPercent {
				t.Errorf("Expected CPU quota percentage %f, but got %f", tc.expectedCPUQuotaPercent, plugin.cpuQuotaPercentage)
			}
		})
	}
}

// TestIsGPUNode tests the isGPUNode method
func TestIsGPUNode(t *testing.T) {
	plugin := &cpuQuotaPlugin{
		gpuResourcePatterns: []*regexp.Regexp{
			regexp.MustCompile("nvidia\\.com/gpu"),
			regexp.MustCompile("amd\\.com/gpu"),
		},
	}

	testCases := []struct {
		name     string
		node     *api.NodeInfo
		expected bool
	}{
		{
			name: "GPU node with nvidia.com/gpu",
			node: &api.NodeInfo{
				Node: &v1.Node{
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
							"nvidia.com/gpu":  resource.MustParse("4"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "GPU node with amd.com/gpu",
			node: &api.NodeInfo{
				Node: &v1.Node{
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
							"amd.com/gpu":     resource.MustParse("2"),
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-GPU node",
			node: &api.NodeInfo{
				Node: &v1.Node{
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Node with different GPU resource",
			node: &api.NodeInfo{
				Node: &v1.Node{
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
							"intel.com/gpu":   resource.MustParse("1"),
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := plugin.isGPUNode(tc.node)
			if result != tc.expected {
				t.Errorf("Expected %v, but got %v", tc.expected, result)
			}
		})
	}
}

// TestIsCPUPod tests the isCPUPod method
func TestIsCPUPod(t *testing.T) {
	plugin := &cpuQuotaPlugin{
		gpuResourcePatterns: []*regexp.Regexp{
			regexp.MustCompile("nvidia\\.com/gpu"),
			regexp.MustCompile("amd\\.com/gpu"),
		},
	}

	testCases := []struct {
		name     string
		task     *api.TaskInfo
		expected bool
	}{
		{
			name: "CPU pod without GPU requests",
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1024,
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourceCPU:    1.0,
						v1.ResourceMemory: 1024,
					},
				},
			},
			expected: true,
		},
		{
			name: "GPU pod with nvidia.com/gpu request",
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1024,
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourceCPU:    1.0,
						v1.ResourceMemory: 1024,
						"nvidia.com/gpu":  1.0,
					},
				},
			},
			expected: false,
		},
		{
			name: "GPU pod with amd.com/gpu request",
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1024,
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourceCPU:    1.0,
						v1.ResourceMemory: 1024,
						"amd.com/gpu":     1.0,
					},
				},
			},
			expected: false,
		},
		{
			name: "Pod with different GPU resource",
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1024,
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourceCPU:    1.0,
						v1.ResourceMemory: 1024,
						"intel.com/gpu":   1.0,
					},
				},
			},
			expected: true, // Should be considered CPU pod since intel.com/gpu is not in patterns
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := plugin.isCPUPod(tc.task)
			if result != tc.expected {
				t.Errorf("Expected %v, but got %v", tc.expected, result)
			}
		})
	}
}

// TestCalculateCPUQuota tests the calculateCPUQuota method
func TestCalculateCPUQuota(t *testing.T) {
	plugin := &cpuQuotaPlugin{
		cpuQuota:           4.0,
		cpuQuotaPercentage: 50.0,
	}

	node := &api.NodeInfo{
		Node: &v1.Node{
			Status: v1.NodeStatus{
				Allocatable: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("8"),
				},
			},
		},
		Allocatable: &api.Resource{
			MilliCPU: 8000, // 8 CPU cores
		},
	}

	testCases := []struct {
		name        string
		node        *api.NodeInfo
		expected    float64
		expectError bool
	}{
		{
			name: "Node with CPU quota annotation",
			node: &api.NodeInfo{
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							NodeAnnotationCPUQuota: "6",
						},
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("8"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 8000,
				},
			},
			expected:    6000.0, // 6 cores (in milliCPU)
			expectError: false,
		},
		{
			name: "Node with CPU quota percentage annotation",
			node: &api.NodeInfo{
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							NodeAnnotationCPUQuotaPercentage: "75",
						},
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("8"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 8000,
				},
			},
			expected:    6000.0, // 75% of 8 cores (in milliCPU)
			expectError: false,
		},
		{
			name:        "Node without annotations (use plugin config)",
			node:        node,
			expected:    4000.0, // From plugin cpuQuota (in milliCPU)
			expectError: false,
		},
		{
			name: "Invalid CPU quota annotation",
			node: &api.NodeInfo{
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							NodeAnnotationCPUQuota: "invalid",
						},
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("8"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 8000,
				},
			},
			expected:    0.0,
			expectError: true,
		},
		{
			name: "Invalid CPU quota percentage annotation",
			node: &api.NodeInfo{
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							NodeAnnotationCPUQuotaPercentage: "150", // > 100%
						},
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU: resource.MustParse("8"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 8000,
				},
			},
			expected:    0.0,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := plugin.calculateCPUQuota(tc.node)

			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tc.expected {
				t.Errorf("Expected CPU quota %f, but got %f", tc.expected, result)
			}
		})
	}
}

// TestOnSessionClose tests the OnSessionClose method
func TestOnSessionClose(t *testing.T) {
	plugin := &cpuQuotaPlugin{
		Nodes: map[string]*api.NodeInfo{
			"node1": &api.NodeInfo{},
			"node2": &api.NodeInfo{},
		},
	}

	ssn := &framework.Session{}
	plugin.OnSessionClose(ssn)

	if len(plugin.Nodes) != 0 {
		t.Errorf("Expected Nodes to be cleared, but got %d nodes", len(plugin.Nodes))
	}
}

// TestArguments tests argument parsing with different types
func TestArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		GPUResourceNames:   "nvidia.com/gpu,amd.com/gpu",
		CPUQuota:           4,
		CPUQuotaPercentage: 50.0,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)
	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	cpuQuotaPlugin, ok := plugin.(*cpuQuotaPlugin)
	if !ok {
		t.Fatalf("plugin should be %T, but not %T", cpuQuotaPlugin, plugin)
	}

	if len(cpuQuotaPlugin.gpuResourcePatterns) != 2 {
		t.Errorf("Expected 2 GPU patterns, but got %d", len(cpuQuotaPlugin.gpuResourcePatterns))
	}

	if cpuQuotaPlugin.cpuQuota != 4.0 {
		t.Errorf("Expected CPU quota 4.0, but got %f", cpuQuotaPlugin.cpuQuota)
	}

	if cpuQuotaPlugin.cpuQuotaPercentage != 0 {
		t.Errorf("Expected CPU quota percentage 0 (reset when quota is specified), but got %f", cpuQuotaPlugin.cpuQuotaPercentage)
	}
}
