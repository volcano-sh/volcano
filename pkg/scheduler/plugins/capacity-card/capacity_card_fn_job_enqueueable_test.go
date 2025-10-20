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
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// TestJobEnqueueableFn tests the JobEnqueueableFn function
func TestJobEnqueueableFn(t *testing.T) {
	tests := []struct {
		name        string
		plugin      *Plugin
		jobInfo     *api.JobInfo
		queueState  scheduling.QueueState
		expected    int
		description string
	}{
		{
			name: "queue is open and job is enqueueable",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-1": {
						queueID: "queue-1",
						name:    "test-queue",
						allocated: &api.Resource{
							MilliCPU: 2000,
							Memory:   2 * 1024 * 1024 * 1024,
						},
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-1",
				Name:         "test-job",
				Namespace:    "default",
				Queue:        "queue-1",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "default",
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "test-task",
						Namespace: "default",
						Resreq: &api.Resource{
							MilliCPU: 1000,
							Memory:   1 * 1024 * 1024 * 1024,
						},
					},
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    util.Permit,
			description: "Queue is open and has enough resources, should permit",
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
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-2",
				Name:         "test-job-2",
				Namespace:    "default",
				Queue:        "queue-2",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job-2",
							Namespace: "default",
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
			},
			queueState:  scheduling.QueueStateClosed,
			expected:    util.Reject,
			description: "Queue is closed, should reject job",
		},
		{
			name: "queue has insufficient CPU quota",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-3": {
						queueID: "queue-3",
						name:    "cpu-limited-queue",
						allocated: &api.Resource{
							MilliCPU: 8000,
							Memory:   2 * 1024 * 1024 * 1024,
						},
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-3",
				Name:         "cpu-heavy-job",
				Namespace:    "default",
				Queue:        "queue-3",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cpu-heavy-job",
							Namespace: "default",
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("3"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    util.Reject,
			description: "Queue has insufficient CPU quota, should reject job",
		},
		{
			name: "queue has insufficient Memory quota",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-4": {
						queueID: "queue-4",
						name:    "memory-limited-queue",
						allocated: &api.Resource{
							MilliCPU: 2000,
							Memory:   8 * 1024 * 1024 * 1024,
						},
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-4",
				Name:         "memory-heavy-job",
				Namespace:    "default",
				Queue:        "queue-4",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "memory-heavy-job",
							Namespace: "default",
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("3Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    util.Reject,
			description: "Queue has insufficient memory quota, should reject job",
		},
		{
			name: "queue has insufficient scalar quota (GPU)",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-5": {
						queueID: "queue-5",
						name:    "gpu-limited-queue",
						allocated: &api.Resource{
							MilliCPU: 2000,
							Memory:   2 * 1024 * 1024 * 1024,
							ScalarResources: map[v1.ResourceName]float64{
								"NVIDIA-A100": 3500, // 3.5 GPUs allocated
							},
						},
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
							ScalarResources: map[v1.ResourceName]float64{
								"NVIDIA-A100": 4000, // Only 4 GPUs available
							},
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-5",
				Name:         "gpu-job",
				Namespace:    "default",
				Queue:        "queue-5",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "gpu-job",
							Namespace: "default",
							Annotations: map[string]string{
								JobAnnotationKeyCardRequest: `{"NVIDIA-A100": 1}`,
							},
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("1"),
								v1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    util.Reject,
			description: "Queue has insufficient GPU quota, should reject job",
		},
		{
			name: "job with card annotation and sufficient resources",
			plugin: &Plugin{
				queueOpts: map[api.QueueID]*queueAttr{
					"queue-6": {
						queueID: "queue-6",
						name:    "gpu-queue",
						allocated: &api.Resource{
							MilliCPU: 2000,
							Memory:   2 * 1024 * 1024 * 1024,
							ScalarResources: map[v1.ResourceName]float64{
								"NVIDIA-A100": 2000, // 2 GPUs allocated
							},
						},
						inqueue: api.EmptyResource(),
						elastic: api.EmptyResource(),
						capability: &api.Resource{
							MilliCPU: 10000,
							Memory:   10 * 1024 * 1024 * 1024,
							ScalarResources: map[v1.ResourceName]float64{
								"NVIDIA-A100": 8000, // 8 GPUs available
							},
						},
					},
				},
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &api.JobInfo{
				UID:          "job-6",
				Name:         "gpu-job-ok",
				Namespace:    "default",
				Queue:        "queue-6",
				MinAvailable: 1,
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "gpu-job-ok",
							Namespace: "default",
							Annotations: map[string]string{
								JobAnnotationKeyCardRequest: `{"NVIDIA-A100": 2}`,
							},
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("2"),
								v1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
					Version: api.PodGroupVersionV1Beta1,
				},
			},
			queueState:  scheduling.QueueStateOpen,
			expected:    util.Permit,
			description: "Job with card annotation and sufficient resources should be permitted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create session using mock cache
			mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

			// Create queue info
			queueInfo := &api.QueueInfo{
				UID:  api.QueueID(tt.jobInfo.Queue),
				Name: tt.plugin.queueOpts[api.QueueID(tt.jobInfo.Queue)].name,
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: tt.plugin.queueOpts[api.QueueID(tt.jobInfo.Queue)].name,
					},
					Status: scheduling.QueueStatus{
						State: tt.queueState,
					},
				},
			}

			ssn := framework.OpenSession(mockCache, nil, nil)
			defer framework.CloseSession(ssn)

			// Add queue to session
			ssn.Queues[queueInfo.UID] = queueInfo

			result := tt.plugin.JobEnqueueableFn(ssn, tt.jobInfo)
			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}

// TestIsJobEnqueueable tests the isJobEnqueueable function
func TestIsJobEnqueueable(t *testing.T) {
	// Create a mock session for event recording
	mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	ssn := framework.OpenSession(mockCache, nil, nil)
	defer framework.CloseSession(ssn)

	tests := []struct {
		name        string
		plugin      *Plugin
		qAttr       *queueAttr
		job         *JobInfo
		expected    bool
		description string
	}{
		{
			name: "cpu and memory job with sufficient resources and isCardUnlimitedCpuMemory is false",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Job with sufficient resources and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with insufficient cpu and isCardUnlimitedCpuMemory is false",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("9"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient cpu and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with insufficient memory and isCardUnlimitedCpuMemory is false",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("9Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient memory and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with sufficient resources and isCardUnlimitedCpuMemory is true",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Job with sufficient resources and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with insufficient cpu and isCardUnlimitedCpuMemory is true",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("9"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient cpu and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with insufficient memory and isCardUnlimitedCpuMemory is true",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("9Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient memory and isCardUnlimitedCpuMemory is false should be enqueueable",
		},
		{
			name: "cpu and memory job with sufficient elastic resources",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: &api.Resource{
					MilliCPU: 1000,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("9"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Job with sufficient elastic resources should be enqueueable",
		},
		{
			name: "cpu and memory job with sufficient elastic resources",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: &api.Resource{
					Memory: 1 * 1024 * 1024 * 1024,
				},
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("9Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Job with sufficient elastic resources should be enqueueable",
		},
		{
			name: "cpu and memory job with insufficient inqueue cpu",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: &api.Resource{
					MilliCPU: 1000,
				},
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient inqueue cpu should be rejected",
		},
		{
			name: "cpu and memory job with insufficient inqueue memory",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-1",
				name:    "test-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: &api.Resource{
					Memory: 1 * 1024 * 1024 * 1024,
				},
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-1",
					Name:      "test-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("8"),
									v1.ResourceMemory: resource.MustParse("8Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    false,
			description: "Job with insufficient inqueue cpu should be rejected",
		},
		{
			name: "job with nil resource request",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-2",
				name:    "test-queue-2",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-2",
					Name:      "nil-request-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "nil-request-job",
								Namespace: "default",
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: nil,
			},
			expected:    true,
			description: "Job with nil request should be allowed if allocated is within capability",
		},
		{
			name: "job with no resource quota used - should allow",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID:   "queue-5",
				name:      "empty-queue",
				allocated: api.EmptyResource(),
				inqueue:   api.EmptyResource(),
				elastic:   api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-5",
					Name:      "test-job-5",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-job-5",
								Namespace: "default",
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Should allow when queue has no allocated resources",
		},
		{
			name: "zero CPU request should not trigger CPU check",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-6",
				name:    "zero-cpu-queue",
				allocated: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-6",
					Name:      "zero-cpu-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "zero-cpu-job",
								Namespace: "default",
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Zero CPU request should skip CPU quota check",
		},
		{
			name: "sufficient scalar quota",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-8",
				name:    "gpu-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 3000,
					},
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 4000,
					},
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-8",
					Name:      "gpu-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("10"),
									v1.ResourceMemory: resource.MustParse("10Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 1000,
					},
				},
			},
			expected:    true,
			description: "Should allow job when scalar quota is sufficient",
		},
		{
			name: "insufficient scalar quota",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			qAttr: &queueAttr{
				queueID: "queue-8",
				name:    "gpu-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 3000,
					},
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 4000,
					},
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-8",
					Name:      "gpu-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("10"),
									v1.ResourceMemory: resource.MustParse("10Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000,
					},
				},
			},
			expected:    false,
			description: "Should reject job when scalar quota is insufficient",
		},
		{
			name: "insufficient memory and cpu quota and isCardUnlimitedCpuMemory is false",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID: "queue-8",
				name:    "gpu-limited-queue",
				allocated: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 3000,
					},
				},
				inqueue: api.EmptyResource(),
				elastic: api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 4000,
					},
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-8",
					Name:      "gpu-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "gpu-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("10"),
									v1.ResourceMemory: resource.MustParse("10Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 1000,
					},
				},
			},
			expected:    false,
			description: "Should reject job when memory quota is insufficient",
		},
		{
			name: "ignored scalar resource (pods) should not be checked",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			qAttr: &queueAttr{
				queueID:   "queue-11",
				name:      "ignore-scalar-queue",
				allocated: api.EmptyResource(),
				inqueue:   api.EmptyResource(),
				elastic:   api.EmptyResource(),
				capability: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourcePods: 0, // Pods is set to 0 to simulate exceeding quota
					},
				},
			},
			job: &JobInfo{
				JobInfo: &api.JobInfo{
					UID:       "job-11",
					Name:      "ignore-scalar-job",
					Namespace: "default",
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "ignore-scalar-job",
								Namespace: "default",
							},
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("1"),
									v1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
						Version: api.PodGroupVersionV1Beta1,
					},
				},
				preCheckCardResource: &api.Resource{},
			},
			expected:    true,
			description: "Pods resource should be ignored and not block enqueue even when quota is exceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.plugin.isJobEnqueueable(ssn, tt.qAttr, tt.job)
			if result != tt.expected {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expected, result)
			}
		})
	}
}
