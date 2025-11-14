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

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// TestOnAllocate tests the OnAllocate event handler
func TestOnAllocate(t *testing.T) {
	tests := []struct {
		name                 string
		setupPlugin          func() *Plugin
		setupSession         func() *framework.Session
		taskToAllocate       *api.TaskInfo
		expectedAllocatedCPU float64
		expectedAllocatedMem float64
		expectedAllocatedGPU float64
		expectedShareGreater float64
		shouldSucceed        bool
	}{
		{
			name: "allocate task with CPU and memory only",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:              map[api.QueueID]*queueAttr{},
					totalResource:          api.EmptyResource(),
					totalGuarantee:         api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
				}
				// Pre-create queue attributes
				p.queueOpts["queue-1"] = &queueAttr{
					queueID:   "queue-1",
					name:      "test-queue-1",
					allocated: api.EmptyResource(),
					deserved: &api.Resource{
						MilliCPU: 10000,
						Memory:   20 * 1024 * 1024 * 1024, // 10Gi
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue-1",
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("10"),
							v1.ResourceMemory: resource.MustParse("20Gi"),
						},
					},
				}

				job := &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job-1",
					Namespace: "default",
					Queue:     "queue-1",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job-1",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinMember: 1,
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:   "queue-1",
						Name:  "test-queue-1",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-1": job,
				}

				return session
			},
			taskToAllocate: &api.TaskInfo{
				UID:       "task-1",
				Name:      "test-task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task-1",
						Namespace: "default",
					},
				},
			},
			expectedAllocatedCPU: 2000,
			expectedAllocatedMem: 2 * 1024 * 1024 * 1024,
			expectedShareGreater: 0.2, // 2000/10000 = 0.2
			shouldSucceed:        true,
		},
		{
			name: "allocate task with GPU card resources and card unlimited cpu memory disabled",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:      map[api.QueueID]*queueAttr{},
					totalResource:  api.EmptyResource(),
					totalGuarantee: api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
						"NVIDIA-H200": "nvidia.com/gpu",
					},
					isCardUnlimitedCpuMemory: false,
				}
				p.queueOpts["queue-gpu"] = &queueAttr{
					queueID:   "queue-gpu",
					name:      "gpu-queue",
					allocated: api.EmptyResource(),
					deserved: &api.Resource{
						MilliCPU: 4000,
						Memory:   8 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 4000, // 4 GPUs
						},
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-queue",
						Annotations: map[string]string{
							"volcano.sh/card.quota": `{"NVIDIA-H200": 4}`,
						},
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}

				job := &api.JobInfo{
					UID:       "job-gpu",
					Name:      "gpu-job",
					Namespace: "default",
					Queue:     "queue-gpu",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-gpu": {
						UID:   "queue-gpu",
						Name:  "gpu-queue",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-gpu": job,
				}

				return session
			},
			taskToAllocate: &api.TaskInfo{
				UID:       "task-gpu",
				Name:      "gpu-task",
				Namespace: "default",
				Job:       "job-gpu",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							"volcano.sh/card.name": "NVIDIA-H200",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			expectedAllocatedCPU: 4000,
			expectedAllocatedMem: 8 * 1024 * 1024 * 1024,
			expectedAllocatedGPU: 2000, // 2 GPUs * 1000
			expectedShareGreater: 1,    // max(4000/4000, 8Gi/8Gi) = 1
			shouldSucceed:        true,
		},
		{
			name: "allocate task with GPU card resources and card unlimited cpu memory",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:      map[api.QueueID]*queueAttr{},
					totalResource:  api.EmptyResource(),
					totalGuarantee: api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
						"NVIDIA-H200": "nvidia.com/gpu",
					},
					isCardUnlimitedCpuMemory: true,
				}
				p.queueOpts["queue-gpu"] = &queueAttr{
					queueID:   "queue-gpu",
					name:      "gpu-queue",
					allocated: api.EmptyResource(),
					deserved: &api.Resource{
						MilliCPU: 4000,
						Memory:   8 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 4000, // 4 GPUs
						},
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-queue",
						Annotations: map[string]string{
							"volcano.sh/card.quota": `{"NVIDIA-H200": 4}`,
						},
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				}

				job := &api.JobInfo{
					UID:       "job-gpu",
					Name:      "gpu-job",
					Namespace: "default",
					Queue:     "queue-gpu",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-gpu": {
						UID:   "queue-gpu",
						Name:  "gpu-queue",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-gpu": job,
				}

				return session
			},
			taskToAllocate: &api.TaskInfo{
				UID:       "task-gpu",
				Name:      "gpu-task",
				Namespace: "default",
				Job:       "job-gpu",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							"volcano.sh/card.name": "NVIDIA-H200",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			expectedAllocatedCPU: 0,
			expectedAllocatedMem: 0,
			expectedAllocatedGPU: 2000, // 2 GPUs * 1000
			expectedShareGreater: 0.5,  // max(2000/4000) = 0.5
			shouldSucceed:        true,
		},
		{
			name: "allocate task without pod but with Resreq",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:              map[api.QueueID]*queueAttr{},
					totalResource:          api.EmptyResource(),
					totalGuarantee:         api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
				}
				p.queueOpts["queue-1"] = &queueAttr{
					queueID:   "queue-1",
					name:      "test-queue-1",
					allocated: api.EmptyResource(),
					deserved: &api.Resource{
						MilliCPU: 10000,
						Memory:   20 * 1024 * 1024 * 1024,
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue-1",
					},
				}

				job := &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job-1",
					Namespace: "default",
					Queue:     "queue-1",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job-1",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:   "queue-1",
						Name:  "test-queue-1",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-1": job,
				}

				return session
			},
			taskToAllocate: &api.TaskInfo{
				UID:       "task-no-pod",
				Name:      "task-no-pod",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
				Pod: nil, // No pod, but Resreq is still allocated
			},
			expectedAllocatedCPU: 1000, // Still allocated even without pod
			expectedAllocatedMem: 1 * 1024 * 1024 * 1024,
			expectedShareGreater: 0.1, // 1000/10000 = 0.1
			shouldSucceed:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := tt.setupPlugin()
			session := tt.setupSession()

			event := &framework.Event{
				Task: tt.taskToAllocate,
			}

			// Call OnAllocate
			plugin.OnAllocate(session, event)

			// Verify the allocated resources
			job := session.Jobs[tt.taskToAllocate.Job]
			if job == nil {
				t.Fatalf("Job not found for task %s", tt.taskToAllocate.Name)
			}

			qAttr := plugin.queueOpts[job.Queue]
			if qAttr == nil {
				t.Fatalf("Queue attributes not found for queue %s", job.Queue)
			}

			if tt.shouldSucceed {
				if qAttr.allocated.MilliCPU != tt.expectedAllocatedCPU {
					t.Errorf("Expected allocated CPU %v, got %v", tt.expectedAllocatedCPU, qAttr.allocated.MilliCPU)
				}

				if qAttr.allocated.Memory != tt.expectedAllocatedMem {
					t.Errorf("Expected allocated memory %v, got %v", tt.expectedAllocatedMem, qAttr.allocated.Memory)
				}

				if tt.expectedAllocatedGPU > 0 {
					if qAttr.allocated.ScalarResources == nil {
						t.Errorf("Expected scalar resources to be allocated, got nil")
					} else {
						allocatedGPU := qAttr.allocated.ScalarResources["NVIDIA-H200"]
						if allocatedGPU != tt.expectedAllocatedGPU {
							t.Errorf("Expected allocated GPU %v, got %v", tt.expectedAllocatedGPU, allocatedGPU)
						}
					}
				}

				if tt.expectedShareGreater >= 0 && qAttr.share != tt.expectedShareGreater {
					t.Errorf("Expected share %v, got %v", tt.expectedShareGreater, qAttr.share)
				}
			}
		})
	}
}

// TestOnDeallocate tests the OnDeallocate event handler
func TestOnDeallocate(t *testing.T) {
	tests := []struct {
		name                 string
		setupPlugin          func() *Plugin
		setupSession         func() *framework.Session
		taskToDeallocate     *api.TaskInfo
		initialAllocatedCPU  float64
		initialAllocatedMem  float64
		initialAllocatedGPU  float64
		expectedAllocatedCPU float64
		expectedAllocatedMem float64
		expectedAllocatedGPU float64
		shouldSucceed        bool
	}{
		{
			name: "deallocate task with CPU and memory",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:              map[api.QueueID]*queueAttr{},
					totalResource:          api.EmptyResource(),
					totalGuarantee:         api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
				}
				p.queueOpts["queue-1"] = &queueAttr{
					queueID: "queue-1",
					name:    "test-queue-1",
					allocated: &api.Resource{
						MilliCPU: 5000, // Already allocated
						Memory:   5 * 1024 * 1024 * 1024,
					},
					deserved: &api.Resource{
						MilliCPU: 10000,
						Memory:   10 * 1024 * 1024 * 1024,
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue-1",
					},
				}

				job := &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job-1",
					Namespace: "default",
					Queue:     "queue-1",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job-1",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:   "queue-1",
						Name:  "test-queue-1",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-1": job,
				}

				return session
			},
			taskToDeallocate: &api.TaskInfo{
				UID:       "task-1",
				Name:      "test-task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-task-1",
						Namespace: "default",
					},
				},
			},
			initialAllocatedCPU:  5000,
			initialAllocatedMem:  5 * 1024 * 1024 * 1024,
			expectedAllocatedCPU: 3000, // 5000 - 2000
			expectedAllocatedMem: 3 * 1024 * 1024 * 1024,
			shouldSucceed:        true,
		},
		{
			name: "deallocate task with GPU resources and card unlimited cpu memory enabled",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:      map[api.QueueID]*queueAttr{},
					totalResource:  api.EmptyResource(),
					totalGuarantee: api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
						"NVIDIA-H200": "nvidia.com/gpu",
					},
					isCardUnlimitedCpuMemory: true,
				}
				p.queueOpts["queue-gpu"] = &queueAttr{
					queueID: "queue-gpu",
					name:    "gpu-queue",
					allocated: &api.Resource{
						MilliCPU: 8000,
						Memory:   16 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 4000, // 4 GPUs allocated
						},
					},
					deserved: &api.Resource{
						MilliCPU: 16000,
						Memory:   32 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 8000,
						},
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-queue",
					},
				}

				job := &api.JobInfo{
					UID:       "job-gpu",
					Name:      "gpu-job",
					Namespace: "default",
					Queue:     "queue-gpu",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-gpu": {
						UID:   "queue-gpu",
						Name:  "gpu-queue",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-gpu": job,
				}

				return session
			},
			taskToDeallocate: &api.TaskInfo{
				UID:       "task-gpu",
				Name:      "gpu-task",
				Namespace: "default",
				Job:       "job-gpu",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							"volcano.sh/card.name": "NVIDIA-H200",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			initialAllocatedCPU:  8000,
			initialAllocatedMem:  16 * 1024 * 1024 * 1024,
			initialAllocatedGPU:  4000,
			expectedAllocatedCPU: 8000,
			expectedAllocatedMem: 16 * 1024 * 1024 * 1024,
			expectedAllocatedGPU: 2000, // 4000 - 2000
			shouldSucceed:        true,
		},
		{
			name: "deallocate task with GPU resources and card unlimited cpu memory disabled",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:      map[api.QueueID]*queueAttr{},
					totalResource:  api.EmptyResource(),
					totalGuarantee: api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
						"NVIDIA-H200": "nvidia.com/gpu",
					},
					isCardUnlimitedCpuMemory: false,
				}
				p.queueOpts["queue-gpu"] = &queueAttr{
					queueID: "queue-gpu",
					name:    "gpu-queue",
					allocated: &api.Resource{
						MilliCPU: 8000,
						Memory:   16 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 4000, // 4 GPUs allocated
						},
					},
					deserved: &api.Resource{
						MilliCPU: 16000,
						Memory:   32 * 1024 * 1024 * 1024,
						ScalarResources: map[v1.ResourceName]float64{
							"NVIDIA-H200": 8000,
						},
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "gpu-queue",
					},
				}

				job := &api.JobInfo{
					UID:       "job-gpu",
					Name:      "gpu-job",
					Namespace: "default",
					Queue:     "queue-gpu",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-gpu": {
						UID:   "queue-gpu",
						Name:  "gpu-queue",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-gpu": job,
				}

				return session
			},
			taskToDeallocate: &api.TaskInfo{
				UID:       "task-gpu",
				Name:      "gpu-task",
				Namespace: "default",
				Job:       "job-gpu",
				Resreq: &api.Resource{
					MilliCPU: 4000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "gpu-task",
						Namespace: "default",
						Annotations: map[string]string{
							"volcano.sh/card.name": "NVIDIA-H200",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Limits: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			initialAllocatedCPU:  8000,
			initialAllocatedMem:  16 * 1024 * 1024 * 1024,
			initialAllocatedGPU:  4000,
			expectedAllocatedCPU: 4000,
			expectedAllocatedMem: 8 * 1024 * 1024 * 1024,
			expectedAllocatedGPU: 2000, // 4000 - 2000
			shouldSucceed:        true,
		},
		{
			name: "deallocate task without pod but with Resreq",
			setupPlugin: func() *Plugin {
				p := &Plugin{
					queueOpts:              map[api.QueueID]*queueAttr{},
					totalResource:          api.EmptyResource(),
					totalGuarantee:         api.EmptyResource(),
					cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
				}
				p.queueOpts["queue-1"] = &queueAttr{
					queueID: "queue-1",
					name:    "test-queue-1",
					allocated: &api.Resource{
						MilliCPU: 5000,
						Memory:   5 * 1024 * 1024 * 1024,
					},
					deserved: &api.Resource{
						MilliCPU: 10000,
						Memory:   10 * 1024 * 1024 * 1024,
					},
				}
				return p
			},
			setupSession: func() *framework.Session {
				mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
				session := framework.OpenSession(mockCache, nil, nil)

				queue := &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue-1",
					},
				}

				job := &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job-1",
					Namespace: "default",
					Queue:     "queue-1",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job-1",
								Namespace: "default",
							},
						},
					},
				}

				session.Queues = map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:   "queue-1",
						Name:  "test-queue-1",
						Queue: queue,
					},
				}
				session.Jobs = map[api.JobID]*api.JobInfo{
					"job-1": job,
				}

				return session
			},
			taskToDeallocate: &api.TaskInfo{
				UID:       "task-no-pod",
				Name:      "task-no-pod",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
				},
				Pod: nil, // No pod, but Resreq is still deallocated
			},
			initialAllocatedCPU:  5000,
			initialAllocatedMem:  5 * 1024 * 1024 * 1024,
			expectedAllocatedCPU: 4000, // 5000 - 1000, still deallocated even without pod
			expectedAllocatedMem: 4 * 1024 * 1024 * 1024,
			shouldSucceed:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := tt.setupPlugin()
			session := tt.setupSession()

			event := &framework.Event{
				Task: tt.taskToDeallocate,
			}

			// Call OnDeallocate
			plugin.OnDeallocate(session, event)

			// Verify the deallocated resources
			job := session.Jobs[tt.taskToDeallocate.Job]
			if job == nil {
				t.Fatalf("Job not found for task %s", tt.taskToDeallocate.Name)
			}

			qAttr := plugin.queueOpts[job.Queue]
			if qAttr == nil {
				t.Fatalf("Queue attributes not found for queue %s", job.Queue)
			}

			if qAttr.allocated.MilliCPU != tt.expectedAllocatedCPU {
				t.Errorf("Expected allocated CPU %v, got %v", tt.expectedAllocatedCPU, qAttr.allocated.MilliCPU)
			}

			if qAttr.allocated.Memory != tt.expectedAllocatedMem {
				t.Errorf("Expected allocated memory %v, got %v", tt.expectedAllocatedMem, qAttr.allocated.Memory)
			}

			if tt.expectedAllocatedGPU > 0 {
				if qAttr.allocated.ScalarResources == nil {
					t.Errorf("Expected scalar resources, got nil")
				} else {
					allocatedGPU := qAttr.allocated.ScalarResources["NVIDIA-H200"]
					if allocatedGPU != tt.expectedAllocatedGPU {
						t.Errorf("Expected allocated GPU %v, got %v", tt.expectedAllocatedGPU, allocatedGPU)
					}
				}
			}
		})
	}
}
