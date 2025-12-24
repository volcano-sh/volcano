/*
Copyright 2021 The Volcano Authors.

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

package helpers

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestCompareTask(t *testing.T) {
	createTime := time.Now()
	items := []struct {
		lv     *api.TaskInfo
		rv     *api.TaskInfo
		expect bool
	}{
		{
			generateTaskInfo("prod-worker-21", createTime, "pod-1"),
			generateTaskInfo("prod-worker-1", createTime, "pod-2"),
			false,
		},
		{
			generateTaskInfo("prod-worker-0", createTime, "pod-1"),
			generateTaskInfo("prod-worker-3", createTime, "pod-2"),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime, "pod-1"),
			generateTaskInfo("prod-worker-3", createTime.Add(time.Hour), "pod-2"),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime, "pod-1"),
			generateTaskInfo("prod-worker-3", createTime.Add(-time.Hour), "pod-2"),
			false,
		},
		{
			generateTaskInfo("prod-worker", createTime, "pod-1"),
			generateTaskInfo("prod-worker-3", createTime, "pod-2"),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime, "pod-2"),
			generateTaskInfo("prod-worker-3", createTime, "pod-1"),
			false,
		},
	}

	for i, item := range items {
		if value := CompareTask(item.lv, item.rv); value != item.expect {
			t.Errorf("case %d: expected: %v, got %v", i, item.expect, value)
		}
	}
}

func generateTaskInfo(name string, createTime time.Time, uid api.TaskID) *api.TaskInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Time{Time: createTime},
		},
	}
	return &api.TaskInfo{
		Name: name,
		UID:  uid,
		Pod:  pod,
	}
}

func TestGetTasklndexUnderJobFunc(t *testing.T) {
	namespace := "test"
	testCases := []struct {
		Name     string
		TaskName string
		Job      *batch.Job
		Expect   int
	}{
		{
			Name:     "GetTasklndexUnderJob1",
			TaskName: "task1",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 2,
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
						{
							Name:     "task2",
							Replicas: 2,
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
			Expect: 0,
		},
		{
			Name:     "GetTasklndexUnderJob2",
			TaskName: "task2",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 2,
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
						{
							Name:     "task2",
							Replicas: 2,
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
			Expect: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			index := GetTaskIndexUnderJob(testCase.TaskName, testCase.Job)
			if index != testCase.Expect {
				t.Errorf("GetTasklndexUnderJobFunc(%s) = %d, expect %d", testCase.TaskName, index, testCase.Expect)
			}
		})
	}
}

func TestGetPodsNameUnderTaskFunc(t *testing.T) {
	namespace := "test"
	testCases := []struct {
		Name     string
		TaskName string
		Job      *batch.Job
		Expect   []string
	}{
		{
			Name:     "GetTasklndexUnderJob1",
			TaskName: "task1",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods1",
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
						{
							Name:     "task2",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods2",
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
			Expect: []string{"job1-task1-0", "job1-task1-1"},
		},
		{
			Name:     "GetTasklndexUnderJob2",
			TaskName: "task2",
			Job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods1",
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
						{
							Name:     "task2",
							Replicas: 2,
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "pods2",
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
			Expect: []string{"job1-task2-0", "job1-task2-1"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			pods := GetPodsNameUnderTask(testCase.TaskName, testCase.Job)
			for _, pod := range pods {
				if !contains(testCase.Expect, pod) {
					t.Errorf("Test case failed: %s, expect: %v, got: %v", testCase.Name, testCase.Expect, pods)
				}
			}
		})
	}
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

func TestGetTaskIndexOfPod(t *testing.T) {
	testCases := []struct {
		name      string
		pod       *v1.Pod
		expected  int
		expectErr bool
	}{
		{
			name: "valid task index should return correct value",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						"volcano.sh/task-index": "0",
					},
				},
			},
			expected:  0,
			expectErr: false,
		},
		{
			name: "missing task index label should return error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-pod",
					Labels: map[string]string{},
				},
			},
			expected:  -1,
			expectErr: true,
		},
		{
			name: "invalid task index format should return error",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						"volcano.sh/task-index": "invalid",
					},
				},
			},
			expected:  -1,
			expectErr: true,
		},
		{
			name: "negative task index should be valid",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						"volcano.sh/task-index": "-1",
					},
				},
			},
			expected:  -1,
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			taskIndex, err := GetTaskIndexOfPod(tc.pod)

			if tc.expectErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if taskIndex != tc.expected {
					t.Errorf("expected task index %d, got %d", tc.expected, taskIndex)
				}
			}
		})
	}
}

// TestGetTaskReplicasUnderJob tests the GetTaskReplicasUnderJob function
func TestGetTaskReplicasUnderJob(t *testing.T) {
	testCases := []struct {
		name     string
		taskName string
		job      *batch.Job
		expected int32
	}{
		{
			name:     "should return correct replicas for existing task",
			taskName: "task1",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 3,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "task2",
							Replicas: 5,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 3,
		},
		{
			name:     "should return correct replicas for task with zero replicas",
			taskName: "task2",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 3,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "task2",
							Replicas: 0,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 0,
		},
		{
			name:     "should return zero for non-existent task",
			taskName: "non-existent-task",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 3,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 0,
		},
		{
			name:     "should return zero for empty task list",
			taskName: "any-task",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{},
				},
			},
			expected: 0,
		},
		{
			name:     "should return correct replicas for task with large replica count",
			taskName: "large-task",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "large-task",
							Replicas: 1000,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 1000,
		},
		{
			name:     "should return zero for case-sensitive task name mismatch",
			taskName: "TASK1",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task1",
							Replicas: 3,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 0,
		},
		{
			name:     "should return correct replicas for task with special characters in name",
			taskName: "task-with-dash",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "task-with-dash",
							Replicas: 7,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 7,
		},
		{
			name:     "should return correct replicas for task in multi-task job",
			taskName: "middle-task",
			job: &batch.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-job",
					Namespace: "default",
				},
				Spec: batch.JobSpec{
					Tasks: []batch.TaskSpec{
						{
							Name:     "first-task",
							Replicas: 2,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "middle-task",
							Replicas: 4,
							Template: v1.PodTemplateSpec{},
						},
						{
							Name:     "last-task",
							Replicas: 6,
							Template: v1.PodTemplateSpec{},
						},
					},
				},
			},
			expected: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetTaskReplicasUnderJob(tc.taskName, tc.job)

			if result != tc.expected {
				t.Errorf("expected replicas %d for task '%s', but got %d",
					tc.expected, tc.taskName, result)
			}
		})
	}
}
