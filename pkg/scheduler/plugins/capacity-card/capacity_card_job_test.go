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
	"k8s.io/client-go/kubernetes/fake"
	v1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestNewJobInfo(t *testing.T) {
	tests := []struct {
		name                     string
		plugin                   *Plugin
		job                      *api.JobInfo
		expectedError            bool
		expectedAllocatedCPU     float64
		expectedAllocatedMemory  float64
		expectedPreCheckCardName v1.ResourceName
		expectedPreCheckCardQty  float64
		description              string
	}{
		{
			name: "job with card request annotation and card unlimited cpu memory disabled",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{"NVIDIA-A100": "nvidia.com/gpu"},
				isCardUnlimitedCpuMemory: false,
			},
			job: &api.JobInfo{
				UID:       "job-1",
				Name:      "test-job",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "default",
							Annotations: map[string]string{
								JobAnnotationKeyCardRequest: `{"NVIDIA-A100": 2}`,
							},
						},
					},
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "task-1",
						Namespace: "default",
						TransactionContext: api.TransactionContext{
							Status: api.Running,
						},
						Resreq: &api.Resource{
							MilliCPU: 1000,
							Memory:   1024 * 1024 * 1024,
						},
						Pod: &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "task-1",
								Namespace: "default",
								Annotations: map[string]string{
									TaskAnnotationKeyCardName: "NVIDIA-A100",
								},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError:            false,
			expectedAllocatedCPU:     1000,
			expectedAllocatedMemory:  1024 * 1024 * 1024,
			expectedPreCheckCardName: "NVIDIA-A100",
			expectedPreCheckCardQty:  1000,
			description:              "Should create JobInfo with card request annotation and allocated resources when card unlimited cpu memory is enabled",
		},
		{
			name: "job with card request annotation and card unlimited cpu memory enabled",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{"NVIDIA-A100": "nvidia.com/gpu"},
				isCardUnlimitedCpuMemory: true,
			},
			job: &api.JobInfo{
				UID:       "job-1",
				Name:      "test-job",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job",
							Namespace: "default",
							Annotations: map[string]string{
								JobAnnotationKeyCardRequest: `{"NVIDIA-A100": 2}`,
							},
						},
					},
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "task-1",
						Namespace: "default",
						TransactionContext: api.TransactionContext{
							Status: api.Running,
						},
						Resreq: &api.Resource{
							MilliCPU: 1000,
							Memory:   1024 * 1024 * 1024,
						},
						Pod: &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "task-1",
								Namespace: "default",
								Annotations: map[string]string{
									TaskAnnotationKeyCardName: "NVIDIA-A100",
								},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError:            false,
			expectedAllocatedCPU:     0,
			expectedAllocatedMemory:  0,
			expectedPreCheckCardName: "NVIDIA-A100",
			expectedPreCheckCardQty:  1000,
			description:              "Should create JobInfo with card request annotation and allocated resources when card unlimited cpu memory is disabled",
		},
		{
			name: "job without card request annotation",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			job: &api.JobInfo{
				UID:       "job-2",
				Name:      "test-job-2",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job-2",
							Namespace: "default",
						},
					},
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "task-1",
						Namespace: "default",
						TransactionContext: api.TransactionContext{
							Status: api.Running,
						},
						Resreq: &api.Resource{
							MilliCPU: 2000,
							Memory:   2 * 1024 * 1024 * 1024,
						},
						Pod: &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "task-1",
								Namespace: "default",
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												v1.ResourceCPU:    resource.MustParse("2"),
												v1.ResourceMemory: resource.MustParse("2Gi"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError:           false,
			expectedAllocatedCPU:    2000,
			expectedAllocatedMemory: 2 * 1024 * 1024 * 1024,
			description:             "Should create JobInfo without card request annotation",
		},
		{
			name: "job with pending task",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{"NVIDIA-A100": "nvidia.com/gpu"},
				isCardUnlimitedCpuMemory: false,
			},
			job: &api.JobInfo{
				UID:       "job-3",
				Name:      "test-job-3",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job-3",
							Namespace: "default",
						},
					},
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "task-1",
						Namespace: "default",
						TransactionContext: api.TransactionContext{
							Status: api.Pending,
						},
						Resreq: &api.Resource{
							MilliCPU: 1000,
							Memory:   1024 * 1024 * 1024,
						},
						Pod: &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "task-1",
								Namespace: "default",
								Annotations: map[string]string{
									TaskAnnotationKeyCardName: "NVIDIA-A100",
								},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Resources: v1.ResourceRequirements{
											Requests: v1.ResourceList{
												"nvidia.com/gpu": resource.MustParse("1"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expectedError:            false,
			expectedAllocatedCPU:     0,
			expectedAllocatedMemory:  0,
			expectedPreCheckCardName: "NVIDIA-A100",
			expectedPreCheckCardQty:  1000,
			description:              "Should not count pending task in allocated resources",
		},
		{
			name: "job with task that has invalid card",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: false,
			},
			job: &api.JobInfo{
				UID:       "job-4",
				Name:      "test-job-4",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-job-4",
							Namespace: "default",
						},
					},
				},
				Tasks: map[api.TaskID]*api.TaskInfo{
					"task-1": {
						UID:       "task-1",
						Name:      "task-1",
						Namespace: "default",
						TransactionContext: api.TransactionContext{
							Status: api.Running,
						},
						Resreq: &api.Resource{
							MilliCPU: 1000,
							Memory:   1024 * 1024 * 1024,
						},
						Pod: &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "task-1",
								Namespace: "default",
								Annotations: map[string]string{
									TaskAnnotationKeyCardName: "INVALID-CARD",
								},
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
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
				},
			},
			expectedError: false,
			description:   "Should not return error when task has invalid card name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobInfo, err := tt.plugin.NewJobInfo(tt.job)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if jobInfo == nil {
				t.Errorf("Expected JobInfo but got nil")
				return
			}

			if jobInfo.allocated.MilliCPU != tt.expectedAllocatedCPU {
				t.Errorf("Expected allocated CPU %v, got %v", tt.expectedAllocatedCPU, jobInfo.allocated.MilliCPU)
			}

			if jobInfo.allocated.Memory != tt.expectedAllocatedMemory {
				t.Errorf("Expected allocated Memory %v, got %v", tt.expectedAllocatedMemory, jobInfo.allocated.Memory)
			}

			if tt.expectedPreCheckCardName != "" {
				if qty, ok := jobInfo.preCheckCardResource.ScalarResources[tt.expectedPreCheckCardName]; !ok {
					t.Errorf("Expected preCheckCardResource to have %v", tt.expectedPreCheckCardName)
				} else if qty != tt.expectedPreCheckCardQty {
					t.Errorf("Expected preCheckCardResource quantity %v, got %v", tt.expectedPreCheckCardQty, qty)
				}
			}
		})
	}
}

func TestGetMinResources(t *testing.T) {
	tests := []struct {
		name                 string
		plugin               *Plugin
		jobInfo              *JobInfo
		expectedCPU          float64
		expectedMemory       float64
		expectedCardName     v1.ResourceName
		expectedCardQuantity float64
		description          string
	}{
		{
			name: "job with card resource and cardUnlimitedCpuMemory enabled",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: true,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("4"),
									v1.ResourceMemory: resource.MustParse("4Gi"),
									"nvidia.com/gpu":  resource.MustParse("2"),
								},
							},
						},
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000,
					},
				},
			},
			expectedCardName:     "NVIDIA-A100",
			expectedCardQuantity: 2000,
			description:          "Should not include MinResources CPU/Memory when card resource exists and cardUnlimitedCpuMemory is enabled",
		},
		{
			name: "job with card resource and cardUnlimitedCpuMemory disabled",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("4"),
									v1.ResourceMemory: resource.MustParse("4Gi"),
									"nvidia.com/gpu":  resource.MustParse("2"),
								},
							},
						},
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"NVIDIA-A100": 2000,
					},
				},
			},
			expectedCPU:          4000,                   // MinResources CPU replaces preCheckCardResource CPU
			expectedMemory:       4 * 1024 * 1024 * 1024, // MinResources Memory replaces preCheckCardResource Memory
			expectedCardName:     "NVIDIA-A100",
			expectedCardQuantity: 2000,
			description:          "Should replace with MinResources CPU/Memory when cardUnlimitedCpuMemory is disabled",
		},
		{
			name: "job without card resource",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: true,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
									"nvidia.com/gpu":  resource.MustParse("2"),
								},
							},
						},
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{},
				},
			},
			expectedCPU:    2000,
			expectedMemory: 2 * 1024 * 1024 * 1024,
			description:    "Should include CPU/Memory when no card resource exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minRes := tt.plugin.GetMinResources(tt.jobInfo)

			if minRes.MilliCPU != tt.expectedCPU {
				t.Errorf("Expected CPU %v, got %v", tt.expectedCPU, minRes.MilliCPU)
			}

			if minRes.Memory != tt.expectedMemory {
				t.Errorf("Expected Memory %v, got %v", tt.expectedMemory, minRes.Memory)
			}

			if tt.expectedCardName != "" {
				if qty, ok := minRes.ScalarResources[tt.expectedCardName]; !ok {
					t.Errorf("Expected card resource %v", tt.expectedCardName)
				} else if qty != tt.expectedCardQuantity {
					t.Errorf("Expected card quantity %v, got %v", tt.expectedCardQuantity, qty)
				}
			}
		})
	}
}

func TestGetElasticResources(t *testing.T) {
	tests := []struct {
		name                 string
		plugin               *Plugin
		jobInfo              *JobInfo
		expectedCPU          float64
		expectedMemory       float64
		expectedCardName     v1.ResourceName
		expectedCardQuantity float64
		description          string
	}{
		{
			name: "job with elastic resources and card unlimited cpu memory disabled",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
									"nvidia.com/gpu":  resource.MustParse("2"),
								},
							},
						},
					},
				},
				allocated: &api.Resource{
					MilliCPU: 4000,
					Memory:   4 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 6000,
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 4000,
					},
				},
			},
			expectedCPU:          2000,
			expectedMemory:       2 * 1024 * 1024 * 1024,
			expectedCardName:     "Tesla-V100",
			expectedCardQuantity: 2000,
			description:          "Should return elastic resources (allocated - min)",
		},

		{
			name: "job with elastic resources and card unlimited cpu memory enabled",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: true,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
									"nvidia.com/gpu":  resource.MustParse("2"),
								},
							},
						},
					},
				},
				allocated: &api.Resource{
					MilliCPU: 4000,
					Memory:   4 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 6000,
					},
				},
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 4000,
					},
				},
			},
			expectedCPU:          4000,
			expectedMemory:       4 * 1024 * 1024 * 1024,
			expectedCardName:     "Tesla-V100",
			expectedCardQuantity: 2000,
			description:          "Should return elastic resources (allocated - min)",
		},
		{
			name: "job without allocated resources",
			plugin: &Plugin{
				isCardUnlimitedCpuMemory: false,
			},
			jobInfo: &JobInfo{
				JobInfo: &api.JobInfo{
					PodGroup: &api.PodGroup{
						PodGroup: scheduling.PodGroup{
							Spec: scheduling.PodGroupSpec{
								MinResources: &v1.ResourceList{
									v1.ResourceCPU:    resource.MustParse("2"),
									v1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
				allocated:            nil,
				preCheckCardResource: &api.Resource{},
			},
			expectedCPU:          0,
			expectedMemory:       0,
			expectedCardName:     "",
			expectedCardQuantity: 0,
			description:          "Should return empty resource when allocated is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			elastic := tt.plugin.GetElasticResources(tt.jobInfo)

			if elastic.MilliCPU != tt.expectedCPU {
				t.Errorf("Expected elastic CPU %v, got %v", tt.expectedCPU, elastic.MilliCPU)
			}

			if elastic.Memory != tt.expectedMemory {
				t.Errorf("Expected elastic Memory %v, got %v", tt.expectedMemory, elastic.Memory)
			}

			if tt.expectedCardName != "" {
				if qty, ok := elastic.ScalarResources[tt.expectedCardName]; !ok {
					t.Errorf("Expected card resource %v", tt.expectedCardName)
				} else if qty != tt.expectedCardQuantity {
					t.Errorf("Expected card quantity %v, got %v", tt.expectedCardQuantity, qty)
				}
			}
		})
	}
}

func TestGetCardNameFromTask(t *testing.T) {
	tests := []struct {
		name         string
		plugin       *Plugin
		task         *api.TaskInfo
		expectedName string
		description  string
	}{
		{
			name:   "task with card name annotation",
			plugin: &Plugin{},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
				},
			},
			expectedName: "NVIDIA-A100",
			description:  "Should return card name from annotation",
		},
		{
			name:   "task without card name annotation",
			plugin: &Plugin{},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			expectedName: "",
			description:  "Should return empty string when no card name annotation",
		},
		{
			name:   "task without pod",
			plugin: &Plugin{},
			task: &api.TaskInfo{
				Pod: nil,
			},
			expectedName: "",
			description:  "Should return empty string when pod is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cardName := tt.plugin.getCardNameFromTask(tt.task)

			if cardName != tt.expectedName {
				t.Errorf("Expected card name %v, got %v", tt.expectedName, cardName)
			}
		})
	}
}

func TestGetCardResourceFromTaskPod(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		cardName      string
		pod           *v1.Pod
		expectedCard  v1.ResourceName
		expectedQty   float64
		expectedError bool
		description   string
	}{
		{
			name: "pod with card in limits",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			cardName: "NVIDIA-A100",
			pod: &v1.Pod{
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
			expectedCard:  "NVIDIA-A100",
			expectedQty:   2000,
			expectedError: false,
			description:   "Should get card resource from limits",
		},
		{
			name: "pod with card in requests",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			cardName: "NVIDIA-A100",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									"nvidia.com/gpu": resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expectedCard:  "NVIDIA-A100",
			expectedQty:   1000,
			expectedError: false,
			description:   "Should get card resource from requests",
		},
		{
			name: "pod without card resource",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			cardName: "NVIDIA-A100",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Resources: v1.ResourceRequirements{
								Requests: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			expectedError: true,
			description:   "Should return error when card resource not found",
		},
		{
			name: "unknown card name",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
			},
			cardName: "UNKNOWN-CARD",
			pod: &v1.Pod{
				Spec: v1.PodSpec{
					Containers: []v1.Container{},
				},
			},
			expectedError: true,
			description:   "Should return error for unknown card name",
		},
		{
			name: "nil pod",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			cardName:      "NVIDIA-A100",
			pod:           nil,
			expectedError: true,
			description:   "Should return error when pod is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.plugin.getCardResourceFromTaskPod(tt.cardName, tt.pod)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if qty, ok := res.ScalarResources[tt.expectedCard]; !ok {
				t.Errorf("Expected card resource %v", tt.expectedCard)
			} else if qty != tt.expectedQty {
				t.Errorf("Expected card quantity %v, got %v", tt.expectedQty, qty)
			}
		})
	}
}

func TestGetCardResourceFromTask(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		task          *api.TaskInfo
		expectedCard  v1.ResourceName
		expectedQty   float64
		expectedError bool
		description   string
	}{
		{
			name: "task with single card",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			expectedCard:  "NVIDIA-A100",
			expectedQty:   2000,
			expectedError: false,
			description:   "Should get card resource from single card task",
		},
		{
			name: "task without card",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
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
			expectedError: false,
			description:   "Should return empty resource for CPU-only task",
		},
		{
			name:   "task without pod",
			plugin: &Plugin{},
			task: &api.TaskInfo{
				Pod: nil,
			},
			expectedError: false,
			description:   "Should return empty resource when pod is nil",
		},
		{
			name: "multi-card task without bound node",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
					"NVIDIA-H100": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "", // Not bound to any node
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expectedCard:  "NVIDIA-A100|NVIDIA-H100",
			expectedQty:   1000,
			expectedError: false,
			description:   "Should get multi-card resource from unbound pod using first card name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.plugin.getCardResourceFromTask(tt.task)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.expectedCard != "" {
				if qty, ok := res.ScalarResources[tt.expectedCard]; !ok {
					t.Errorf("Expected card resource %v", tt.expectedCard)
				} else if qty != tt.expectedQty {
					t.Errorf("Expected card quantity %v, got %v", tt.expectedQty, qty)
				}
			}
		})
	}
}

func TestGetTaskRequestResources(t *testing.T) {
	tests := []struct {
		name           string
		plugin         *Plugin
		task           *api.TaskInfo
		expectedCPU    float64
		expectedMemory float64
		expectedCard   v1.ResourceName
		expectedQty    float64
		expectedError  bool
		description    string
	}{
		{
			name: "task with card and cardUnlimitedCpuMemory enabled",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: true,
			},
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expectedCPU:    0,
			expectedMemory: 0,
			expectedCard:   "NVIDIA-A100",
			expectedQty:    1000,
			expectedError:  false,
			description:    "Should not include CPU/Memory when card exists and cardUnlimitedCpuMemory is enabled",
		},
		{
			name: "task with card and cardUnlimitedCpuMemory disabled",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: false,
			},
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 2000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			expectedCPU:    2000,
			expectedMemory: 2 * 1024 * 1024 * 1024,
			expectedCard:   "NVIDIA-A100",
			expectedQty:    1000,
			expectedError:  false,
			description:    "Should include CPU/Memory when cardUnlimitedCpuMemory is disabled",
		},
		{
			name: "CPU-only task",
			plugin: &Plugin{
				cardNameToResourceName:   map[v1.ResourceName]v1.ResourceName{},
				isCardUnlimitedCpuMemory: true,
			},
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   1024 * 1024 * 1024,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
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
			expectedCPU:    1000,
			expectedMemory: 1024 * 1024 * 1024,
			expectedError:  false,
			description:    "Should include CPU/Memory for CPU-only task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tt.plugin.GetTaskRequestResources(tt.task)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if res.MilliCPU != tt.expectedCPU {
				t.Errorf("Expected CPU %v, got %v", tt.expectedCPU, res.MilliCPU)
			}

			if res.Memory != tt.expectedMemory {
				t.Errorf("Expected Memory %v, got %v", tt.expectedMemory, res.Memory)
			}

			if tt.expectedCard != "" {
				if qty, ok := res.ScalarResources[tt.expectedCard]; !ok {
					t.Errorf("Expected card resource %v", tt.expectedCard)
				} else if qty != tt.expectedQty {
					t.Errorf("Expected card quantity %v, got %v", tt.expectedQty, qty)
				}
			}
		})
	}
}

func TestGetCardResourceFromNodeNameForMultiCardTask(t *testing.T) {
	tests := []struct {
		name          string
		plugin        *Plugin
		task          *api.TaskInfo
		multiCardName string
		expectedCard  v1.ResourceName
		expectedQty   float64
		expectedError bool
		description   string
	}{
		{
			name: "multi-card task with bound node",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
					"NVIDIA-H100": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			multiCardName: "NVIDIA-A100|NVIDIA-H100",
			expectedError: true, // Will fail without proper node lister setup
			description:   "Should attempt to retrieve card from bound node",
		},
		{
			name: "task with empty node name",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "", // Empty node name
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			multiCardName: "NVIDIA-A100",
			expectedError: true,
			description:   "Should fail when node name is empty",
		},
		{
			name: "task with non-existent node",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-T4": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-T4",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "non-existent-node",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			multiCardName: "NVIDIA-T4",
			expectedError: true,
			description:   "Should fail when node does not exist in lister",
		},
		{
			name: "multi-card task with empty multi-card name",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
				},
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
					Spec: v1.PodSpec{
						NodeName: "test-node",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("1"),
									},
								},
							},
						},
					},
				},
			},
			multiCardName: "", // Empty multi-card name
			expectedError: true,
			description:   "Should fail when multi-card name is empty",
		},
		{
			name: "multi-card task successfully retrieves card from bound node",
			plugin: &Plugin{
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-A100": "nvidia.com/gpu",
					"NVIDIA-H100": "nvidia.com/gpu",
				},
				nodeCardInfos: make(map[string]NodeCardResourceInfo),
			},
			task: &api.TaskInfo{
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							TaskAnnotationKeyCardName: "NVIDIA-A100|NVIDIA-H100",
						},
					},
					Spec: v1.PodSpec{
						NodeName: "success-node",
						Containers: []v1.Container{
							{
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										"nvidia.com/gpu": resource.MustParse("2"),
									},
								},
							},
						},
					},
				},
			},
			multiCardName: "NVIDIA-A100|NVIDIA-H100",
			expectedCard:  "NVIDIA-A100",
			expectedQty:   2000, // 2 * 1000
			expectedError: false,
			description:   "Should successfully retrieve card resource from bound node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake Kubernetes clientset and node lister
			_ = fake.NewSimpleClientset()
			informer := cache.NewSharedIndexInformer(
				nil,
				&v1.Node{},
				0,
				cache.Indexers{},
			)
			tt.plugin.nodeLister = v1lister.NewNodeLister(informer.GetIndexer())

			// For success case, add node to indexer
			if !tt.expectedError {
				node := &v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: tt.task.Pod.Spec.NodeName,
						Labels: map[string]string{
							"nvidia.com/gpu.product": "NVIDIA-A100",
							"nvidia.com/gpu.count":   "8",
							"nvidia.com/gpu.memory":  "81920",
						},
					},
					Status: v1.NodeStatus{
						Allocatable: v1.ResourceList{
							"nvidia.com/gpu": resource.MustParse("8"),
						},
					},
				}
				_ = informer.GetIndexer().Add(node)
			}

			res, err := tt.plugin.getCardResourceFromNodeNameForMultiCardTask(tt.task, tt.multiCardName)

			if tt.expectedError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify successful result
			if tt.expectedCard != "" {
				if qty, ok := res.ScalarResources[tt.expectedCard]; !ok {
					t.Errorf("Expected card resource %v not found", tt.expectedCard)
				} else if qty != tt.expectedQty {
					t.Errorf("Expected card quantity %v, got %v", tt.expectedQty, qty)
				}
			}
		})
	}
}
