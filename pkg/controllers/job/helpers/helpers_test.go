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
			generateTaskInfo("prod-worker-21", createTime),
			generateTaskInfo("prod-worker-1", createTime),
			false,
		},
		{
			generateTaskInfo("prod-worker-0", createTime),
			generateTaskInfo("prod-worker-3", createTime),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime),
			generateTaskInfo("prod-worker-3", createTime.Add(time.Hour)),
			true,
		},
		{
			generateTaskInfo("prod-worker", createTime),
			generateTaskInfo("prod-worker-3", createTime.Add(-time.Hour)),
			false,
		},
	}

	for i, item := range items {
		if value := CompareTask(item.lv, item.rv); value != item.expect {
			t.Errorf("case %d: expected: %v, got %v", i, item.expect, value)
		}
	}
}

func generateTaskInfo(name string, createTime time.Time) *api.TaskInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: metav1.Time{Time: createTime},
		},
	}
	return &api.TaskInfo{
		Name: name,
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
			index := GetTasklndexUnderJob(testCase.TaskName, testCase.Job)
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
