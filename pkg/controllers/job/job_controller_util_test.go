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

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
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
		Job         *v1alpha1.Job
		PodTemplate *v1.PodTemplateSpec
		Index       int
		ReturnVal   *v1.Pod
	}{
		{
			Name: "Test Create Job Pod",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
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
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					Volumes: []v1alpha1.VolumeSpec{
						{
							VolumeClaimName: "vc1",
						},
						{
							VolumeClaimName: "vc2",
						},
					},
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
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Volumes: []v1alpha1.VolumeSpec{
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
			pod := createJobPod(testcase.Job, testcase.PodTemplate, "", testcase.Index, false)

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
