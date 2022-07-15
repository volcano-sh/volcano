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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatchv1 "volcano.sh/apis/pkg/apis/batch/v1"
	vcbusv1 "volcano.sh/apis/pkg/apis/bus/v1"
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

func TestCreateJobPod(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *vcbatchv1.Job
		PodTemplate *v1.PodTemplateSpec
		Index       int
		ReturnVal   *v1.Pod
	}{
		{
			Name: "Test Create Job Pod",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					Tasks: []vcbatchv1.TaskSpec{
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
			PodTemplate: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
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
			Index: 0,
			ReturnVal: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
			},
		},
		{
			Name: "Test Create Job Pod with volumes",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					Volumes: []vcbatchv1.VolumeSpec{
						{
							VolumeClaimName: "vc1",
						},
						{
							VolumeClaimName: "vc2",
						},
					},
					Tasks: []vcbatchv1.TaskSpec{
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
			PodTemplate: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
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
			Index: 0,
			ReturnVal: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
			},
		},
		{
			Name: "Test Create Job Pod with volumes added to controlled resources",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Volumes: []vcbatchv1.VolumeSpec{
						{
							VolumeClaimName: "vc1",
							VolumeClaim: &v1.PersistentVolumeClaimSpec{
								VolumeName: "v1",
							},
						},
						{
							VolumeClaimName: "vc2",
						},
					},
					Tasks: []vcbatchv1.TaskSpec{
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
			PodTemplate: &v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
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
			Index: 0,
			ReturnVal: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
			},
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			pod := createJobPod(testcase.Job, testcase.PodTemplate, vcbatchv1.NumaPolicy(""), testcase.Index, false)

			if testcase.ReturnVal != nil && pod != nil && pod.Name != testcase.ReturnVal.Name && pod.Namespace != testcase.ReturnVal.Namespace {
				t.Errorf("Expected Return Value to be %v but got %v in case %d", testcase.ReturnVal, pod, i)
			}
		})
	}
}

func TestApplyPolicies(t *testing.T) {
	namespace := "test"
	errorCode0 := int32(0)

	testcases := []struct {
		Name      string
		Job       *vcbatchv1.Job
		Request   *apis.Request
		ReturnVal vcbusv1.Action
	}{
		{
			Name: "Test Apply policies where Action is not empty",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
				Action: vcbusv1.EnqueueAction,
			},
			ReturnVal: vcbusv1.EnqueueAction,
		},
		{
			Name: "Test Apply policies where event is OutOfSync",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
				Event: vcbusv1.OutOfSyncEvent,
			},
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where version is outdated",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where overriding job level policies and with exitcode",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
							Policies: []vcbatchv1.LifecyclePolicy{
								{
									Action:   vcbusv1.SyncJobAction,
									Event:    vcbusv1.CommandIssuedEvent,
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
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies where overriding job level policies and without exitcode",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
							Policies: []vcbatchv1.LifecyclePolicy{
								{
									Action: vcbusv1.SyncJobAction,
									Event:  vcbusv1.CommandIssuedEvent,
								},
							},
						},
					},
				},
			},
			Request: &apis.Request{
				TaskName: "task1",
				Event:    vcbusv1.CommandIssuedEvent,
			},
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
				Event:    vcbusv1.CommandIssuedEvent,
			},
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
					Policies: []vcbatchv1.LifecyclePolicy{
						{
							Action: vcbusv1.SyncJobAction,
							Event:  vcbusv1.CommandIssuedEvent,
						},
					},
				},
			},
			Request: &apis.Request{
				Event: vcbusv1.CommandIssuedEvent,
			},
			ReturnVal: vcbusv1.SyncJobAction,
		},
		{
			Name: "Test Apply policies with job level policies with exitcode",
			Job: &vcbatchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: vcbatchv1.JobSpec{
					SchedulerName: "volcano",
					Tasks: []vcbatchv1.TaskSpec{
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
					Policies: []vcbatchv1.LifecyclePolicy{
						{
							Action:   vcbusv1.SyncJobAction,
							Event:    vcbusv1.CommandIssuedEvent,
							ExitCode: &errorCode0,
						},
					},
				},
			},
			Request:   &apis.Request{},
			ReturnVal: vcbusv1.SyncJobAction,
		},
	}

	for i, testcase := range testcases {

		t.Run(testcase.Name, func(t *testing.T) {
			action := applyPolicies(testcase.Job, testcase.Request)

			if testcase.ReturnVal != "" && action != "" && testcase.ReturnVal != action {
				t.Errorf("Expected return value to be %s but got %s in case %d", testcase.ReturnVal, action, i)
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
		t.Run(testcase.Name, func(t *testing.T) {
			testcase.TasksPriority.Swap(testcase.Task1Index, testcase.Task2Index)
		})
	}
}
