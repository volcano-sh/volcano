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
	"k8s.io/client-go/tools/record"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TestAllocatableFn tests the AllocatableFn function
func TestAllocatableFn(t *testing.T) {
	tests := []struct {
		name        string
		plugin      *Plugin
		queue       *api.QueueInfo
		task        *api.TaskInfo
		queueState  scheduling.QueueState
		expected    bool
		description string
	}{
		{
			name: "queue is open and task is allocatable",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-1": {
						queueID: "queue-1",
						name:    "test-queue",
						allocated: &api.Resource{
							MilliCPU: 1000,
							Memory:   1 * 1024 * 1024 * 1024,
						},
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			queue: &api.QueueInfo{
				UID:  "queue-1",
				Name: "test-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue",
					},
					Status: scheduling.QueueStatus{
						State: scheduling.QueueStateOpen,
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-1",
				Name:      "test-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task",
						Namespace: "default",
					},
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    true,
			description: "Queue is open and has enough resources",
		},
		{
			name: "queue is closed",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-2": {
						queueID: "queue-2",
						name:    "closed-queue",
						allocated: &api.Resource{
							MilliCPU: 0,
							Memory:   0,
						},
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			queue: &api.QueueInfo{
				UID:  "queue-2",
				Name: "closed-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "closed-queue",
					},
					Status: scheduling.QueueStatus{
						State: scheduling.QueueStateClosed,
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-2",
				Name:      "test-task-2",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
			},
			queueState:  scheduling.QueueStateClosed,
			expected:    false,
			description: "Queue is closed, should reject task",
		},
		{
			name: "queue is unknown state",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-3": {
						queueID:   "queue-3",
						name:      "unknown-queue",
						allocated: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 5000,
							Memory:   5 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			queue: &api.QueueInfo{
				UID:  "queue-3",
				Name: "unknown-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unknown-queue",
					},
					Status: scheduling.QueueStatus{
						State: scheduling.QueueStateUnknown,
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-3",
				Name:      "test-task-3",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
			},
			queueState:  scheduling.QueueStateUnknown,
			expected:    false,
			description: "Queue state is unknown, should reject task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plugin.AllocatableFn(tt.queue, tt.task)
			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

// TestIsTaskAllocatable tests the isTaskAllocatable function
func TestIsTaskAllocatable(t *testing.T) {
	// Initialize the global eventRecorder for testing
	eventRecorder = record.NewFakeRecorder(100)

	tests := []struct {
		name        string
		plugin      *Plugin
		qAttr       *queueAttr
		task        *api.TaskInfo
		expected    bool
		description string
	}{
		{
			name: "task with card resource but no card name - should use CPU/Memory only",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-H200": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-1",
				Name:      "no-card-name-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "no-card-name-task",
						Namespace: "default",
						// No card annotation
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expected:    true,
			description: "Task without card name annotation should be allocatable based on CPU/Memory",
		},
		{
			name: "empty allocated resource should work",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID:   "queue-2",
				name:      "empty-allocated-queue",
				allocated: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-2",
				Name:      "test-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
			},
			expected:    true,
			description: "Empty allocated resource should allow allocation",
		},
		{
			name: "insufficient CPU quota",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-3",
				name:    "cpu-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 8000,
					Memory:   5 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   20 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-3",
				Name:      "cpu-heavy-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 3000, // 8000 + 3000 > 10000
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: nil, // No pod to avoid eventRecorder issue in unit test
			},
			expected:    false,
			description: "Should reject task when CPU quota is insufficient",
		},
		{
			name: "insufficient Memory quota",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-4",
				name:    "memory-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 20000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-4",
				Name:      "memory-heavy-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   3 * 1024 * 1024 * 1024, // 8 + 3 > 10
				},
				Pod: nil, // No pod to avoid eventRecorder issue in unit test
			},
			expected:    false,
			description: "Should reject task when memory quota is insufficient",
		},
		{
			name: "task without GPU request should pass even with GPU quota nearly full",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-H200": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-5",
				name:    "gpu-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-H200": 3500, // 3.5 GPUs already allocated
					},
				},
				capability: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-H200": 4000, // Only 4 GPUs available
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-5",
				Name:      "cpu-only-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   4 * 1024 * 1024 * 1024,
				},
				Pod: nil, // No pod, no GPU request
			},
			expected:    true,
			description: "Task without GPU request should pass even if GPU quota is nearly full",
		},
		{
			name: "successful allocation with CPU and memory",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-6",
				name:    "normal-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-6",
				Name:      "normal-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 3000,
					Memory:   3 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "normal-task",
						Namespace: "default",
					},
				},
			},
			expected:    true,
			description: "Should allow allocation when resources are sufficient",
		},
		{
			name: "successful allocation with GPU",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-7",
				name:    "gpu-queue",
				allocated: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000, // 2 GPUs allocated
					},
				},
				capability: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 8000, // 8 GPUs available
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-7",
				Name:      "gpu-task-ok",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task-ok",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("4"), // 2 + 4 <= 8, OK
									},
								},
							},
						},
					},
				},
			},
			expected:    true,
			description: "Should allow allocation when GPU quota is sufficient",
		},
		{
			name: "successful allocation with GPU and card unlimited cpu memory enabled",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-7",
				name:    "gpu-queue",
				allocated: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000, // 2 GPUs allocated
					},
				},
				capability: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 8000, // 8 GPUs available
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-7",
				Name:      "gpu-task-ok",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task-ok",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("4"), // 2 + 4 <= 8, OK
									},
								},
							},
						},
					},
				},
			},
			expected:    true,
			description: "Should allow allocation when GPU quota is sufficient and card unlimited cpu memory is enabled",
		},
		{
			name: "successful allocation with GPU and card unlimited cpu memory disabled",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-7",
				name:    "gpu-queue",
				allocated: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000, // 2 GPUs allocated
					},
				},
				capability: &api.Resource{
					MilliCPU: 16000,
					Memory:   32 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 8000, // 8 GPUs available
					},
				},
			},
			task: &api.TaskInfo{
				UID:       "task-7",
				Name:      "gpu-task-ok",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task-ok",
						Namespace: "default",
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("4"), // 2 + 4 <= 8, OK
									},
								},
							},
						},
					},
				},
			},
			expected:    false,
			description: "Should not allow allocation when GPU quota is sufficient and card unlimited cpu memory is disabled",
		},
		{
			name: "task with nil request resource should pass if queue has capacity",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-8",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-8",
				Name:      "nil-request-task",
				Namespace: "default",
				Resreq:    nil,
				Pod:       nil,
			},
			expected:    true,
			description: "Task with nil request should be allowed if allocated is within capability",
		},
		{
			name: "totalToBeAllocated ScalarResources is nil - should allow",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-9",
				name:    "no-scalar-queue",
				allocated: &api.Resource{
					MilliCPU:        2000,
					Memory:          2 * 1024 * 1024 * 1024,
					ScalarResources: nil, // No scalar resources
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-9",
				Name:      "no-scalar-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
			},
			expected:    true,
			description: "Should allow when totalToBeAllocated has no scalar resources",
		},
		{
			name: "zero CPU request should not trigger CPU check",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-10",
				name:    "zero-cpu-queue",
				allocated: &api.Resource{
					MilliCPU: 9500, // Almost full
					Memory:   2 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-10",
				Name:      "zero-cpu-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 0, // Zero CPU, should skip CPU check
					Memory:   1 * 1024 * 1024 * 1024,
				},
			},
			expected:    true,
			description: "Zero CPU request should skip CPU quota check",
		},
		{
			name: "zero memory request should not trigger memory check",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-11",
				name:    "zero-memory-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   9 * 1024 * 1024 * 1024, // Almost full
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			task: &api.TaskInfo{
				UID:       "task-11",
				Name:      "zero-memory-task",
				Namespace: "default",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   0, // Zero memory, should skip memory check
				},
			},
			expected:    true,
			description: "Zero memory request should skip memory quota check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plugin.isTaskAllocatable(tt.qAttr, tt.task)
			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

// TestIsTaskAllocatableWithIgnoredScalarResource tests ignored scalar resources
func TestIsTaskAllocatableWithIgnoredScalarResource(t *testing.T) {
	// Initialize the global eventRecorder for testing
	eventRecorder = record.NewFakeRecorder(100)

	// This test verifies that ignored scalar resources (like ephemeral-storage)
	// are not checked against quota
	plugin := &Plugin{
		cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
		isCardUnlimitedCpuMemory: false,
	}

	qAttr := &queueAttr{
		queueID: "queue-ignore",
		name:    "ignore-scalar-queue",
		allocated: &api.Resource{
			MilliCPU: 2000,
			Memory:   2 * 1024 * 1024 * 1024,
			ScalarResources: map[v1.ResourceName]float64{
				v1.ResourceEphemeralStorage: 100 * 1024 * 1024 * 1024, // High ephemeral storage
			},
		},
		capability: &api.Resource{
			MilliCPU: 10000,
			Memory:   10 * 1024 * 1024 * 1024,
			ScalarResources: map[v1.ResourceName]float64{
				v1.ResourceEphemeralStorage: 50 * 1024 * 1024 * 1024, // Lower than allocated
			},
		},
	}

	task := &api.TaskInfo{
		UID:       "task-ignore",
		Name:      "ignore-scalar-task",
		Namespace: "default",
		Resreq: &api.Resource{
			MilliCPU: 1000,
			Memory:   1 * 1024 * 1024 * 1024,
			ScalarResources: map[v1.ResourceName]float64{
				v1.ResourceEphemeralStorage: 10 * 1024 * 1024 * 1024,
			},
		},
	}

	// Should pass because ephemeral-storage is ignored
	result := plugin.isTaskAllocatable(qAttr, task)
	if !result {
		t.Errorf("Expected task to be allocatable when only ignored scalar resources exceed quota, got false")
	}
}
