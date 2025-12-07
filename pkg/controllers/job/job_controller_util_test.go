/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestMakePodName(t *testing.T) {
	testcases := []struct {
		Name      string
		TaskName  string
		JobName   string
		Index     int
		ReturnVal string
	}{
		{
			Name:      "Test MakePodName function",
			TaskName:  "task1",
			JobName:   "job1",
			Index:     1,
			ReturnVal: "job1-task1-1",
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			podName := MakePodName(testcase.JobName, testcase.TaskName, testcase.Index)

			if podName != testcase.ReturnVal {
				t.Errorf("Expected Return value to be: %s, but got: %s in case %d", testcase.ReturnVal, podName, i)
			}
		})

	}
}

// Basic test case: Verify basic properties of normally created Pods
func TestCreateJobPod_Basic(t *testing.T) {
	// Prepare test data
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "test-ns",
			UID:       uuid.NewUUID(),
		},
		Spec: v1alpha1.JobSpec{
			SchedulerName: "volcano",
			Queue:         "test-queue",
		},
		Status: v1alpha1.JobStatus{
			Version:    1,
			RetryCount: 0,
		},
	}

	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-task",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "test-container", Image: "busybox"}},
		},
	}

	ts := &v1alpha1.TaskSpec{
		Name: "test-task",
	}

	// Call the test function
	pod := createJobPod(job, template, 0, false, nil, ts)

	// Verify basic properties
	if pod.Name != "test-job-test-task-0" {
		t.Errorf("expected pod name 'test-job-test-task-0', got %s", pod.Name)
	}
	if pod.Namespace != "test-ns" {
		t.Errorf("expected pod namespace 'test-ns', got %s", pod.Namespace)
	}
	if pod.Spec.SchedulerName != "volcano" {
		t.Errorf("expected scheduler name 'volcano', got %s", pod.Spec.SchedulerName)
	}

	// Verify labels
	if pod.Labels[v1alpha1.JobNameKey] != "test-job" {
		t.Errorf("expected job name label 'test-job', got %s", pod.Labels[v1alpha1.JobNameKey])
	}
	if pod.Labels[v1alpha1.TaskIndex] != "0" {
		t.Errorf("expected task index '0', got %s", pod.Labels[v1alpha1.TaskIndex])
	}

	// Verify annotations
	if pod.Annotations[v1alpha1.JobVersion] != "1" {
		t.Errorf("expected job version '1', got %s", pod.Annotations[v1alpha1.JobVersion])
	}
	if pod.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey] != "test-job-"+string(job.UID) {
		t.Errorf("expected pod group annotation 'test-job-%s', got %s", job.UID, pod.Annotations[schedulingv1beta1.KubeGroupNameAnnotationKey])
	}
}

// Test case: Verify priority when scheduler name exists in Pod template
func TestCreateJobPod_SchedulerNameOverride(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "test-ns"},
		Spec: v1alpha1.JobSpec{
			SchedulerName: "job-scheduler", // Job-level scheduler
		},
	}

	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "test-task"},
		Spec: v1.PodSpec{
			SchedulerName: "template-scheduler", // Template-level scheduler (should take precedence)
			Containers:    []v1.Container{{Name: "test", Image: "busybox"}},
		},
	}

	pod := createJobPod(job, template, 0, false, nil, &v1alpha1.TaskSpec{})
	if pod.Spec.SchedulerName != "template-scheduler" {
		t.Errorf("expected scheduler name 'template-scheduler', got %s", pod.Spec.SchedulerName)
	}
}

// Test case: Verify priority class inheritance logic
func TestCreateJobPod_PriorityClass(t *testing.T) {
	// Scenario 1: No priority class in template, but exists in job
	job1 := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "job1", Namespace: "test-ns"},
		Spec: v1alpha1.JobSpec{
			PriorityClassName: "high-priority",
		},
	}
	template1 := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "task1"},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "test", Image: "busybox"}}},
	}
	pod1 := createJobPod(job1, template1, 0, false, nil, &v1alpha1.TaskSpec{})
	if pod1.Spec.PriorityClassName != "high-priority" {
		t.Errorf("case 1: expected priority class 'high-priority', got %s", pod1.Spec.PriorityClassName)
	}

	// Scenario 2: Priority class exists in both template and job (template takes precedence)
	template2 := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "task2"},
		Spec: v1.PodSpec{
			PriorityClassName: "highest-priority",
			Containers:        []v1.Container{{Name: "test", Image: "busybox"}},
		},
	}
	pod2 := createJobPod(job1, template2, 0, false, nil, &v1alpha1.TaskSpec{})
	if pod2.Spec.PriorityClassName != "highest-priority" {
		t.Errorf("case 2: expected priority class 'highest-priority', got %s", pod2.Spec.PriorityClassName)
	}
}

// Test case: Verify Volume mounting logic
func TestCreateJobPod_Volumes(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "vol-job", Namespace: "test-ns"},
		Spec: v1alpha1.JobSpec{
			Volumes: []v1alpha1.VolumeSpec{
				{
					VolumeClaimName: "pvc-1",
					MountPath:       "/data1",
				},
				{
					VolumeClaimName: "pvc-2",
					MountPath:       "/data2",
				},
			},
		},
	}

	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "vol-task"},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "test", Image: "busybox"}},
		},
	}

	pod := createJobPod(job, template, 0, false, nil, &v1alpha1.TaskSpec{})

	// Verify number of Volumes
	if len(pod.Spec.Volumes) != 2 {
		t.Errorf("expected 2 volumes, got %d", len(pod.Spec.Volumes))
	}

	// Verify VolumeMounts
	if len(pod.Spec.Containers[0].VolumeMounts) != 2 {
		t.Errorf("expected 2 volume mounts, got %d", len(pod.Spec.Containers[0].VolumeMounts))
	}

	// Verify mount paths
	mountPaths := make([]string, len(pod.Spec.Containers[0].VolumeMounts))
	for i, vm := range pod.Spec.Containers[0].VolumeMounts {
		mountPaths[i] = vm.MountPath
	}
	if !contains(mountPaths, "/data1") || !contains(mountPaths, "/data2") {
		t.Errorf("missing expected mount paths, got %v", mountPaths)
	}
}

// Test case: Verify handling of duplicate Volumes (duplicates should be skipped)
func TestCreateJobPod_DuplicateVolumes(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "dup-job", Namespace: "test-ns"},
		Spec: v1alpha1.JobSpec{
			Volumes: []v1alpha1.VolumeSpec{
				{VolumeClaimName: "pvc-1", MountPath: "/data1"},
				{VolumeClaimName: "pvc-1", MountPath: "/data1"}, // Duplicate PVC
			},
		},
	}

	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "dup-task"},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "test", Image: "busybox"}}},
	}

	pod := createJobPod(job, template, 0, false, nil, &v1alpha1.TaskSpec{})
	if len(pod.Spec.Volumes) != 1 {
		t.Errorf("expected 1 volume (duplicate skipped), got %d", len(pod.Spec.Volumes))
	}
}

// Test case: Verify JobForwarding labels and annotations
func TestCreateJobPod_JobForwarding(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "forward-job", Namespace: "test-ns"},
	}
	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "forward-task"},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "test", Image: "busybox"}}},
	}

	pod := createJobPod(job, template, 0, true, nil, &v1alpha1.TaskSpec{})
	if pod.Annotations[v1alpha1.JobForwardingKey] != "true" {
		t.Errorf("expected job forwarding annotation 'true', got %s", pod.Annotations[v1alpha1.JobForwardingKey])
	}
	if pod.Labels[v1alpha1.JobForwardingKey] != "true" {
		t.Errorf("expected job forwarding label 'true', got %s", pod.Labels[v1alpha1.JobForwardingKey])
	}
}

// Test case: Verify partition policy labels
func TestCreateJobPod_PartitionPolicy(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "partition-job", Namespace: "test-ns"},
	}
	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Name: "partition-task"},
		Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "test", Image: "busybox"}}},
	}
	ts := &v1alpha1.TaskSpec{
		Name: "partition-task",
		PartitionPolicy: &v1alpha1.PartitionPolicySpec{
			PartitionSize: 2, // 2 Pods per partition
		},
	}

	// Test index 0 (partition 0)
	pod0 := createJobPod(job, template, 0, false, nil, ts)
	if pod0.Labels[v1alpha1.TaskPartitionID] != "0" {
		t.Errorf("expected partition 0 for index 0, got %s", pod0.Labels[v1alpha1.TaskPartitionID])
	}

	// Test index 3 (partition 1: 3/2=1)
	pod3 := createJobPod(job, template, 3, false, nil, ts)
	if pod3.Labels[v1alpha1.TaskPartitionID] != "1" {
		t.Errorf("expected partition 1 for index 3, got %s", pod3.Labels[v1alpha1.TaskPartitionID])
	}
}

// TestCreatePodWithPartitionPolicy verifies the impact of partition policy on Pod labels
func TestCreatePodWithPartitionPolicy(t *testing.T) {
	tests := []struct {
		name          string
		partitionSize int32
		index         int
		expectedPart  string
		expectedTask  string
	}{
		{
			name:          "partition size 2, index 0",
			partitionSize: 2,
			index:         0,
			expectedPart:  "0",
			expectedTask:  "test-task",
		},
		{
			name:          "partition size 2, index 1",
			partitionSize: 2,
			index:         1,
			expectedPart:  "0",
			expectedTask:  "test-task",
		},
		{
			name:          "partition size 2, index 2",
			partitionSize: 2,
			index:         2,
			expectedPart:  "1",
			expectedTask:  "test-task",
		},
		{
			name:          "partition size 5, index 10",
			partitionSize: 5,
			index:         10,
			expectedPart:  "2",
			expectedTask:  "test-task",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Construct test Job and TaskSpec
			job := &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: tt.expectedTask,
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize: tt.partitionSize,
							},
						},
					},
				},
			}

			template := &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.expectedTask,
				},
				Spec: v1.PodSpec{},
			}

			// Call Pod creation function
			pod := createJobPod(job, template, tt.index, false, nil, &job.Spec.Tasks[0])

			// Verify partition label
			if pod.Labels[v1alpha1.TaskPartitionID] != tt.expectedPart {
				t.Errorf("expected partition ID %s, got %s", tt.expectedPart, pod.Labels[v1alpha1.TaskPartitionID])
			}

			// Verify task name label
			if pod.Labels[v1alpha1.TaskNameKey] != tt.expectedTask {
				t.Errorf("expected task name %s, got %s", tt.expectedTask, pod.Labels[v1alpha1.TaskNameKey])
			}
		})
	}
}

// TestPartitionPolicyWithoutPolicy verifies no related labels are set when there's no partition policy
func TestPartitionPolicyWithoutPolicy(t *testing.T) {
	job := &v1alpha1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job",
			Namespace: "default",
		},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				{
					Name:            "test-task",
					PartitionPolicy: nil, // No partition policy
				},
			},
		},
	}

	template := &v1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-task",
		},
	}

	pod := createJobPod(job, template, 0, false, nil, &job.Spec.Tasks[0])

	// Verify no partition-related labels are set
	if _, ok := pod.Labels[v1alpha1.TaskPartitionID]; ok {
		t.Error("should not set partition ID label when no partition policy")
	}

	if _, ok := pod.Labels[v1alpha1.TaskNameKey]; ok {
		t.Error("should not set task name label when no partition policy")
	}
}

// TestPartitionPolicyEdgeCases tests boundary conditions
func TestPartitionPolicyEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		partitionSize int32
		index         int
		expectedPart  string
	}{
		{
			name:          "partition size 1, index 0",
			partitionSize: 1,
			index:         0,
			expectedPart:  "0",
		},
		{
			name:          "partition size 0 (invalid), index 5",
			partitionSize: 0,
			index:         5,
			expectedPart:  "-1", // Go returns 0 for division by zero
		},
		{
			name:          "negative index (invalid), size 3",
			partitionSize: 3,
			index:         -2,
			expectedPart:  "0", // Negative divided by positive is negative in Go, verify label format here
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{Name: "test-job"},
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: "test-task",
							PartitionPolicy: &v1alpha1.PartitionPolicySpec{
								PartitionSize: tt.partitionSize,
							},
						},
					},
				},
			}

			template := &v1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Name: "test-task"}}
			pod := createJobPod(job, template, tt.index, false, nil, &job.Spec.Tasks[0])

			if pod.Labels[v1alpha1.TaskPartitionID] != tt.expectedPart {
				t.Errorf("expected %s, got %s", tt.expectedPart, pod.Labels[v1alpha1.TaskPartitionID])
			}
		})
	}
}

// Helper function: Check if slice contains specified element
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestApplyPolicies(t *testing.T) {
	namespace := "test"
	errorCode0 := int32(0)

	testcases := []struct {
		Name      string
		Job       *v1alpha1.Job
		Request   *apis.Request
		ReturnVal busv1alpha1.Action
	}{
		{
			Name: "Test Apply policies where Action is not empty",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				Action: busv1alpha1.EnqueueAction,
			},
			ReturnVal: busv1alpha1.EnqueueAction,
		},
		{
			Name: "Test Apply policies where event is OutOfSync",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				Event: busv1alpha1.OutOfSyncEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where event is PodRunning",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				Event: busv1alpha1.PodRunningEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where job uid is inconsistent, ignore the existing policy action in the job and execute syncjob",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
					UID:       "job1-uid-10001",
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
							Policies: []v1alpha1.LifecyclePolicy{
								{
									Action:   busv1alpha1.TerminateJobAction,
									Event:    busv1alpha1.PodEvictedEvent,
									ExitCode: &errorCode0,
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				JobUid: "job1-uid-10000",
				Event:  busv1alpha1.PodEvictedEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where version is outdated",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				JobVersion: 1,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where overriding job level policies and with exitcode",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
							Policies: []v1alpha1.LifecyclePolicy{
								{
									Action:   busv1alpha1.SyncJobAction,
									Event:    busv1alpha1.CommandIssuedEvent,
									ExitCode: &errorCode0,
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				TaskName: "task1",
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where overriding job level policies and without exitcode",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
							Policies: []v1alpha1.LifecyclePolicy{
								{
									Action: busv1alpha1.SyncJobAction,
									Event:  busv1alpha1.CommandIssuedEvent,
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				TaskName: "task1",
				Event:    busv1alpha1.CommandIssuedEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				TaskName: "task1",
				Event:    busv1alpha1.CommandIssuedEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action: busv1alpha1.SyncJobAction,
							Event:  busv1alpha1.CommandIssuedEvent,
						},
					},
				},
			},
			Request: &apis.Request{
				Event: busv1alpha1.CommandIssuedEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies, the event is PodPending with timeout",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action: busv1alpha1.RestartPodAction,
							Event:  busv1alpha1.PodPendingEvent,
							Timeout: &metav1.Duration{
								Duration: 10 * time.Second,
							},
						},
					},
				},
			},
			Request: &apis.Request{
				Event: busv1alpha1.PodPendingEvent,
			},
			ReturnVal: busv1alpha1.RestartPodAction,
		},
		{
			Name: "Test Apply policies with job level policies, the event is PodPending without timeout",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action: busv1alpha1.RestartPodAction,
							Event:  busv1alpha1.PodPendingEvent,
						},
					},
				},
			},
			Request: &apis.Request{
				Event: busv1alpha1.PodPendingEvent,
			},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies with exitcode",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods",
									Namespace: namespace,
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name: "Containers",
										},
									},
								},
							},
						},
					},
					Policies: []v1alpha1.LifecyclePolicy{
						{
							Action:   busv1alpha1.SyncJobAction,
							Event:    busv1alpha1.CommandIssuedEvent,
							ExitCode: &errorCode0,
						},
					},
				},
			},
			Request:   &apis.Request{},
			ReturnVal: busv1alpha1.SyncJobAction,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			action := applyPolicies(testcase.Job, testcase.Request)

			if testcase.ReturnVal != "" && action.action != "" && testcase.ReturnVal != action.action {
				t.Errorf("Expected return value to be %s but got %s in case %d", testcase.ReturnVal, action.action, i)
			}
		})
	}
}

func TestTasksPriority_Less(t *testing.T) {
	testcases := []struct {
		Name          string
		TasksPriority TasksPriority
		Task1Index    int
		Task2Index    int
		ReturnVal     bool
	}{
		{
			Name: "False Case",
			TasksPriority: []TaskPriority{
				{
					priority: 1,
				},
				{
					priority: 2,
				},
				{
					priority: 3,
				},
			},
			Task1Index: 1,
			Task2Index: 2,
			ReturnVal:  false,
		},
		{
			Name: "True Case",
			TasksPriority: []TaskPriority{
				{
					priority: 1,
				},
				{
					priority: 2,
				},
				{
					priority: 3,
				},
			},
			Task1Index: 2,
			Task2Index: 1,
			ReturnVal:  true,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			less := testcase.TasksPriority.Less(testcase.Task1Index, testcase.Task2Index)

			if less != testcase.ReturnVal {
				t.Errorf("Expected Return Value to be %t, but got %t in case %d", testcase.ReturnVal, less, i)
			}
		})
	}
}

func TestTasksPriority_Swap(t *testing.T) {
	testcases := []struct {
		Name          string
		TasksPriority TasksPriority
		Task1Index    int
		Task2Index    int
		ReturnVal     bool
	}{
		{
			Name: "False Case",
			TasksPriority: []TaskPriority{
				{
					priority: 1,
				},
				{
					priority: 2,
				},
				{
					priority: 3,
				},
			},
			Task1Index: 1,
			Task2Index: 2,
		},
		{
			Name: "True Case",
			TasksPriority: []TaskPriority{
				{
					priority: 1,
				},
				{
					priority: 2,
				},
				{
					priority: 3,
				},
			},
			Task1Index: 2,
			Task2Index: 1,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(_ *testing.T) {
			testcase.TasksPriority.Swap(testcase.Task1Index, testcase.Task2Index)
		})
	}
}

var (
	worker = v1alpha1.TaskSpec{
		Name:     "worker",
		Replicas: 2,
		Template: v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "Containers",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("100m"),
							},
						},
					},
				},
			},
		},
	}
	master = v1alpha1.TaskSpec{
		Name:     "master",
		Replicas: 2,
		Template: v1.PodTemplateSpec{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "Containers",
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU: resource.MustParse("50m"),
							},
						},
					},
				},
			},
		},
	}
)

func TestTaskPriority_CalcPGMin(t *testing.T) {

	oneMinAvailable := int32(1)
	zeroMinAvailable := int32(0)

	testcases := []struct {
		Name              string
		TasksPriority     TasksPriority
		TasksMinAvailable []*int32
		JobMinMember      int32
		ExpectValue       v1.ResourceList
	}{
		{ // jobMinAvailable = sum(taskMinAvailable)
			Name: "job's min available is 0, master's and workers is set to 1: min=1*master+1*worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker,
				},
			},
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(150, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(150, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable = sum(taskMinAvailable)
			Name: "job's min available is 0, master's is null and worker's is set to 1: min=2*master+worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master, priority: 2,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      0,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(200, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(200, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable = sum(taskMinAvailable)
			Name: "job's min available is 3, master's is null and worker's is set to 1: min=2*master+1*worker ",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      3,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(200, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(200, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable = sum(taskMinAvailable)
			Name: "job's min available is 3, master's is 1 and worker's is null: min=1*master+2*worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&oneMinAvailable, nil},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(250, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(250, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable = sum(taskMinAvailable)
			Name: "job's min available is 2, master's and worker's is set to 1: min=1*master+1*worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      2,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(150, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(150, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			Name: "job's min available is 3, master's and worker's is set to 1: min=1*master+1*worker+1*master(high-priority)",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master, priority: 2,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(200, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(200, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			Name: "job's min available is 3, master's and worker's is set to 1: min=1*worker+1*master+1*worker(high-priority)",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker, priority: 2,
				},
			},
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(250, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(250, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			Name: "job's min available is 3, master's is 0 and worker's is set to 1: min=1*worker+1*worker(high-priority)+1*master",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker, priority: 2,
				},
			},
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&zeroMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(250, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(250, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			Name: "job's min available is 4, master's is null and worker's is set to 1: min=1*worker+2*master+1*worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker, priority: 2,
				},
			},
			JobMinMember:      4,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(300, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(300, resource.DecimalSI),
				"pods": *resource.NewQuantity(4, resource.DecimalSI), "count/pods": *resource.NewQuantity(4, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			Name: "job's min available is 4, master's and worker's is set to 1: min=1*worker+1*master+1*worker(hi-prio)+1*master",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker, priority: 2,
				},
			},
			JobMinMember:      4,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(300, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(300, resource.DecimalSI),
				"pods": *resource.NewQuantity(4, resource.DecimalSI), "count/pods": *resource.NewQuantity(4, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable < sum(taskMinAvailable)
			Name: "job's min available is 2, master's is null and worker's is set to 1: min=2*master",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master, priority: 2,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      2,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(100, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(100, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable < sum(taskMinAvailable)
			Name: "job's min available is 2, master's is null and worker's is set to 1: min=1*worker+1*master",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker, priority: 2,
				},
			},
			JobMinMember:      2,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(150, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(150, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable < sum(taskMinAvailable)
			Name: "job's min available is 2, master's is 1 and worker's is set to bull: min=1*master+1*worker",
			TasksPriority: []TaskPriority{
				{
					TaskSpec: master,
				},
				{
					TaskSpec: worker,
				},
			},
			JobMinMember:      2,
			TasksMinAvailable: []*int32{&oneMinAvailable, nil},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(150, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(150, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
	}
	for i, testcase := range testcases {
		jobMinMember := int32(0)
		for i, min := range testcase.TasksMinAvailable {
			// patch task min available
			if min == nil {
				min = &testcase.TasksPriority[i].TaskSpec.Replicas
			}
			testcase.TasksPriority[i].TaskSpec.MinAvailable = min
			// patch job min member
			if testcase.JobMinMember == 0 {
				jobMinMember += *testcase.TasksPriority[i].TaskSpec.MinAvailable
			}
		}
		if testcase.JobMinMember == 0 {
			testcase.JobMinMember = jobMinMember
		}
		gotMin := testcase.TasksPriority.CalcPGMinResources(testcase.JobMinMember)
		if !reflect.DeepEqual(gotMin, testcase.ExpectValue) {
			t.Fatalf("case %d/%v: expected %v got %v", i, testcase.Name, testcase.ExpectValue, gotMin)
		}
	}
}

func TestCalcPGMinResources(t *testing.T) {
	jc := newFakeController()
	job := &v1alpha1.Job{
		TypeMeta: metav1.TypeMeta{},
		Spec: v1alpha1.JobSpec{
			Tasks: []v1alpha1.TaskSpec{
				master, worker,
			},
		},
	}

	oneMinAvailable := int32(1)
	//zeroMinAvailable := int32(0)

	tests := []struct {
		TasksMinAvailable []*int32
		JobMinMember      int32
		ExpectValue       v1.ResourceList
	}{
		// jobMinAvailable < sum(taskMinAvailable)
		{
			JobMinMember:      2,
			TasksMinAvailable: []*int32{&oneMinAvailable, nil},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(100, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(100, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{
			JobMinMember:      2,
			TasksMinAvailable: []*int32{nil, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(100, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(100, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
		{
			JobMinMember:      3,
			TasksMinAvailable: []*int32{nil, nil},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(200, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(200, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{ // jobMinAvailable > sum(taskMinAvailable)
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(200, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(200, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		// jobMinAvailable = sum(taskMinAvailable)
		{
			JobMinMember:      3,
			TasksMinAvailable: []*int32{&oneMinAvailable, nil},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(250, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(250, resource.DecimalSI),
				"pods": *resource.NewQuantity(3, resource.DecimalSI), "count/pods": *resource.NewQuantity(3, resource.DecimalSI),
			},
		},
		{
			JobMinMember:      2,
			TasksMinAvailable: []*int32{&oneMinAvailable, &oneMinAvailable},
			ExpectValue: v1.ResourceList{
				v1.ResourceCPU: *resource.NewMilliQuantity(150, resource.DecimalSI), "requests.cpu": *resource.NewMilliQuantity(150, resource.DecimalSI),
				"pods": *resource.NewQuantity(2, resource.DecimalSI), "count/pods": *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
	}
	for i, tt := range tests {
		job.Spec.MinAvailable = tt.JobMinMember
		for i := range tt.TasksMinAvailable {
			job.Spec.Tasks[i].MinAvailable = tt.TasksMinAvailable[i]
		}
		// simulating patch in webhook
		for i, task := range job.Spec.Tasks {
			if task.MinAvailable == nil {
				min := &task.Replicas
				job.Spec.Tasks[i].MinAvailable = min
			}
		}
		gotMin := jc.calcPGMinResources(job)
		if !reflect.DeepEqual(gotMin, &tt.ExpectValue) {
			t.Fatalf("case %d: expected %v got %v", i, tt.ExpectValue, gotMin)
		}

	}
}
