/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestNodeOrderFn tests the NodeOrderFn function
func TestNodeOrderFn(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		task          *api.TaskInfo
		node          *api.NodeInfo
		expectedScore float64
		description   string
		expectError   bool
	}{
		{
			name: "task without card request",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
				nodeCardInfos:          map[string]NodeCardResourceInfo{},
			},
			task: &api.TaskInfo{
				UID:       "task-1",
				Name:      "cpu-task",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cpu-task",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("1"),
										v1.ResourceMemory: resource.MustParse("1Gi"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-1",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				},
			},
			expectedScore: 0,
			description:   "Task without card request should return score 0",
			expectError:   false,
		},
		{
			name: "single card type request",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-1": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-A100",
							Count:          4,
							Memory:         40 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-A100": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-A100": "nvidia.com/gpu",
						},
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-2",
				Name:      "single-type-gpu-task",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "single-type-gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("4"),
										v1.ResourceMemory: resource.MustParse("8Gi"),
										"nvidia.com/gpu":  resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-1",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("32"),
							v1.ResourceMemory: resource.MustParse("128Gi"),
							"nvidia.com/gpu":  resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu": 4000,
					},
				},
			},
			expectedScore: 0,
			description:   "Single card type request should return score 0 (no ordering needed)",
			expectError:   false,
		},
		{
			name: "multi-card types - node has first priority card (A100)",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
					"NVIDIA-H100": "nvidia.com/gpu",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-1": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-A100",
							Count:          4,
							Memory:         40 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-A100": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-A100": "nvidia.com/gpu",
						},
					},
				},
				nodeOrderWeight: 1.0, // Default weight
			},
			task: &api.TaskInfo{
				UID:       "task-3",
				Name:      "multi-type-gpu-task",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-type-gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("8"),
										v1.ResourceMemory: resource.MustParse("32Gi"),
										"nvidia.com/gpu":  resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-1",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("32"),
							v1.ResourceMemory: resource.MustParse("128Gi"),
							"nvidia.com/gpu":  resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu": 4000,
					},
				},
			},
			expectedScore: 100,
			description:   "Node with first priority card type (A100) should get score 100",
			expectError:   false,
		},
		{
			name: "multi-card types - node has second priority card (H100)",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu-a100",
					"NVIDIA-H100": "nvidia.com/gpu-h100",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-2": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-H100",
							Count:          4,
							Memory:         80 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-H100": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-H100": "nvidia.com/gpu-h100",
						},
					},
				},
				nodeOrderWeight: 1.0, // Default weight
			},
			task: &api.TaskInfo{
				UID:       "task-4",
				Name:      "multi-type-gpu-task-2",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-type-gpu-task-2",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:        resource.MustParse("8"),
										v1.ResourceMemory:     resource.MustParse("32Gi"),
										"nvidia.com/gpu-h100": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-2",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:        resource.MustParse("32"),
							v1.ResourceMemory:     resource.MustParse("128Gi"),
							"nvidia.com/gpu-h100": resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-h100": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-h100": 4000,
					},
				},
			},
			expectedScore: 50, // baseScore: 100 * 0.5^1 = 50, finalScore: 50 * 1.0 = 50
			description:   "Node with second priority card type (H100) should get score 50 (with default weight 1.0)",
			expectError:   false,
		},
		{
			name: "multi-card types - node has third priority card (T4)",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu-a100",
					"NVIDIA-H100": "nvidia.com/gpu-h100",
					"NVIDIA-T4":   "nvidia.com/gpu-t4",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-3": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-T4",
							Count:          4,
							Memory:         16 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-T4": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-T4": "nvidia.com/gpu-t4",
						},
					},
				},
				nodeOrderWeight: 1.0, // Default weight
			},
			task: &api.TaskInfo{
				UID:       "task-5",
				Name:      "multi-type-gpu-task-3",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-type-gpu-task-3",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100|NVIDIA-T4",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:      resource.MustParse("4"),
										v1.ResourceMemory:   resource.MustParse("16Gi"),
										"nvidia.com/gpu-t4": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-3",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-3",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:      resource.MustParse("16"),
							v1.ResourceMemory:   resource.MustParse("64Gi"),
							"nvidia.com/gpu-t4": resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 16000,
					Memory:   64 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-t4": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 16000,
					Memory:   64 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-t4": 4000,
					},
				},
			},
			expectedScore: 25, // baseScore: 100 * 0.5^2 = 25, finalScore: 25 * 1.0 = 25
			description:   "Node with third priority card type (T4) should get score 25 (with default weight 1.0)",
			expectError:   false,
		},
		{
			name: "multi-card types with custom weight 2.0 - second priority card",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu-a100",
					"NVIDIA-H100": "nvidia.com/gpu-h100",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-5": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-H100",
							Count:          4,
							Memory:         80 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-H100": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-H100": "nvidia.com/gpu-h100",
						},
					},
				},
				nodeOrderWeight: 2.0, // Custom weight to scale up scores
			},
			task: &api.TaskInfo{
				UID:       "task-6",
				Name:      "multi-type-gpu-task-custom-weight",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-type-gpu-task-custom-weight",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:        resource.MustParse("8"),
										v1.ResourceMemory:     resource.MustParse("32Gi"),
										"nvidia.com/gpu-h100": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-5",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-5",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:        resource.MustParse("32"),
							v1.ResourceMemory:     resource.MustParse("128Gi"),
							"nvidia.com/gpu-h100": resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-h100": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-h100": 4000,
					},
				},
			},
			expectedScore: 100, // baseScore: 100 * 0.5^1 = 50, finalScore: 50 * 2.0 = 100
			description:   "Node with second priority card and custom weight 2.0 should get score 100",
			expectError:   false,
		},
		{
			name: "multi-card types - node has no matching card type",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu-a100",
					"NVIDIA-H100": "nvidia.com/gpu-h100",
					"NVIDIA-V100": "nvidia.com/gpu-v100",
				},
				nodeCardInfos: map[string]NodeCardResourceInfo{
					"node-4": {
						CardInfo: CardInfo{
							Name:           "NVIDIA-V100",
							Count:          4,
							Memory:         32 * 1024 * 1024 * 1024,
							ResourcePrefix: "nvidia.com",
						},
						CardResource: v1.ResourceList{
							"NVIDIA-V100": resource.MustParse("4"),
						},
						CardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
							"NVIDIA-V100": "nvidia.com/gpu-v100",
						},
					},
				},
				nodeOrderWeight: 1.0, // Default weight
			},
			task: &api.TaskInfo{
				UID:       "task-7",
				Name:      "multi-type-gpu-task-4",
				Namespace: "default",
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-type-gpu-task-4",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "container",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:        resource.MustParse("8"),
										v1.ResourceMemory:     resource.MustParse("32Gi"),
										"nvidia.com/gpu-v100": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			node: &api.NodeInfo{
				Name: "node-4",
				Node: &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-4",
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:        resource.MustParse("32"),
							v1.ResourceMemory:     resource.MustParse("128Gi"),
							"nvidia.com/gpu-v100": resource.MustParse("4"),
						},
					},
				},
				Allocatable: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-v100": 4000,
					},
				},
				Idle: &api.Resource{
					MilliCPU: 32000,
					Memory:   128 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"nvidia.com/gpu-v100": 4000,
					},
				},
			},
			expectedScore: 0,
			description:   "Node without any matching card type should get score 0",
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, err := tt.plugin.NodeOrderFn(tt.task, tt.node)
			if tt.expectError && err == nil {
				t.Errorf("%s: expected error but got none", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("%s: unexpected error: %v", tt.description, err)
			}
			if score != tt.expectedScore {
				t.Errorf("%s: expected score %.2f, got %.2f", tt.description, tt.expectedScore, score)
			}
		})
	}
}
