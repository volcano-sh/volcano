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

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/volcano/pkg/apis/bus/v1alpha1"
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
		name      string
		job       *v1alpha1.Job
		taskIndex int
		expectPod *v1.Pod
	}{
		{
			name: "Test Create Job Pod",
			job: &v1alpha1.Job{
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
			taskIndex: 0,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
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
			name: "Test Create Job Pod with volumes",
			job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					Volumes: []v1alpha1.VolumeSpec{
						{
							VolumeClaimName: "vc1",
							MountPath:       "/a",
						},
						{
							VolumeClaimName: "vc2",
							MountPath:       "/b",
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
			taskIndex: 0,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc1",
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc2",
									ReadOnly:  false,
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "Containers",
							VolumeMounts: []v1.VolumeMount{
								{
									MountPath: "/a",
									Name:      "xxx",
								},
								{
									MountPath: "/b",
									Name:      "xxx",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Test Create Job Pod with volumes added to controlled resources",
			job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Volumes: []v1alpha1.VolumeSpec{
						{
							MountPath:       "/a",
							VolumeClaimName: "vc1",
						},
						{
							MountPath:       "/b",
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
			taskIndex: 0,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc1",
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc2",
									ReadOnly:  false,
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "Containers",
							VolumeMounts: []v1.VolumeMount{
								{
									MountPath: "/a",
									Name:      "xxx",
								},
								{
									MountPath: "/b",
									Name:      "xxx",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "task specific volumes added to controlled resources",
			job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					SchedulerName: "volcano",
					Volumes: []v1alpha1.VolumeSpec{
						{
							MountPath:       "/a",
							VolumeClaimName: "vc1",
						},
					},
					Tasks: []v1alpha1.TaskSpec{
						{
							Name:     "task1",
							Replicas: 6,
							Volumes: []v1alpha1.VolumeSpec{
								{
									MountPath:       "/b",
									VolumeClaimName: "vc2",
								},
								{
									MountPath:    "/c",
									GenerateName: "gen-pvc-",
								},
							},
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
			taskIndex: 0,
			expectPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1-task1-0",
					Namespace: namespace,
				},
				Spec: v1.PodSpec{
					Volumes: []v1.Volume{
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc1",
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "vc2",
									ReadOnly:  false,
								},
							},
						},
						{
							Name: "xxx",
							VolumeSource: v1.VolumeSource{
								PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
									ClaimName: "gen-pvc-0",
									ReadOnly:  false,
								},
							},
						},
					},
					Containers: []v1.Container{
						{
							Name: "Containers",
							VolumeMounts: []v1.VolumeMount{
								{
									MountPath: "/a",
									Name:      "xxx",
								},
								{
									MountPath: "/b",
									Name:      "xxx",
								},
								{
									MountPath: "/c",
									Name:      "xxx",
								},
							}},
					},
				},
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.name, func(t *testing.T) {
			pod := createJobPod(testcase.job, testcase.job.Spec.Tasks[0], testcase.taskIndex)
			if pod.Name != testcase.expectPod.Name && pod.Namespace != testcase.expectPod.Namespace {
				t.Errorf("Expected Return Value to be %v but got %v", testcase.expectPod, pod)
			}

			// strip volume name
			for i := range pod.Spec.Volumes {
				pod.Spec.Volumes[i].Name = "xxx"
			}
			if !reflect.DeepEqual(pod.Spec.Volumes, testcase.expectPod.Spec.Volumes) {
				t.Errorf("Expected volumes %v but got %v", testcase.expectPod.Spec.Volumes, pod.Spec.Volumes)
				t.Logf("%s", cmp.Diff(testcase.expectPod.Spec.Volumes, pod.Spec.Volumes))
			}

			// strip name
			for i := range pod.Spec.Containers[0].VolumeMounts {
				pod.Spec.Containers[0].VolumeMounts[i].Name = "xxx"
			}
			if !reflect.DeepEqual(pod.Spec.Containers[0].VolumeMounts, testcase.expectPod.Spec.Containers[0].VolumeMounts) {
				t.Errorf("Expected volumeMounts %v but got %v", testcase.expectPod.Spec.Containers[0].VolumeMounts, pod.Spec.Containers[0].VolumeMounts)
				t.Logf("%s", cmp.Diff(testcase.expectPod.Spec.Containers[0].VolumeMounts, pod.Spec.Containers[0].VolumeMounts))
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

			if testcase.ReturnVal != "" && action != "" && testcase.ReturnVal != action {
				t.Errorf("Expected return value to be %s but got %s in case %d", testcase.ReturnVal, action, i)
			}
		})
	}
}

func TestAddResourceList(t *testing.T) {
	testcases := []struct {
		Name string
		List v1.ResourceList
		New  v1.ResourceList
	}{
		{
			Name: "Already Present resource",
			List: map[v1.ResourceName]resource.Quantity{
				"cpu": *resource.NewQuantity(100, ""),
			},
			New: map[v1.ResourceName]resource.Quantity{
				"cpu": *resource.NewQuantity(100, ""),
			},
		},
		{
			Name: "New resource",
			List: map[v1.ResourceName]resource.Quantity{
				"cpu": *resource.NewQuantity(100, ""),
			},
			New: map[v1.ResourceName]resource.Quantity{
				"memory": *resource.NewQuantity(100, ""),
			},
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			addResourceList(testcase.List, testcase.New, nil)
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
