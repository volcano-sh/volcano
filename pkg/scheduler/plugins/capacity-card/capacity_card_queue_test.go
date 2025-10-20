package capacitycard

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// createTestSessionWithQueuesAndJobs creates a test session with queues and jobs
func createTestSessionWithQueuesAndJobs() *framework.Session {
	// Create a mock cache
	mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

	// Create session
	session := framework.OpenSession(mockCache, nil, nil)

	// Create queues
	queue1 := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-queue-1",
		},
		Spec: scheduling.QueueSpec{
			Deserved: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("10"),
			},
			Capability: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("20"),
			},
			Guarantee: scheduling.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("5"),
				},
			},
		},
	}

	queue2 := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-queue-2",
		},
		Spec: scheduling.QueueSpec{
			Deserved: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("15"),
			},
		},
	}

	// Create a simple job
	job1 := &api.JobInfo{
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
				Status: scheduling.PodGroupStatus{
					Phase: scheduling.PodGroupRunning,
				},
			},
			Version: api.PodGroupVersionV1Beta1,
		},
		Tasks: map[api.TaskID]*api.TaskInfo{
			"task-1": {
				UID:       "task-1",
				Name:      "task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
				},
				TransactionContext: api.TransactionContext{
					Status: api.Running,
				},
			},
		},
	}

	// Manually set the queues and jobs
	session.Queues = map[api.QueueID]*api.QueueInfo{
		"queue-1": {
			UID:   "queue-1",
			Name:  "test-queue-1",
			Queue: queue1,
		},
		"queue-2": {
			UID:   "queue-2",
			Name:  "test-queue-2",
			Queue: queue2,
		},
	}

	session.Jobs = map[api.JobID]*api.JobInfo{
		"job-1": job1,
	}

	return session
}

// createTestSessionWithNoQueues creates a test session with no queues
func createTestSessionWithNoQueues() *framework.Session {
	// Create a mock cache
	mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

	// Create session
	session := framework.OpenSession(mockCache, nil, nil)

	// Ensure queues map is empty
	session.Queues = map[api.QueueID]*api.QueueInfo{}

	return session
}

// createTestSessionWithElasticAndInqueueResources creates a test session with jobs that have elastic and inqueue resources
func createTestSessionWithElasticAndInqueueResources() *framework.Session {
	// Create a mock cache
	mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

	// Create session using OpenSession to properly initialize internal fields
	session := framework.OpenSession(mockCache, nil, nil)

	// Create queue with card annotations
	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "elastic-inqueue-test-queue",
			Annotations: map[string]string{
				"volcano.sh/card.quota": `{"nvidia.com/gpu": 8}`,
			},
		},
		Spec: scheduling.QueueSpec{
			Deserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("50"),
				v1.ResourceMemory: resource.MustParse("100Gi"),
			},
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100"),
				v1.ResourceMemory: resource.MustParse("200Gi"),
			},
			Guarantee: scheduling.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("20"),
					v1.ResourceMemory: resource.MustParse("40Gi"),
				},
			},
		},
	}

	// Job 1: Running job with elastic resources (allocated > minResources for CPU/Memory)
	// MinMember = 2, MinResources = 4 CPU + 8Gi (CPU/Memory from MinResources spec)
	// preCheckCardResource = 4 GPU (from card.request annotation, will be replaced by task GPU sum = 3*1 = 3 GPU)
	// Allocated = 3 running tasks with 3 CPU + 6Gi + 1 GPU each = 9 CPU + 18Gi + 3 GPU
	// Final preCheckCardResource = 3 GPU (from task GPU sum, replaces card.request)
	// minResources = 4 CPU + 8Gi + 3 GPU
	// Elastic = Allocated - MinResources = 5 CPU + 10Gi + 0 GPU (GPU is 0 because all tasks are running)
	task1_1 := &api.TaskInfo{
		UID:       "task-1-1",
		Name:      "elastic-task-1-1",
		Namespace: "default",
		Job:       "elastic-job-1",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   6 * 1024 * 1024 * 1024, // 6Gi
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "elastic-task-1-1",
				Namespace: "default",
				UID:       "task-1-1",
				Annotations: map[string]string{
					"volcano.sh/card.name": "NVIDIA-H200",
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
		TransactionContext: api.TransactionContext{
			Status: api.Running,
		},
	}

	task1_2 := &api.TaskInfo{
		UID:       "task-1-2",
		Name:      "elastic-task-1-2",
		Namespace: "default",
		Job:       "elastic-job-1",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   6 * 1024 * 1024 * 1024, // 6Gi
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "elastic-task-1-2",
				Namespace: "default",
				UID:       "task-1-2",
				Annotations: map[string]string{
					"volcano.sh/card.name": "NVIDIA-H200",
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
		TransactionContext: api.TransactionContext{
			Status: api.Running,
		},
	}

	task1_3 := &api.TaskInfo{
		UID:       "task-1-3",
		Name:      "elastic-task-1-3",
		Namespace: "default",
		Job:       "elastic-job-1",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   6 * 1024 * 1024 * 1024, // 6Gi
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "elastic-task-1-3",
				Namespace: "default",
				UID:       "task-1-3",
				Annotations: map[string]string{
					"volcano.sh/card.name": "NVIDIA-H200",
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
		TransactionContext: api.TransactionContext{
			Status: api.Running,
		},
	}

	job1 := api.NewJobInfo("elastic-job-1", task1_1, task1_2, task1_3)
	job1.Name = "elastic-job-1"
	job1.Namespace = "default"
	job1.Queue = "elastic-inqueue-test-queue"
	job1.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "elastic-job-1",
				Namespace: "default",
				Annotations: map[string]string{
					"volcano.sh/card.request": `{"NVIDIA-H200": 4}`,
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 2,
				Queue:     "elastic-inqueue-test-queue",
				MinResources: &v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("4"),
					v1.ResourceMemory: resource.MustParse("8Gi"),
				},
			},
			Status: scheduling.PodGroupStatus{
				Phase:   scheduling.PodGroupRunning,
				Running: 3,
			},
		},
		Version: api.PodGroupVersionV1Beta1,
	}

	// Job 2: Inqueue job with MinResources set
	// MinMember = 2, MinResources = 6 CPU + 12Gi + 2 GPU (from preCheckCardResource)
	// Inqueue = MinResources = 6 CPU + 12Gi + 2 GPU
	task2_1 := &api.TaskInfo{
		UID:       "task-2-1",
		Name:      "inqueue-task-2-1",
		Namespace: "default",
		Job:       "inqueue-job-2",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   6 * 1024 * 1024 * 1024, // 6Gi
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inqueue-task-2-1",
				Namespace: "default",
				UID:       "task-2-1",
				Annotations: map[string]string{
					"volcano.sh/card.name": "NVIDIA-H200",
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
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}

	task2_2 := &api.TaskInfo{
		UID:       "task-2-2",
		Name:      "inqueue-task-2-2",
		Namespace: "default",
		Job:       "inqueue-job-2",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   6 * 1024 * 1024 * 1024, // 6Gi
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inqueue-task-2-2",
				Namespace: "default",
				UID:       "task-2-2",
				Annotations: map[string]string{
					"volcano.sh/card.name": "NVIDIA-H200",
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
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}

	job2 := api.NewJobInfo("inqueue-job-2", task2_1, task2_2)
	job2.Name = "inqueue-job-2"
	job2.Namespace = "default"
	job2.Queue = "elastic-inqueue-test-queue"
	job2.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "inqueue-job-2",
				Namespace: "default",
				Annotations: map[string]string{
					"volcano.sh/card.request": `{"NVIDIA-H200": 2}`,
				},
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 2,
				Queue:     "elastic-inqueue-test-queue",
				MinResources: &v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("6"),
					v1.ResourceMemory: resource.MustParse("12Gi"),
				},
			},
			Status: scheduling.PodGroupStatus{
				Phase: scheduling.PodGroupInqueue,
			},
		},
		Version: api.PodGroupVersionV1Beta1,
	}

	// Set queues and jobs in session
	session.Queues = map[api.QueueID]*api.QueueInfo{
		"elastic-inqueue-test-queue": {
			UID:   "elastic-inqueue-test-queue",
			Name:  "elastic-inqueue-test-queue",
			Queue: queue,
		},
	}

	session.Jobs = map[api.JobID]*api.JobInfo{
		"elastic-job-1": job1,
		"inqueue-job-2": job2,
	}

	return session
}

// createTestSessionWithMultipleJobsAndScalarResources creates a test session with multiple jobs that have scalar resources
func createTestSessionWithMultipleJobsAndScalarResources() *framework.Session {
	// Create a mock cache
	mockCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

	// Create session using OpenSession to properly initialize internal fields
	session := framework.OpenSession(mockCache, nil, nil)

	// Create queue and jobs
	queue4 := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "multi-job-queue",
			Annotations: map[string]string{
				"volcano.sh/card.quota": `{"nvidia.com/gpu": 4, "amd.com/gpu": 2}`,
			},
		},
		Spec: scheduling.QueueSpec{
			Deserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("20"),
				v1.ResourceMemory: resource.MustParse("40Gi"),
			},
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("40"),
				v1.ResourceMemory: resource.MustParse("80Gi"),
			},
			Guarantee: scheduling.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("10"),
					v1.ResourceMemory: resource.MustParse("20Gi"),
				},
			},
		},
	}

	task1 := &api.TaskInfo{
		UID:       "task-1",
		Name:      "gpu-task-1",
		Namespace: "default",
		Job:       "job-1",
		Resreq: &api.Resource{
			MilliCPU: 3000,
			Memory:   1073741824, // 1Gi
			ScalarResources: map[v1.ResourceName]float64{
				"nvidia.com/gpu": 1000, // 1 GPU
				"amd.com/gpu":    500,  // 0.5 GPU
			},
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpu-task-1",
				Namespace: "default",
				UID:       "task-1",
			},
		},
		TransactionContext: api.TransactionContext{
			Status: api.Running,
		},
	}

	// Use api.NewJobInfo to properly initialize TaskStatusIndex and other fields
	job1 := api.NewJobInfo("job-1", task1)
	job1.Name = "gpu-job-1"
	job1.Namespace = "default"
	job1.Queue = "queue-4"
	job1.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpu-job-1",
				Namespace: "default",
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 1,
				Queue:     "queue-4",
			},
			Status: scheduling.PodGroupStatus{
				Phase: scheduling.PodGroupRunning,
			},
		},
		Version: api.PodGroupVersionV1Beta1,
	}

	task2 := &api.TaskInfo{
		UID:       "task-2",
		Name:      "gpu-task-2",
		Namespace: "default",
		Job:       "job-2",
		Resreq: &api.Resource{
			MilliCPU: 6000,
			Memory:   1073741824, // 1Gi
			ScalarResources: map[v1.ResourceName]float64{
				"nvidia.com/gpu": 2000, // 2 GPUs
				"amd.com/gpu":    1000, // 1 GPU
			},
		},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpu-task-2",
				Namespace: "default",
				UID:       "task-2",
			},
		},
		TransactionContext: api.TransactionContext{
			Status: api.Running,
		},
	}

	// Use api.NewJobInfo to properly initialize TaskStatusIndex and other fields
	job2 := api.NewJobInfo("job-2", task2)
	job2.Name = "gpu-job-2"
	job2.Namespace = "default"
	job2.Queue = "queue-4"
	job2.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "gpu-job-2",
				Namespace: "default",
			},
			Spec: scheduling.PodGroupSpec{
				MinMember: 1,
				Queue:     "queue-4",
			},
			Status: scheduling.PodGroupStatus{
				Phase: scheduling.PodGroupRunning,
			},
		},
		Version: api.PodGroupVersionV1Beta1,
	}

	// Set queues and jobs in session
	session.Queues = map[api.QueueID]*api.QueueInfo{
		"queue-4": {
			UID:   "queue-4",
			Name:  "multi-job-queue",
			Queue: queue4,
		},
	}

	session.Jobs = map[api.JobID]*api.JobInfo{
		"job-1": job1,
		"job-2": job2,
	}

	return session
}

// TestNewQueueAttr tests the newQueueAttr function
func TestNewQueueAttr(t *testing.T) {
	tests := []struct {
		name                   string
		queue                  *api.QueueInfo
		expectedName           string
		expectedUID            api.QueueID
		expectedFields         []string
		expectedDeserved       *api.Resource
		expectedCapability     *api.Resource
		expectedGuarantee      *api.Resource
		expectedAllocated      *api.Resource
		expectedRequest        *api.Resource
		expectedElastic        *api.Resource
		expectedInqueue        *api.Resource
		expectedRealCapability *api.Resource
	}{
		{
			name: "queue without card resources in annotations",
			queue: &api.QueueInfo{
				UID:  "queue-1",
				Name: "test-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue",
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("10"),
							v1.ResourceMemory: resource.MustParse("20Gi"),
						},
						Capability: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("20"),
							v1.ResourceMemory: resource.MustParse("40Gi"),
						},
						Guarantee: scheduling.Guarantee{
							Resource: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("5"),
								v1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			expectedName:   "test-queue",
			expectedUID:    "queue-1",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("10"),
				v1.ResourceMemory: resource.MustParse("20Gi"),
			}),
			expectedCapability: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("20"),
				v1.ResourceMemory: resource.MustParse("40Gi"),
			}),
			expectedGuarantee: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("5"),
				v1.ResourceMemory: resource.MustParse("10Gi"),
			}),
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 105 * 1000,
				Memory:   210 * 1024 * 1024 * 1024,
			},
		},
		{
			name: "queue with card resources in annotations",
			queue: &api.QueueInfo{
				UID:  "queue-3",
				Name: "scalar-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scalar-queue",
						Annotations: map[string]string{
							"volcano.sh/card.quota": `{"Tesla-V100": 2, "AMD-RX-7900-XT": 1, "NVIDIA-H200": 3}`,
						},
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("10"),
							v1.ResourceMemory: resource.MustParse("20Gi"),
						},
						Capability: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("20"),
							v1.ResourceMemory: resource.MustParse("40Gi"),
						},
						Guarantee: scheduling.Guarantee{
							Resource: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("5"),
								v1.ResourceMemory: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
			expectedName:   "scalar-queue",
			expectedUID:    "queue-3",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: &api.Resource{
				MilliCPU: 10000,
				Memory:   21474836480,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedCapability: &api.Resource{
				MilliCPU: 20000,
				Memory:   42949672960,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedGuarantee: &api.Resource{
				MilliCPU: 5000,
				Memory:   10737418240,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 105 * 1000,
				Memory:   210 * 1024 * 1024 * 1024,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
		},
		{
			name: "queue without card resources in annotations and empty resource",
			queue: &api.QueueInfo{
				UID:  "queue-1",
				Name: "test-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue",
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0"),
							v1.ResourceMemory: resource.MustParse("0"),
						},
						Capability: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0"),
							v1.ResourceMemory: resource.MustParse("0"),
						},
						Guarantee: scheduling.Guarantee{
							Resource: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("0"),
								v1.ResourceMemory: resource.MustParse("0"),
							},
						},
					},
				},
			},
			expectedName:   "test-queue",
			expectedUID:    "queue-1",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedCapability: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedGuarantee: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 100 * 1000,
				Memory:   200 * 1024 * 1024 * 1024,
			},
		},
		{
			name: "queue with card resources in annotations and empty resource",
			queue: &api.QueueInfo{
				UID:  "queue-3",
				Name: "scalar-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scalar-queue",
						Annotations: map[string]string{
							"volcano.sh/card.quota": `{"Tesla-V100": 2, "AMD-RX-7900-XT": 1, "NVIDIA-H200": 3}`,
						},
					},
					Spec: scheduling.QueueSpec{
						Deserved: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0"),
							v1.ResourceMemory: resource.MustParse("0"),
						},
						Capability: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("0"),
							v1.ResourceMemory: resource.MustParse("0"),
						},
						Guarantee: scheduling.Guarantee{
							Resource: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("0"),
								v1.ResourceMemory: resource.MustParse("0"),
							},
						},
					},
				},
			},
			expectedName:   "scalar-queue",
			expectedUID:    "queue-3",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedCapability: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedGuarantee: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 100 * 1000,
				Memory:   200 * 1024 * 1024 * 1024,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
		},
		{
			name: "queue without card resources in annotations and nil resource",
			queue: &api.QueueInfo{
				UID:  "queue-1",
				Name: "test-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-queue",
					},
					Spec: scheduling.QueueSpec{},
				},
			},
			expectedName:   "test-queue",
			expectedUID:    "queue-1",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedCapability: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedGuarantee: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("0"),
				v1.ResourceMemory: resource.MustParse("0"),
			}),
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 100 * 1000,
				Memory:   200 * 1024 * 1024 * 1024,
			},
		},
		{
			name: "queue with card resources in annotations and nil resource",
			queue: &api.QueueInfo{
				UID:  "queue-3",
				Name: "scalar-queue",
				Queue: &scheduling.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scalar-queue",
						Annotations: map[string]string{
							"volcano.sh/card.quota": `{"Tesla-V100": 2, "AMD-RX-7900-XT": 1, "NVIDIA-H200": 3}`,
						},
					},
					Spec: scheduling.QueueSpec{},
				},
			},
			expectedName:   "scalar-queue",
			expectedUID:    "queue-3",
			expectedFields: []string{"deserved", "capability", "guarantee", "allocated", "request", "elastic", "inqueue"},
			expectedDeserved: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedCapability: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedGuarantee: &api.Resource{
				MilliCPU: 0,
				Memory:   0,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
			expectedAllocated: api.EmptyResource(),
			expectedRequest:   api.EmptyResource(),
			expectedElastic:   api.EmptyResource(),
			expectedInqueue:   api.EmptyResource(),
			expectedRealCapability: &api.Resource{
				MilliCPU: 100 * 1000,
				Memory:   200 * 1024 * 1024 * 1024,
				ScalarResources: map[v1.ResourceName]float64{
					"Tesla-V100":     2000,
					"AMD-RX-7900-XT": 1000,
					"NVIDIA-H200":    3000,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				totalResource: api.NewResource(v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse("100"),
					v1.ResourceMemory: resource.MustParse("200Gi"),
				}),
				totalGuarantee: api.EmptyResource(),
			}

			qAttr := plugin.newQueueAttr(tt.queue)

			if qAttr.name != tt.expectedName {
				t.Errorf("expected name %s, got %s", tt.expectedName, qAttr.name)
			}

			if qAttr.queueID != tt.expectedUID {
				t.Errorf("expected UID %s, got %s", tt.expectedUID, qAttr.queueID)
			}

			// Check that all expected fields are initialized and have expected values
			for _, field := range tt.expectedFields {
				switch field {
				case "deserved":
					if qAttr.deserved == nil {
						t.Error("deserved should not be nil")
					} else if tt.expectedDeserved != nil {
						// Check deserved resource values using expected values from test case
						if qAttr.deserved.MilliCPU != tt.expectedDeserved.MilliCPU {
							t.Errorf("expected deserved CPU %f, got %f", tt.expectedDeserved.MilliCPU, qAttr.deserved.MilliCPU)
						}
						if qAttr.deserved.Memory != tt.expectedDeserved.Memory {
							t.Errorf("expected deserved Memory %f, got %f", tt.expectedDeserved.Memory, qAttr.deserved.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedDeserved.ScalarResources {
							actualValue, exists := qAttr.deserved.ScalarResources[resName]
							if !exists {
								t.Errorf("expected deserved scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected deserved scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "capability":
					if qAttr.capability == nil {
						t.Error("capability should not be nil")
					} else if tt.expectedCapability != nil {
						// Check capability resource values using expected values from test case
						if qAttr.capability.MilliCPU != tt.expectedCapability.MilliCPU {
							t.Errorf("expected capability CPU %f, got %f", tt.expectedCapability.MilliCPU, qAttr.capability.MilliCPU)
						}
						if qAttr.capability.Memory != tt.expectedCapability.Memory {
							t.Errorf("expected capability Memory %f, got %f", tt.expectedCapability.Memory, qAttr.capability.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedCapability.ScalarResources {
							actualValue, exists := qAttr.capability.ScalarResources[resName]
							if !exists {
								t.Errorf("expected capability scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected capability scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "guarantee":
					if qAttr.guarantee == nil {
						t.Error("guarantee should not be nil")
					} else if tt.expectedGuarantee != nil {
						// Check guarantee resource values using expected values from test case
						if qAttr.guarantee.MilliCPU != tt.expectedGuarantee.MilliCPU {
							t.Errorf("expected guarantee CPU %f, got %f", tt.expectedGuarantee.MilliCPU, qAttr.guarantee.MilliCPU)
						}
						if qAttr.guarantee.Memory != tt.expectedGuarantee.Memory {
							t.Errorf("expected guarantee Memory %f, got %f", tt.expectedGuarantee.Memory, qAttr.guarantee.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedGuarantee.ScalarResources {
							actualValue, exists := qAttr.guarantee.ScalarResources[resName]
							if !exists {
								t.Errorf("expected guarantee scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected guarantee scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "allocated":
					if qAttr.allocated == nil {
						t.Error("allocated should not be nil")
					} else if tt.expectedAllocated != nil {
						// Check allocated resource values using expected values from test case
						if qAttr.allocated.MilliCPU != tt.expectedAllocated.MilliCPU {
							t.Errorf("expected allocated CPU %f, got %f", tt.expectedAllocated.MilliCPU, qAttr.allocated.MilliCPU)
						}
						if qAttr.allocated.Memory != tt.expectedAllocated.Memory {
							t.Errorf("expected allocated Memory %f, got %f", tt.expectedAllocated.Memory, qAttr.allocated.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedAllocated.ScalarResources {
							actualValue, exists := qAttr.allocated.ScalarResources[resName]
							if !exists {
								t.Errorf("expected allocated scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected allocated scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "request":
					if qAttr.request == nil {
						t.Error("request should not be nil")
					} else if tt.expectedRequest != nil {
						// Check request resource values using expected values from test case
						if qAttr.request.MilliCPU != tt.expectedRequest.MilliCPU {
							t.Errorf("expected request CPU %f, got %f", tt.expectedRequest.MilliCPU, qAttr.request.MilliCPU)
						}
						if qAttr.request.Memory != tt.expectedRequest.Memory {
							t.Errorf("expected request Memory %f, got %f", tt.expectedRequest.Memory, qAttr.request.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedRequest.ScalarResources {
							actualValue, exists := qAttr.request.ScalarResources[resName]
							if !exists {
								t.Errorf("expected request scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected request scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "elastic":
					if qAttr.elastic == nil {
						t.Error("elastic should not be nil")
					} else if tt.expectedElastic != nil {
						// Check elastic resource values using expected values from test case
						if qAttr.elastic.MilliCPU != tt.expectedElastic.MilliCPU {
							t.Errorf("expected elastic CPU %f, got %f", tt.expectedElastic.MilliCPU, qAttr.elastic.MilliCPU)
						}
						if qAttr.elastic.Memory != tt.expectedElastic.Memory {
							t.Errorf("expected elastic Memory %f, got %f", tt.expectedElastic.Memory, qAttr.elastic.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedElastic.ScalarResources {
							actualValue, exists := qAttr.elastic.ScalarResources[resName]
							if !exists {
								t.Errorf("expected elastic scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected elastic scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				case "inqueue":
					if qAttr.inqueue == nil {
						t.Error("inqueue should not be nil")
					} else if tt.expectedInqueue != nil {
						// Check inqueue resource values using expected values from test case
						if qAttr.inqueue.MilliCPU != tt.expectedInqueue.MilliCPU {
							t.Errorf("expected inqueue CPU %f, got %f", tt.expectedInqueue.MilliCPU, qAttr.inqueue.MilliCPU)
						}
						if qAttr.inqueue.Memory != tt.expectedInqueue.Memory {
							t.Errorf("expected inqueue Memory %f, got %f", tt.expectedInqueue.Memory, qAttr.inqueue.Memory)
						}
						// Check scalar resources if present
						for resName, expectedValue := range tt.expectedInqueue.ScalarResources {
							actualValue, exists := qAttr.inqueue.ScalarResources[resName]
							if !exists {
								t.Errorf("expected inqueue scalar resource %s not found", resName)
							} else if actualValue != expectedValue {
								t.Errorf("expected inqueue scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
							}
						}
					}
				}
			}

			// Check realCapability calculation
			if qAttr.realCapability == nil {
				t.Error("realCapability should not be nil")
			} else if tt.expectedRealCapability != nil {
				// Check realCapability resource values using expected values from test case
				if qAttr.realCapability.MilliCPU != tt.expectedRealCapability.MilliCPU {
					t.Errorf("expected realCapability CPU %f, got %f", tt.expectedRealCapability.MilliCPU, qAttr.realCapability.MilliCPU)
				}
				if qAttr.realCapability.Memory != tt.expectedRealCapability.Memory {
					t.Errorf("expected realCapability Memory %f, got %f", tt.expectedRealCapability.Memory, qAttr.realCapability.Memory)
				}
				// Check scalar resources if present
				for resName, expectedValue := range tt.expectedRealCapability.ScalarResources {
					actualValue, exists := qAttr.realCapability.ScalarResources[resName]
					if !exists {
						t.Errorf("expected realCapability scalar resource %s not found", resName)
					} else if actualValue != expectedValue {
						t.Errorf("expected realCapability scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
					}
				}
			}

		})
	}
}

// TestBuildQueueAttrs tests the buildQueueAttrs function
func TestBuildQueueAttrs(t *testing.T) {
	tests := []struct {
		name                    string
		session                 *framework.Session
		totalResource           *api.Resource
		totalGuarantee          *api.Resource
		expectedQueueCount      int
		expectedError           bool
		queueID                 api.QueueID                 // Optional: specific queue to check
		expectedAllocatedCPU    *float64                    // Optional: expected allocated CPU
		expectedAllocatedMemory *float64                    // Optional: expected allocated Memory
		expectedAllocatedCards  map[v1.ResourceName]float64 // Optional: expected allocated scalar resources
		expectedRequestCPU      *float64                    // Optional: expected request CPU
		expectedRequestMemory   *float64                    // Optional: expected request Memory
		expectedRequestCards    map[v1.ResourceName]float64 // Optional: expected request scalar resources
		expectedElasticCPU      *float64                    // Optional: expected elastic CPU
		expectedElasticMemory   *float64                    // Optional: expected elastic Memory
		expectedElasticCards    map[v1.ResourceName]float64 // Optional: expected elastic scalar resources
		expectedInqueueCPU      *float64                    // Optional: expected inqueue CPU
		expectedInqueueMemory   *float64                    // Optional: expected inqueue Memory
		expectedInqueueCards    map[v1.ResourceName]float64 // Optional: expected inqueue scalar resources
	}{
		{
			name:    "session with multiple queues and jobs",
			session: createTestSessionWithQueuesAndJobs(),
			totalResource: api.NewResource(v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("100"),
			}),
			totalGuarantee:     api.EmptyResource(),
			expectedQueueCount: 1,
			expectedError:      false,
		},
		{
			name:               "session with no queues",
			session:            createTestSessionWithNoQueues(),
			totalResource:      api.EmptyResource(),
			totalGuarantee:     api.EmptyResource(),
			expectedQueueCount: 0,
			expectedError:      false,
		},
		{
			name:    "session with multiple jobs and scalar resources",
			session: createTestSessionWithMultipleJobsAndScalarResources(),
			totalResource: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("100"),
				v1.ResourceMemory: resource.MustParse("200Gi"),
			}),
			totalGuarantee:          api.EmptyResource(),
			expectedQueueCount:      1,
			expectedError:           false,
			queueID:                 "queue-4",
			expectedAllocatedCPU:    floatPtr(9000.0),       // 3 + 6 = 9 CPUs
			expectedAllocatedMemory: floatPtr(2147483648.0), // 1Gi + 1Gi = 2Gi
			expectedRequestCPU:      floatPtr(9000.0),       // Same as allocated since all tasks are running
			expectedRequestMemory:   floatPtr(2147483648.0), // Same as allocated since all tasks are running
		},
		{
			name:    "session with elastic and inqueue resources",
			session: createTestSessionWithElasticAndInqueueResources(),
			totalResource: api.NewResource(v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("200"),
				v1.ResourceMemory: resource.MustParse("400Gi"),
			}),
			totalGuarantee:     api.EmptyResource(),
			expectedQueueCount: 1,
			expectedError:      false,
			queueID:            "elastic-inqueue-test-queue",
			// Job 1: 3 running tasks * (3 CPU + 6Gi + 1 GPU) = 9 CPU + 18Gi + 3 GPU allocated
			// Job 2: 0 allocated (all pending)
			expectedAllocatedCPU:    floatPtr(9000.0),        // 9 CPUs
			expectedAllocatedMemory: floatPtr(19327352832.0), // 18Gi
			// Job 1: 3 running tasks request = 9 CPU + 18Gi + 3 GPU
			// Job 2: 2 pending tasks request = 6 CPU + 12Gi + 2 GPU
			expectedRequestCPU:    floatPtr(15000.0),       // 9 + 6 = 15 CPUs
			expectedRequestMemory: floatPtr(32212254720.0), // 18 + 12 = 30 Gi
			// Job 1 elastic: allocated (9 CPU + 18Gi + 3 GPU) - minResources (4 CPU + 8Gi + 3 GPU) = 5 CPU + 10Gi + 0 GPU
			// Note: elastic GPU is 0 because all tasks are running, so allocated GPU = preCheckCardResource GPU
			expectedElasticCPU:    floatPtr(5000.0),              // 5 CPUs
			expectedElasticMemory: floatPtr(10737418240.0),       // 10Gi
			expectedElasticCards:  map[v1.ResourceName]float64{}, // 0 GPU (3 allocated - 3 from tasks = 0)
			// Job 2 inqueue: minResources (6 CPU + 12Gi + 2 GPU) - allocated (0) = 6 CPU + 12Gi + 2 GPU
			expectedInqueueCPU:    floatPtr(6000.0),        // 6 CPUs
			expectedInqueueMemory: floatPtr(12884901888.0), // 12Gi
			expectedInqueueCards: map[v1.ResourceName]float64{
				"NVIDIA-H200": 2000, // 2 GPUs
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				totalResource:  tt.totalResource,
				totalGuarantee: tt.totalGuarantee,
				queueOpts:      make(map[api.QueueID]*queueAttr),
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"NVIDIA-H200": "nvidia.com/gpu",
				},
			}

			result := plugin.buildQueueAttrs(tt.session)

			if tt.expectedError {
				if result {
					t.Error("expected error but got success")
				}
			} else {
				if !result {
					t.Error("expected success but got error")
				}

				if len(plugin.queueOpts) != tt.expectedQueueCount {
					t.Errorf("expected %d queue options, got %d", tt.expectedQueueCount, len(plugin.queueOpts))
				}

				// Check that queue order function was added
				if tt.expectedQueueCount > 0 {
					// This is a bit tricky to test directly, but we can verify the function was set
					// by checking that the plugin state was properly updated
					for _, qAttr := range plugin.queueOpts {
						if qAttr.deserved == nil {
							t.Error("queue deserved should not be nil")
						}
					}
				}

				// Optional: Check specific queue attributes
				if tt.queueID != "" {
					qAttr, exists := plugin.queueOpts[tt.queueID]
					if !exists {
						t.Errorf("expected queue %s to exist in queueOpts", tt.queueID)
						return
					}

					// Check allocated resources
					if tt.expectedAllocatedCPU != nil || tt.expectedAllocatedMemory != nil || len(tt.expectedAllocatedCards) > 0 {
						if qAttr.allocated == nil {
							t.Error("allocated should not be nil")
						} else {
							if tt.expectedAllocatedCPU != nil && qAttr.allocated.MilliCPU != *tt.expectedAllocatedCPU {
								t.Errorf("expected allocated CPU %f, got %f", *tt.expectedAllocatedCPU, qAttr.allocated.MilliCPU)
							}
							if tt.expectedAllocatedMemory != nil && qAttr.allocated.Memory != *tt.expectedAllocatedMemory {
								t.Errorf("expected allocated Memory %f, got %f", *tt.expectedAllocatedMemory, qAttr.allocated.Memory)
							}
							// Check scalar resources
							for resName, expectedValue := range tt.expectedAllocatedCards {
								actualValue, exists := qAttr.allocated.ScalarResources[resName]
								if !exists {
									t.Errorf("expected allocated scalar resource %s not found", resName)
								} else if actualValue != expectedValue {
									t.Errorf("expected allocated scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
								}
							}
						}
					}

					// Check request resources
					if tt.expectedRequestCPU != nil || tt.expectedRequestMemory != nil || len(tt.expectedRequestCards) > 0 {
						if qAttr.request == nil {
							t.Error("request should not be nil")
						} else {
							if tt.expectedRequestCPU != nil && qAttr.request.MilliCPU != *tt.expectedRequestCPU {
								t.Errorf("expected request CPU %f, got %f", *tt.expectedRequestCPU, qAttr.request.MilliCPU)
							}
							if tt.expectedRequestMemory != nil && qAttr.request.Memory != *tt.expectedRequestMemory {
								t.Errorf("expected request Memory %f, got %f", *tt.expectedRequestMemory, qAttr.request.Memory)
							}
							// Check scalar resources
							for resName, expectedValue := range tt.expectedRequestCards {
								actualValue, exists := qAttr.request.ScalarResources[resName]
								if !exists {
									t.Errorf("expected request scalar resource %s not found", resName)
								} else if actualValue != expectedValue {
									t.Errorf("expected request scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
								}
							}
						}
					}

					// Check elastic resources
					if tt.expectedElasticCPU != nil || tt.expectedElasticMemory != nil || len(tt.expectedElasticCards) > 0 {
						if qAttr.elastic == nil {
							t.Error("elastic should not be nil")
						} else {
							if tt.expectedElasticCPU != nil && qAttr.elastic.MilliCPU != *tt.expectedElasticCPU {
								t.Errorf("expected elastic CPU %f, got %f", *tt.expectedElasticCPU, qAttr.elastic.MilliCPU)
							}
							if tt.expectedElasticMemory != nil && qAttr.elastic.Memory != *tt.expectedElasticMemory {
								t.Errorf("expected elastic Memory %f, got %f", *tt.expectedElasticMemory, qAttr.elastic.Memory)
							}
							// Check scalar resources if present
							for resName, expectedValue := range tt.expectedElasticCards {
								actualValue, exists := qAttr.elastic.ScalarResources[resName]
								if !exists {
									t.Errorf("expected elastic scalar resource %s not found", resName)
								} else if actualValue != expectedValue {
									t.Errorf("expected elastic scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
								}
							}
						}
					}

					// Check inqueue resources
					if tt.expectedInqueueCPU != nil || tt.expectedInqueueMemory != nil || len(tt.expectedInqueueCards) > 0 {
						if qAttr.inqueue == nil {
							t.Error("inqueue should not be nil")
						} else {
							if tt.expectedInqueueCPU != nil && qAttr.inqueue.MilliCPU != *tt.expectedInqueueCPU {
								t.Errorf("expected inqueue CPU %f, got %f", *tt.expectedInqueueCPU, qAttr.inqueue.MilliCPU)
							}
							if tt.expectedInqueueMemory != nil && qAttr.inqueue.Memory != *tt.expectedInqueueMemory {
								t.Errorf("expected inqueue Memory %f, got %f", *tt.expectedInqueueMemory, qAttr.inqueue.Memory)
							}
							// Check scalar resources if present
							for resName, expectedValue := range tt.expectedInqueueCards {
								actualValue, exists := qAttr.inqueue.ScalarResources[resName]
								if !exists {
									t.Errorf("expected inqueue scalar resource %s not found", resName)
								} else if actualValue != expectedValue {
									t.Errorf("expected inqueue scalar resource %s value %f, got %f", resName, expectedValue, actualValue)
								}
							}
						}
					}

					// Log queue attributes for debugging
					if tt.queueID != "" {
						t.Logf("Queue %s allocated: %v", qAttr.name, qAttr.allocated)
						t.Logf("Queue %s request: %v", qAttr.name, qAttr.request)
						t.Logf("Queue %s elastic: %v", qAttr.name, qAttr.elastic)
						t.Logf("Queue %s inqueue: %v", qAttr.name, qAttr.inqueue)
						t.Logf("Queue %s deserved: %v", qAttr.name, qAttr.deserved)
						t.Logf("Queue %s capability: %v", qAttr.name, qAttr.capability)
						t.Logf("Queue %s guarantee: %v", qAttr.name, qAttr.guarantee)
						t.Logf("Queue %s realCapability: %v", qAttr.name, qAttr.realCapability)
					}
				}
			}
		})
	}
}

// floatPtr is a helper function to create a pointer to a float64 value
func floatPtr(f float64) *float64 {
	return &f
}

// TestBuildQueueMetrics tests the buildQueueMetrics function
func TestBuildQueueMetrics(t *testing.T) {
	tests := []struct {
		name      string
		session   *framework.Session
		queueOpts map[api.QueueID]*queueAttr
	}{
		{
			name: "session with queue options",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
						},
					},
				},
			},
			queueOpts: map[api.QueueID]*queueAttr{
				"queue-1": {
					queueID:        "queue-1",
					name:           "test-queue",
					deserved:       &api.Resource{MilliCPU: 10000, Memory: 16 * 1024 * 1024 * 1024},
					allocated:      &api.Resource{MilliCPU: 5000, Memory: 8 * 1024 * 1024 * 1024},
					request:        &api.Resource{MilliCPU: 6000, Memory: 10 * 1024 * 1024 * 1024},
					capability:     &api.Resource{MilliCPU: 20000, Memory: 32 * 1024 * 1024 * 1024},
					realCapability: &api.Resource{MilliCPU: 15000, Memory: 24 * 1024 * 1024 * 1024},
				},
			},
		},
		{
			name: "session with queue but no queue options",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
							Spec: scheduling.QueueSpec{
								Deserved: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("10"),
								},
								Capability: v1.ResourceList{
									v1.ResourceCPU: resource.MustParse("20"),
								},
								Guarantee: scheduling.Guarantee{
									Resource: v1.ResourceList{
										v1.ResourceCPU: resource.MustParse("5"),
									},
								},
							},
						},
					},
				},
			},
			queueOpts: map[api.QueueID]*queueAttr{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				totalResource:  api.NewResource(v1.ResourceList{v1.ResourceCPU: resource.MustParse("100")}),
				totalGuarantee: api.EmptyResource(),
				queueOpts:      tt.queueOpts,
			}

			// This should not panic and should execute without errors
			plugin.buildQueueMetrics(tt.session)

			// Since metrics.UpdateQueue* functions are external, we can't easily verify
			// their calls. But we can verify that the function executes without panicking
			// and that the plugin state remains consistent.
			if len(tt.queueOpts) > 0 {
				// Verify that queue options are still intact
				for queueID, qAttr := range tt.queueOpts {
					if qAttr.name == "" {
						t.Errorf("queue attribute for %s should have a name", queueID)
					}
				}
			}
		})
	}
}

// TestUpdateShare tests the updateShare function
func TestUpdateShare(t *testing.T) {
	tests := []struct {
		name          string
		attr          *queueAttr
		expectedShare float64
	}{
		{
			name: "queue with normal share calculation",
			attr: &queueAttr{
				name: "test-queue",
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   16 * 1024 * 1024 * 1024,
				},
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
			},
			expectedShare: 0.5,
		},
		{
			name: "queue with card share calculation",
			attr: &queueAttr{
				name: "test-queue",
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   16 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 4,
					},
				},
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   8 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 3,
					},
				},
			},
			expectedShare: 0.75,
		},
		{
			name: "queue with zero allocated resources",
			attr: &queueAttr{
				name: "test-queue",
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   16 * 1024 * 1024 * 1024,
				},
				allocated: &api.Resource{
					MilliCPU: 0,
					Memory:   0,
				},
			},
			expectedShare: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{}

			plugin.updateShare(tt.attr)

			if tt.attr.share != tt.expectedShare {
				t.Errorf("expected share %f, got %f", tt.expectedShare, tt.attr.share)
			}
		})
	}
}

// TestNewQueueResourceSupportingCard tests the newQueueResourceSupportingCard function
func TestNewQueueResourceSupportingCard(t *testing.T) {
	tests := []struct {
		name           string
		queue          *scheduling.Queue
		resourceList   v1.ResourceList
		expectedCPU    float64
		expectedMemory float64
		expectedCards  map[v1.ResourceName]float64
	}{
		{
			name: "basic resource list without cards",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue",
				},
			},
			resourceList: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("8Gi"),
			},
			expectedCPU:    4000,                   // 4 * 1000
			expectedMemory: 8 * 1024 * 1024 * 1024, // 8Gi in bytes
			expectedCards:  map[v1.ResourceName]float64{},
		},
		{
			name: "resource list with card annotations",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-queue",
					Annotations: map[string]string{
						"volcano.sh/card.quota": `{"Tesla-V100": 2, "AMD-RX-7900-XT": 1}`,
					},
				},
			},
			resourceList: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("8"),
			},
			expectedCPU:    8000,
			expectedMemory: 0,
			expectedCards: map[v1.ResourceName]float64{
				"Tesla-V100":     2000, // 2 * 1000
				"AMD-RX-7900-XT": 1000, // 1 * 1000
			},
		},
		{
			name: "empty resource list",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "empty-queue",
				},
			},
			resourceList:   v1.ResourceList{},
			expectedCPU:    0,
			expectedMemory: 0,
			expectedCards:  map[v1.ResourceName]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{}

			result := plugin.newQueueResourceSupportingCard(tt.queue, tt.resourceList)

			if result.MilliCPU != tt.expectedCPU {
				t.Errorf("expected CPU %f, got %f", tt.expectedCPU, result.MilliCPU)
			}

			if result.Memory != tt.expectedMemory {
				t.Errorf("expected Memory %f, got %f", tt.expectedMemory, result.Memory)
			}

			// Check card resources
			for cardName, expectedValue := range tt.expectedCards {
				actualValue, exists := result.ScalarResources[cardName]
				if !exists {
					t.Errorf("expected card resource %s not found", cardName)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("expected card %s value %f, got %f", cardName, expectedValue, actualValue)
				}
			}

			// Check that no unexpected card resources exist
			if len(tt.expectedCards) == 0 && len(result.ScalarResources) > 0 {
				t.Errorf("expected no card resources, but found %v", result.ScalarResources)
			}
		})
	}
}

// TestUpdateQueueAttrShare tests the updateQueueAttrShare function
func TestUpdateQueueAttrShare(t *testing.T) {
	tests := []struct {
		name          string
		attr          *queueAttr
		expectedShare float64
	}{
		{
			name: "queue with allocated less than deserved",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 10000,                   // 10 CPUs
					Memory:   16 * 1024 * 1024 * 1024, // 16Gi
				},
				allocated: &api.Resource{
					MilliCPU: 1000,                   // 5 CPUs
					Memory:   8 * 1024 * 1024 * 1024, // 8Gi
				},
			},
			expectedShare: 0.5, // max(5/10, 8/16) = 0.5
		},
		{
			name: "queue with allocated equal to deserved",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   16 * 1024 * 1024 * 1024,
				},
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   16 * 1024 * 1024 * 1024,
				},
			},
			expectedShare: 1.0, // max(10/10, 16/16) = 1.0
		},
		{
			name: "queue with allocated more than deserved",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   16 * 1024 * 1024 * 1024,
				},
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   24 * 1024 * 1024 * 1024,
				},
			},
			expectedShare: 1.5, // max(15/10, 24/16) = 1.5
		},
		{
			name: "queue with zero deserved resources",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 0,
					Memory:   0,
				},
				allocated: &api.Resource{
					MilliCPU: 5000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
			},
			expectedShare: 0, // division by zero should be handled gracefully
		},
		{
			name: "queue with zero allocated resources",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 5000,
					Memory:   8 * 1024 * 1024 * 1024,
				},
				allocated: &api.Resource{
					MilliCPU: 0,
					Memory:   0,
				},
			},
			expectedShare: 0, // division by zero should be handled gracefully
		},
		{
			name: "queue with card resources",
			attr: &queueAttr{
				deserved: &api.Resource{
					MilliCPU: 10000,
					Memory:   10 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 2000, // 2 GPUs
					},
				},
				allocated: &api.Resource{
					MilliCPU: 1000,
					Memory:   1 * 1024 * 1024 * 1024,
					ScalarResources: map[v1.ResourceName]float64{
						"Tesla-V100": 1000, // 1 GPU
					},
				},
			},
			expectedShare: 0.5, // max(5/10, 8/16, 1/2) = 0.5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updateQueueAttrShare(tt.attr)

			if tt.attr.share != tt.expectedShare {
				t.Errorf("expected share %f, got %f", tt.expectedShare, tt.attr.share)
			}
		})
	}
}

// TestGetInqueueResource tests the GetInqueueResource function
func TestGetInqueueResource(t *testing.T) {
	tests := []struct {
		name                     string
		minCPU                   string // MinResources CPU (e.g., "5" for 5 cores)
		minMemory                string // MinResources Memory (e.g., "10Gi")
		minCards                 map[string]int
		allocatedCPU             float64                     // Allocated CPU in milli-cores
		allocatedMemory          float64                     // Allocated Memory in bytes
		allocatedCards           map[v1.ResourceName]float64 // Allocated card resources
		isCardUnlimitedCpuMemory bool
		expectedCPU              float64
		expectedMemory           float64
		expectedCards            map[v1.ResourceName]float64
	}{
		{
			name:                     "job with allocated less than min resources - cardUnlimited=false",
			minCPU:                   "5",
			minMemory:                "10Gi",
			allocatedCPU:             2000,                   // 2 CPUs allocated
			allocatedMemory:          4 * 1024 * 1024 * 1024, // 4Gi allocated
			isCardUnlimitedCpuMemory: false,
			expectedCPU:              3000,                   // 5 - 2 = 3 CPUs needed
			expectedMemory:           6 * 1024 * 1024 * 1024, // 10 - 4 = 6Gi needed
			expectedCards:            map[v1.ResourceName]float64{},
		},
		{
			name:                     "job with allocated equal to min resources - cardUnlimited=false",
			minCPU:                   "5",
			minMemory:                "10Gi",
			allocatedCPU:             5000,                    // 5 CPUs allocated
			allocatedMemory:          10 * 1024 * 1024 * 1024, // 10Gi allocated
			isCardUnlimitedCpuMemory: false,
			expectedCPU:              0,
			expectedMemory:           0,
			expectedCards:            map[v1.ResourceName]float64{},
		},
		{
			name:                     "job with allocated greater than min resources - cardUnlimited=false",
			minCPU:                   "5",
			minMemory:                "10Gi",
			allocatedCPU:             6000,                    // 6 CPUs allocated
			allocatedMemory:          12 * 1024 * 1024 * 1024, // 12Gi allocated
			isCardUnlimitedCpuMemory: false,
			expectedCPU:              0, // no inqueue needed since allocated >= min
			expectedMemory:           0,
			expectedCards:            map[v1.ResourceName]float64{},
		},
		{
			name:      "job with card resources - cardUnlimited=false",
			minCPU:    "5",
			minMemory: "10Gi",
			minCards: map[string]int{
				"Tesla-V100": 2,
			},
			allocatedCPU:    2000,
			allocatedMemory: 4 * 1024 * 1024 * 1024,
			allocatedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 1000, // 1 GPU allocated
			},
			isCardUnlimitedCpuMemory: false,
			expectedCPU:              3000,
			expectedMemory:           6 * 1024 * 1024 * 1024,
			expectedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 1000, // 2 - 1 = 1 GPU needed
			},
		},
		{
			name:      "job with card resources equal to min - cardUnlimited=false",
			minCPU:    "5",
			minMemory: "10Gi",
			minCards: map[string]int{
				"Tesla-V100": 2,
			},
			allocatedCPU:    2000,
			allocatedMemory: 4 * 1024 * 1024 * 1024,
			allocatedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 2000, // 2 GPU allocated
			},
			isCardUnlimitedCpuMemory: false,
			expectedCPU:              3000,
			expectedMemory:           6 * 1024 * 1024 * 1024,
			expectedCards:            map[v1.ResourceName]float64{},
		},
		{
			name: "job with card resources - cardUnlimited=true",
			minCards: map[string]int{
				"Tesla-V100": 2,
			},
			allocatedCPU:    2000,
			allocatedMemory: 4 * 1024 * 1024 * 1024,
			allocatedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 1000, // 1 GPU allocated
			},
			isCardUnlimitedCpuMemory: true,
			expectedCPU:              0, // CPU/Memory should not be counted when cardUnlimited=true
			expectedMemory:           0,
			expectedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 1000, // 2 - 1 = 1 GPU needed
			},
		},
		{
			name: "job with card resources equal to min - cardUnlimited=true",
			minCards: map[string]int{
				"Tesla-V100": 2,
			},
			allocatedCPU:    2000,
			allocatedMemory: 4 * 1024 * 1024 * 1024,
			allocatedCards: map[v1.ResourceName]float64{
				"Tesla-V100": 2000, // 2 GPU allocated
			},
			isCardUnlimitedCpuMemory: true,
			expectedCPU:              0, // CPU/Memory should not be counted when cardUnlimited=true
			expectedMemory:           0,
			expectedCards:            map[v1.ResourceName]float64{},
		},
		{
			name:                     "job without card resources - cardUnlimited=true",
			minCPU:                   "5",
			minMemory:                "10Gi",
			allocatedCPU:             2000,
			allocatedMemory:          4 * 1024 * 1024 * 1024,
			isCardUnlimitedCpuMemory: true,
			expectedCPU:              3000,                   // CPU/Memory should be counted when no card resources
			expectedMemory:           6 * 1024 * 1024 * 1024, // 10 - 4 = 6Gi needed
			expectedCards:            map[v1.ResourceName]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a plugin instance with the test's isCardUnlimitedCpuMemory setting
			plugin := &Plugin{
				isCardUnlimitedCpuMemory: tt.isCardUnlimitedCpuMemory,
			}

			// Build MinResources for PodGroup
			minResourcesList := v1.ResourceList{}
			if tt.minCPU != "" {
				minResourcesList[v1.ResourceCPU] = resource.MustParse(tt.minCPU)
			}
			if tt.minMemory != "" {
				minResourcesList[v1.ResourceMemory] = resource.MustParse(tt.minMemory)
			}

			// Build card request annotation
			cardRequestAnnotation := ""
			if len(tt.minCards) > 0 {
				cardRequestJSON := fmt.Sprintf(`{"%s": %d}`, "Tesla-V100", tt.minCards["Tesla-V100"])
				if len(tt.minCards) == 1 {
					cardRequestAnnotation = cardRequestJSON
				}
			}

			// Create JobInfo with proper structure
			apiJob := &api.JobInfo{
				Name:      "test-job",
				Namespace: "default",
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Name:        "test-job",
							Namespace:   "default",
							Annotations: map[string]string{},
						},
						Spec: scheduling.PodGroupSpec{
							MinResources: &minResourcesList,
						},
					},
				},
			}

			if cardRequestAnnotation != "" {
				apiJob.PodGroup.Annotations["volcano.sh/card.request"] = cardRequestAnnotation
			}

			// Create JobInfo instance
			job := &JobInfo{
				JobInfo: apiJob,
				preCheckCardResource: &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{},
				},
			}

			// Set preCheckCardResource based on minCards
			for cardName, qty := range tt.minCards {
				job.preCheckCardResource.ScalarResources[v1.ResourceName(cardName)] = float64(qty * 1000)
			}

			// Create allocated resource
			allocated := &api.Resource{
				MilliCPU: tt.allocatedCPU,
				Memory:   tt.allocatedMemory,
			}
			if len(tt.allocatedCards) > 0 {
				allocated.ScalarResources = make(map[v1.ResourceName]float64)
				for cardName, qty := range tt.allocatedCards {
					allocated.ScalarResources[cardName] = qty
				}
			}

			// Call GetInqueueResource
			inqueue := plugin.GetInqueueResource(job, allocated)

			// Compare with expected results
			if inqueue.MilliCPU != tt.expectedCPU {
				t.Errorf("expected CPU %f, got %f", tt.expectedCPU, inqueue.MilliCPU)
			}

			if inqueue.Memory != tt.expectedMemory {
				t.Errorf("expected Memory %f, got %f", tt.expectedMemory, inqueue.Memory)
			}

			// Check card resources
			for cardName, expectedValue := range tt.expectedCards {
				actualValue, exists := inqueue.ScalarResources[cardName]
				if expectedValue == 0 {
					// If expected is 0, it should not exist in the map or be 0
					if exists && actualValue != 0 {
						t.Errorf("expected card %s to be absent or 0, got %f", cardName, actualValue)
					}
				} else {
					if !exists {
						t.Errorf("expected card resource %s not found", cardName)
						continue
					}
					if actualValue != expectedValue {
						t.Errorf("expected card %s value %f, got %f", cardName, expectedValue, actualValue)
					}
				}
			}
		})
	}
}

// TestBuildQueueAttrByJob tests the buildQueueAttrByJob function
func TestBuildQueueAttrByJob(t *testing.T) {
	tests := []struct {
		name                     string
		session                  *framework.Session
		job                      *api.JobInfo
		queueOpts                map[api.QueueID]*queueAttr
		isCardUnlimitedCpuMemory bool
		expectedAllocatedCPU     float64
		expectedAllocatedMemory  float64
		expectedCardName         v1.ResourceName
		expectedCardQty          float64
		expectedError            bool
	}{
		{
			name: "job with running tasks isCardUnlimitedCpuMemory=false",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
						},
					},
				},
			},
			job: api.NewJobInfo("job-1", &api.TaskInfo{
				UID:       "task-1",
				Name:      "task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				TransactionContext: api.TransactionContext{
					Status: api.Running,
				},
			}),
			queueOpts: map[api.QueueID]*queueAttr{
				"queue-1": {
					queueID:   "queue-1",
					name:      "test-queue",
					allocated: api.EmptyResource(),
					request:   api.EmptyResource(),
					elastic:   api.EmptyResource(),
					inqueue:   api.EmptyResource(),
				},
			},
			isCardUnlimitedCpuMemory: false,
			expectedAllocatedCPU:     1000,
			expectedAllocatedMemory:  2 * 1024 * 1024 * 1024,
			expectedCardName:         "",
			expectedCardQty:          0,
			expectedError:            false,
		},
		{
			name: "job with running tasks and card resources isCardUnlimitedCpuMemory=false",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
						},
					},
				},
			},
			job: api.NewJobInfo("job-1", &api.TaskInfo{
				UID:       "task-1",
				Name:      "task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				TransactionContext: api.TransactionContext{
					Status: api.Running,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"volcano.sh/card.name": "Tesla-V100",
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
			}),
			queueOpts: map[api.QueueID]*queueAttr{
				"queue-1": {
					queueID:   "queue-1",
					name:      "test-queue",
					allocated: api.EmptyResource(),
					request:   api.EmptyResource(),
					elastic:   api.EmptyResource(),
					inqueue:   api.EmptyResource(),
				},
			},
			isCardUnlimitedCpuMemory: false,
			expectedAllocatedCPU:     1000,
			expectedAllocatedMemory:  2 * 1024 * 1024 * 1024,
			expectedCardName:         "Tesla-V100",
			expectedCardQty:          2000,
			expectedError:            false,
		},
		{
			name: "job with running tasks isCardUnlimitedCpuMemory=true",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
						},
					},
				},
			},
			job: api.NewJobInfo("job-1", &api.TaskInfo{
				UID:       "task-1",
				Name:      "task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				TransactionContext: api.TransactionContext{
					Status: api.Running,
				},
			}),
			queueOpts: map[api.QueueID]*queueAttr{
				"queue-1": {
					queueID:   "queue-1",
					name:      "test-queue",
					allocated: api.EmptyResource(),
					request:   api.EmptyResource(),
					elastic:   api.EmptyResource(),
					inqueue:   api.EmptyResource(),
				},
			},
			isCardUnlimitedCpuMemory: true,
			expectedAllocatedCPU:     1000,
			expectedAllocatedMemory:  2 * 1024 * 1024 * 1024,
			expectedCardName:         "",
			expectedCardQty:          0,
			expectedError:            false,
		},
		{
			name: "job with running tasks and card resources isCardUnlimitedCpuMemory=true",
			session: &framework.Session{
				Queues: map[api.QueueID]*api.QueueInfo{
					"queue-1": {
						UID:  "queue-1",
						Name: "test-queue",
						Queue: &scheduling.Queue{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-queue",
							},
						},
					},
				},
			},
			job: api.NewJobInfo("job-1", &api.TaskInfo{
				UID:       "task-1",
				Name:      "task-1",
				Namespace: "default",
				Job:       "job-1",
				Resreq: &api.Resource{
					MilliCPU: 1000,
					Memory:   2 * 1024 * 1024 * 1024,
				},
				TransactionContext: api.TransactionContext{
					Status: api.Running,
				},
				Pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"volcano.sh/card.name": "Tesla-V100",
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
			}),
			queueOpts: map[api.QueueID]*queueAttr{
				"queue-1": {
					queueID:   "queue-1",
					name:      "test-queue",
					allocated: api.EmptyResource(),
					request:   api.EmptyResource(),
					elastic:   api.EmptyResource(),
					inqueue:   api.EmptyResource(),
				},
			},
			isCardUnlimitedCpuMemory: true,
			expectedAllocatedCPU:     0,
			expectedAllocatedMemory:  0,
			expectedCardName:         "Tesla-V100",
			expectedCardQty:          2000,
			expectedError:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &Plugin{
				queueOpts: tt.queueOpts,
				cardNameToResourceName: map[v1.ResourceName]v1.ResourceName{
					"Tesla-V100": "nvidia.com/gpu",
				},
				isCardUnlimitedCpuMemory: tt.isCardUnlimitedCpuMemory,
			}

			tt.job.SetPodGroup(&api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-job",
						Namespace: "default",
					},
					Spec: scheduling.PodGroupSpec{
						Queue: "queue-1",
					},
				},
			})

			result := plugin.buildQueueAttrByJob(tt.session, tt.job)

			if tt.expectedError {
				if result {
					t.Error("expected error but got success")
				}
			} else {
				if !result {
					t.Error("expected success but got error")
				}

				qAttr := plugin.queueOpts[tt.job.Queue]
				if qAttr.allocated.MilliCPU != tt.expectedAllocatedCPU {
					t.Errorf("expected allocated CPU %f, got %f", tt.expectedAllocatedCPU, qAttr.allocated.MilliCPU)
				}

				if qAttr.allocated.Memory != tt.expectedAllocatedMemory {
					t.Errorf("expected allocated Memory %f, got %f", tt.expectedAllocatedMemory, qAttr.allocated.Memory)
				}

				if qAttr.request.ScalarResources[tt.expectedCardName] != tt.expectedCardQty {
					t.Errorf("expected card quantity %f, got %f", tt.expectedCardQty, qAttr.request.ScalarResources[tt.expectedCardName])
				}
			}
		})
	}
}
