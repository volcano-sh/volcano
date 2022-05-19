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
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1alpha2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

func TestAbortedState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "AbortedState-ResumeAction case",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Aborted,
						},
					},
				},
			},
			Action:      busv1alpha1.ResumeJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "AbortedState-AnyOtherAction case",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Aborted,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			absState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = absState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}
			if testcase.Action == busv1alpha1.ResumeJobAction {
				jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
				if err != nil {
					t.Error("Error while retrieving value from Cache")
				}

				if jobInfo.Job.Status.State.Phase != v1alpha1.Restarting {
					t.Error("Expected Phase to be equal to restarting phase")
				}
			}
		})
	}
}

func TestAbortingState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "AbortedState-ResumeAction case",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Aborting,
						},
					},
				},
			},
			Action:      busv1alpha1.ResumeJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "AbortedState-AnyOtherAction case with pods count equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Aborting,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "AbortedState-AnyOtherAction case with Pods count not equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Pending: 1,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Aborting,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodPending, nil),
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			absState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = absState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}
			if testcase.Action == busv1alpha1.ResumeJobAction {
				jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
				if err != nil {
					t.Error("Error while retrieving value from Cache")
				}

				if jobInfo.Job.Status.RetryCount == 0 {
					t.Error("Retry Count should not be zero")
				}
			}

			if testcase.Action != busv1alpha1.ResumeJobAction {
				jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
				if err != nil {
					t.Error("Error while retrieving value from Cache")
				}

				if testcase.JobInfo.Job.Status.Pending == 0 && testcase.JobInfo.Job.Status.Running == 0 && testcase.JobInfo.Job.Status.Terminating == 0 {
					if jobInfo.Job.Status.State.Phase != v1alpha1.Aborted {
						t.Error("Phase Should be aborted")
					}
				} else {
					if jobInfo.Job.Status.State.Phase != v1alpha1.Aborting {
						t.Error("Phase Should be aborted")
					}
				}
			}
		})
	}
}

func TestCompletingState_Execute(t *testing.T) {

	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "CompletingState- With pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Running: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completing,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.ResumeJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "CompletingState- With pod count equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completing,
						},
					},
				},
			},
			Action:      busv1alpha1.ResumeJobAction,
			ExpectedVal: nil,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}

			jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
			if err != nil {
				t.Error("Error while retrieving value from Cache")
			}

			if testcase.JobInfo.Job.Status.Running == 0 && testcase.JobInfo.Job.Status.Pending == 0 && testcase.JobInfo.Job.Status.Terminating == 0 {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Completed {
					fmt.Println(jobInfo.Job.Status.State.Phase)
					t.Errorf("Expected Phase to be Completed State in test case: %d", i)
				}
			} else {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Completing {
					t.Errorf("Expected Phase to be completing state in test case: %d", i)
				}
			}
		})
	}
}

func TestFinishedState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "FinishedState Test Case",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completed,
						},
					},
				},
			},
			Action:      busv1alpha1.ResumeJobAction,
			ExpectedVal: nil,
		},
	}

	for _, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}
		})
	}
}

func TestPendingState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "PendingState- RestartJobAction case With terminating pod count equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- RestartJobAction case With terminating pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- AbortJobAction case With terminating pod count equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
			},
			Action:      busv1alpha1.AbortJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- AbortJobAction case With terminating pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.AbortJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- TerminateJobAction case With terminating pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.TerminateJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- CompleteJobAction case With terminating pod count equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
			},
			Action:      busv1alpha1.CompleteJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- CompleteJobAction case With terminating pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.CompleteJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- EnqueueAction case With Min Available equal to running pods",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						MinAvailable: 3,
					},
					Status: v1alpha1.JobStatus{
						Running: 3,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
						"pod3": buildPod(namespace, "pod3", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.EnqueueAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- EnqueueAction case With Min Available not equal to running pods",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						MinAvailable: 3,
					},
					Status: v1alpha1.JobStatus{
						Running: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.EnqueueAction,
			ExpectedVal: nil,
		},
		{
			Name: "PendingState- Default case With Min Available equal to running pods",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						MinAvailable: 3,
					},
					Status: v1alpha1.JobStatus{
						Running: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Pending,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.SyncJobAction,
			ExpectedVal: nil,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			patches := gomonkey.ApplyMethod(reflect.TypeOf(fakecontroller), "GetQueueInfo", func(_ *jobcontroller, _ string) (*schedulingv1alpha2.Queue, error) {
				return &schedulingv1alpha2.Queue{}, nil
			})

			defer patches.Reset()

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}

			jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
			if err != nil {
				t.Error("Error while retrieving value from Cache")
			}

			if testcase.Action == busv1alpha1.RestartJobAction {
				// always jump to restarting firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Restarting {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Restarting, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.AbortJobAction {
				// always jump to aborting firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Aborting {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Aborting, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.TerminateJobAction {
				// always jump to completing firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Terminating {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Terminating, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.CompleteJobAction {
				// always jump to completing firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Completing {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Completing, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.EnqueueAction {
				if jobInfo.Job.Spec.MinAvailable <= jobInfo.Job.Status.Running+jobInfo.Job.Status.Succeeded+jobInfo.Job.Status.Failed {
					if jobInfo.Job.Status.State.Phase != v1alpha1.Running {
						t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Running, jobInfo.Job.Status.State.Phase, i)
					}
				}
			} else {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Pending {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Pending, jobInfo.Job.Status.State.Phase, i)
				}
			}
		})
	}
}

func TestRestartingState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "RestartingState- RetryCount is equal to or greater than MaxRetry",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						MaxRetry: 3,
					},
					Status: v1alpha1.JobStatus{
						RetryCount: 3,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Restarting,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RestartingState- RetryCount is less than MaxRetry",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						MaxRetry: 3,
						Tasks: []v1alpha1.TaskSpec{
							{
								Name:     "task1",
								Replicas: 1,
							},
							{
								Name:     "task2",
								Replicas: 1,
							},
						},
					},
					Status: v1alpha1.JobStatus{
						RetryCount:   1,
						MinAvailable: 1,
						Terminating:  0,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Restarting,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}

			jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
			if err != nil {
				t.Error("Error while retrieving value from Cache")
			}

			if testcase.JobInfo.Job.Spec.MaxRetry <= testcase.JobInfo.Job.Status.RetryCount {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Failed {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Failed, jobInfo.Job.Status.State.Phase, i)
				}
			} else {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Pending {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Pending, jobInfo.Job.Status.State.Phase, i)
				}
			}
		})
	}
}

func TestRunningState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "RunningState- RestartJobAction case and Terminating Pods not equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- RestartJobAction case and Terminating Pods equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 0,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			Action:      busv1alpha1.RestartJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- AbortAction case and Terminating Pods equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 0,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			Action:      busv1alpha1.AbortJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- AbortAction case and Terminating Pods not equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.AbortJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- TerminateJobAction case and Terminating Pods equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 0,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			Action:      busv1alpha1.TerminateJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- TerminateJobAction case and Terminating Pods not equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.TerminateJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- CompleteJobAction case and Terminating Pods equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 0,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			Action:      busv1alpha1.CompleteJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- CompleteJobAction case and Terminating Pods not equal to 0",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{},
					Status: v1alpha1.JobStatus{
						Terminating: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.CompleteJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- Default case and Total is equal to failed+succeeded",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						Tasks: []v1alpha1.TaskSpec{
							{
								Name:     "task1",
								Replicas: 2,
								Template: v1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Name: "task1",
									},
								},
							},
						},
					},
					Status: v1alpha1.JobStatus{
						Succeeded: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"job1-task1-0": buildPod(namespace, "pod1", v1.PodSucceeded, nil),
						"job1-task1-1": buildPod(namespace, "pod2", v1.PodSucceeded, nil),
					},
				},
			},
			Action:      busv1alpha1.SyncJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "RunningState- Default case and Total is not equal to failed+succeeded",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Spec: v1alpha1.JobSpec{
						Tasks: []v1alpha1.TaskSpec{
							{
								Name:     "task1",
								Replicas: 2,
								Template: v1.PodTemplateSpec{
									ObjectMeta: metav1.ObjectMeta{
										Name: "task1",
									},
								},
							},
						},
					},
					Status: v1alpha1.JobStatus{
						Succeeded: 1,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"job1-task1-0": buildPod(namespace, "pod1", v1.PodSucceeded, nil),
					},
				},
			},
			Action:      busv1alpha1.SyncJobAction,
			ExpectedVal: nil,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			patches := gomonkey.ApplyMethod(reflect.TypeOf(fakecontroller), "GetQueueInfo", func(_ *jobcontroller, _ string) (*schedulingv1alpha2.Queue, error) {
				return &schedulingv1alpha2.Queue{}, nil
			})

			defer patches.Reset()

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}

			jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
			if err != nil {
				t.Error("Error while retrieving value from Cache")
			}

			if testcase.Action == busv1alpha1.RestartJobAction {
				// always jump to restarting firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Restarting {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Restarting, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.AbortJobAction {
				// always jump to aborting firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Aborting {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Restarting, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.TerminateJobAction {
				// always jump to terminating firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Terminating {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Terminating, jobInfo.Job.Status.State.Phase, i)
				}
			} else if testcase.Action == busv1alpha1.CompleteJobAction {
				// always jump to completing firstly
				if jobInfo.Job.Status.State.Phase != v1alpha1.Completing {
					t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Restarting, jobInfo.Job.Status.State.Phase, i)
				}
			} else {
				total := state.TotalTasks(testcase.JobInfo.Job)
				if total == testcase.JobInfo.Job.Status.Succeeded+testcase.JobInfo.Job.Status.Failed {
					if jobInfo.Job.Status.State.Phase != v1alpha1.Completed {
						t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Completed, jobInfo.Job.Status.State.Phase, i)
					}
				} else {
					if jobInfo.Job.Status.State.Phase != v1alpha1.Running {
						t.Errorf("Expected Job phase to %s, but got %s in case %d", v1alpha1.Running, jobInfo.Job.Status.State.Phase, i)
					}
				}
			}
		})
	}
}

func TestTerminatingState_Execute(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobInfo     *apis.JobInfo
		Action      busv1alpha1.Action
		ExpectedVal error
	}{
		{
			Name: "TerminatingState- With pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						Running: 2,
						State: v1alpha1.JobState{
							Phase: v1alpha1.Terminating,
						},
					},
				},
				Pods: map[string]map[string]*v1.Pod{
					"task1": {
						"pod1": buildPod(namespace, "pod1", v1.PodRunning, nil),
						"pod2": buildPod(namespace, "pod2", v1.PodRunning, nil),
					},
				},
			},
			Action:      busv1alpha1.TerminateJobAction,
			ExpectedVal: nil,
		},
		{
			Name: "TerminatingState- With pod count not equal to zero",
			JobInfo: &apis.JobInfo{
				Namespace: namespace,
				Name:      "jobinfo1",
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "Job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Terminating,
						},
					},
				},
			},
			Action:      busv1alpha1.TerminateJobAction,
			ExpectedVal: nil,
		},
	}

	for i, testcase := range testcases {
		t.Run(testcase.Name, func(t *testing.T) {
			testState := state.NewState(testcase.JobInfo)

			fakecontroller := newFakeController()
			state.KillJob = fakecontroller.killJob

			_, err := fakecontroller.vcClient.BatchV1alpha1().Jobs(namespace).Create(context.TODO(), testcase.JobInfo.Job, metav1.CreateOptions{})
			if err != nil {
				t.Error("Error while creating Job")
			}

			err = fakecontroller.cache.Add(testcase.JobInfo.Job)
			if err != nil {
				t.Error("Error while adding Job in cache")
			}

			err = testState.Execute(testcase.Action)
			if err != nil {
				t.Errorf("Expected Error not to occur but got: %s", err)
			}

			jobInfo, err := fakecontroller.cache.Get(fmt.Sprintf("%s/%s", testcase.JobInfo.Job.Namespace, testcase.JobInfo.Job.Name))
			if err != nil {
				t.Error("Error while retrieving value from Cache")
			}

			if testcase.JobInfo.Job.Status.Running == 0 && testcase.JobInfo.Job.Status.Pending == 0 && testcase.JobInfo.Job.Status.Terminating == 0 {

				if jobInfo.Job.Status.State.Phase != v1alpha1.Terminated {
					fmt.Println(jobInfo.Job.Status.State.Phase)
					t.Errorf("Expected Phase to be Terminated State in test case: %d", i)
				}
			} else {
				if jobInfo.Job.Status.State.Phase != v1alpha1.Terminating {
					t.Errorf("Expected Phase to be Terminating state in test case: %d", i)
				}
			}
		})
	}
}
