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

package cache

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestJobCache_Add(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		JobsInCache map[string]*v1alpha1.Job
		ExpectedVal error
	}{
		{
			Name: "Success case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			JobsInCache: nil,
			ExpectedVal: nil,
		},
		{
			Name: "Error case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
				},
			},
			ExpectedVal: fmt.Errorf("duplicated jobInfo <%s/%s>", "test", "job1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		err := jobCache.Add(testcase.Job)
		if err != nil && testcase.ExpectedVal != nil && err.Error() != testcase.ExpectedVal.Error() {
			t.Errorf("Expected Return Value to be: %s, but got: %s in case %d", testcase.ExpectedVal, err, i)
		}
	}
}

func TestJobCache_GetStatus(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		JobsInCache map[string]*v1alpha1.Job
		ExpectedVal v1alpha1.JobState
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completed,
						},
					},
				},
			},
			ExpectedVal: v1alpha1.JobState{
				Phase: v1alpha1.Completed,
			},
			ExpectedErr: nil,
		},
		{
			Name: "Error Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			JobsInCache: nil,
			ExpectedVal: v1alpha1.JobState{
				Phase: v1alpha1.Completed,
			},
			ExpectedErr: fmt.Errorf("failed to find job <%s/%s>", namespace, "job1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		status, err := jobCache.GetStatus(fmt.Sprintf("%s/%s", testcase.Job.Namespace, testcase.Job.Name))
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected to get: %s, but got: %s in case %d", testcase.ExpectedErr, err, i)
		}
		if status != nil && status.State.Phase != testcase.ExpectedVal.Phase {
			t.Errorf("Expected Return Value to be: %s, but got: %s in case %d", testcase.ExpectedVal, status.State.Phase, i)
		}
	}
}

func TestJobCache_Get(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Key         string
		JobsInCache map[string]*v1alpha1.Job
		ExpectedVal *apis.JobInfo
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			Key:  fmt.Sprintf("%s/%s", namespace, "job1"),
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completed,
						},
					},
				},
			},
			ExpectedVal: &apis.JobInfo{
				Name:      "job1",
				Namespace: namespace,
				Job: &v1alpha1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Completed,
						},
					},
				},
			},
			ExpectedErr: nil,
		},
		{
			Name:        "Error Case",
			Key:         fmt.Sprintf("%s/%s", namespace, "job1"),
			JobsInCache: nil,
			ExpectedVal: &apis.JobInfo{},
			ExpectedErr: fmt.Errorf("failed to find job <%s/%s>", namespace, "job1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		job, err := jobCache.Get(testcase.Key)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected to get: %s, but got: %s in case %d", testcase.ExpectedErr, err, i)
		}
		if job != nil && (job.Name != testcase.ExpectedVal.Name || job.Job.Name != testcase.ExpectedVal.Job.Name) {
			fmt.Println(job.Job)
			t.Errorf("Expected Return Value to be same but got different values in case %d", i)
		}
	}
}

func TestJobCache_Update(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		UpdatedJob  *v1alpha1.Job
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:            "job1",
						Namespace:       namespace,
						ResourceVersion: "100",
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			UpdatedJob: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Completed,
					},
				},
			},
			ExpectedErr: nil,
		},
		{
			Name:        "Error Case",
			JobsInCache: nil,
			UpdatedJob: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "job1",
					Namespace:       namespace,
					ResourceVersion: "100",
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Completed,
					},
				},
			},
			ExpectedErr: fmt.Errorf("failed to find job <%s/%s>", namespace, "job1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		err := jobCache.Update(testcase.UpdatedJob)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected to get: %s, but got: %s in case %d", testcase.ExpectedErr, err, i)
		}
		if testcase.ExpectedErr == nil {
			job, err := jobCache.Get(fmt.Sprintf("%s/%s", testcase.UpdatedJob.Namespace, testcase.UpdatedJob.Name))
			if err != nil {
				t.Errorf("Expected Error not to have occurred in case %d", i)
			}
			if job.Job.Status.State.Phase != testcase.UpdatedJob.Status.State.Phase {
				t.Errorf("Error in updating Job, Expected: %s, but got: %s in case %d", testcase.UpdatedJob.Status.State.Phase, job.Job.Status.State.Phase, i)
			}
		}
	}
}

func TestJobCache_Delete(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		DeleteJob   *v1alpha1.Job
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			DeleteJob: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Completed,
					},
				},
			},
			ExpectedErr: nil,
		},
		{
			Name:        "Error Case",
			JobsInCache: nil,
			DeleteJob: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Completed,
					},
				},
			},
			ExpectedErr: fmt.Errorf("failed to find job <%s/%s>", namespace, "job1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		err := jobCache.Delete(testcase.DeleteJob)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected to get: %s, but got: %s in case %d", testcase.ExpectedErr, err, i)
		}
		if testcase.ExpectedErr == nil {
			job, err := jobCache.Get(fmt.Sprintf("%s/%s", testcase.DeleteJob.Namespace, testcase.DeleteJob.Name))
			if err == nil {
				t.Errorf("Expected Error to have occurred in case %d", i)
			}
			if job != nil {
				t.Errorf("Expected Job to be nil but got value in case %d", i)
			}
		}
	}
}

func TestJobCache_AddPod(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		AddPod      *v1.Pod
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			AddPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					Annotations: map[string]string{
						v1alpha1.JobNameKey:  "job1",
						v1alpha1.TaskSpecKey: "task1",
						v1alpha1.JobVersion:  "1",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
			},
			ExpectedErr: nil,
		},
		{
			Name:        "Error Case",
			JobsInCache: nil,
			AddPod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					Annotations: map[string]string{
						v1alpha1.TaskSpecKey: "task1",
						v1alpha1.JobVersion:  "1",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
			},
			ExpectedErr: fmt.Errorf("failed to find job name of pod <%s/%s>", namespace, "pod1"),
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		err := jobCache.AddPod(testcase.AddPod)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected Error to be: %s, but got: %s in case %d", testcase.ExpectedErr.Error(), err.Error(), i)
		}

		if err == nil {
			job, err := jobCache.Get(fmt.Sprintf("%s/%s", testcase.JobsInCache["job1"].Namespace, testcase.JobsInCache["job1"].Name))
			if err != nil {
				t.Errorf("Expected Error not to occur while retrieving job from cache in case %d", i)
			}
			if err == nil {
				if len(job.Pods) != 1 {
					t.Errorf("Expected Len to 1 but got %d in case %d", len(job.Pods), i)
				}
			}
		}
	}
}

func TestJobCache_DeletePod(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		AddPod      map[string]*v1.Pod
		DeletePod   *v1.Pod
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			AddPod: map[string]*v1.Pod{
				"pod1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
				"pod2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
			DeletePod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					Annotations: map[string]string{
						v1alpha1.JobNameKey:  "job1",
						v1alpha1.TaskSpecKey: "task1",
						v1alpha1.JobVersion:  "1",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodRunning,
				},
			},
			ExpectedErr: nil,
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		for _, pod := range testcase.AddPod {
			err := jobCache.AddPod(pod)
			if err != nil {
				t.Errorf("Expected Error not occur when adding Adding Pod in case %d", i)
			}
		}

		err := jobCache.DeletePod(testcase.DeletePod)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected Error to be: %s, but got: %s in case %d", testcase.ExpectedErr.Error(), err.Error(), i)
		}

		if err == nil {
			job, err := jobCache.Get(fmt.Sprintf("%s/%s", namespace, "job1"))
			if err != nil {
				t.Errorf("Expected Error not to have occurred but got error: %s in case %d", err, i)
			}
			if len(job.Pods["task1"]) != 1 {
				t.Errorf("Expected total pods to be 1, but got: %d in case %d", len(job.Pods["task1"]), i)
			}
		}
	}
}

func TestJobCache_UpdatePod(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		AddPod      map[string]*v1.Pod
		UpdatePod   *v1.Pod
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			AddPod: map[string]*v1.Pod{
				"pod1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
				"pod2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
			UpdatePod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: namespace,
					Annotations: map[string]string{
						v1alpha1.JobNameKey:  "job1",
						v1alpha1.TaskSpecKey: "task1",
						v1alpha1.JobVersion:  "1",
					},
				},
				Status: v1.PodStatus{
					Phase: v1.PodSucceeded,
				},
			},
			ExpectedErr: nil,
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		for _, pod := range testcase.AddPod {
			err := jobCache.AddPod(pod)
			if err != nil {
				t.Errorf("Expected Error not occur when adding Adding Pod in case %d", i)
			}
		}

		err := jobCache.UpdatePod(testcase.UpdatePod)
		if err != nil && testcase.ExpectedErr != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected Error to be: %s, but got: %s in case %d", testcase.ExpectedErr.Error(), err.Error(), i)
		}

		if err == nil {
			job, err := jobCache.Get(fmt.Sprintf("%s/%s", namespace, "job1"))
			if err != nil {
				t.Errorf("Expected Error not to have occurred but got error: %s in case %d", err, i)
			}
			for _, task := range job.Pods {
				for _, pod := range task {
					if pod.Name == testcase.UpdatePod.Name {
						if pod.Status.Phase != testcase.UpdatePod.Status.Phase {
							t.Errorf("Expected Pod status to be updated to %s, but got %s in case %d", testcase.UpdatePod.Status.Phase, pod.Status.Phase, i)
						}
					}
				}
			}
		}
	}
}

func TestJobCache_TaskCompleted(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		JobsInCache map[string]*v1alpha1.Job
		AddPod      map[string]*v1.Pod
		ExpectedVal bool
	}{
		{
			Name: "Success Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Spec: v1alpha1.JobSpec{
						Tasks: []v1alpha1.TaskSpec{
							{
								Name:     "task1",
								Replicas: 2,
							},
						},
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			AddPod: map[string]*v1.Pod{
				"pod1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodSucceeded,
					},
				},
				"pod2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodSucceeded,
					},
				},
			},
			ExpectedVal: true,
		},
		{
			Name: "False Case",
			JobsInCache: map[string]*v1alpha1.Job{
				"job1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "job1",
						Namespace: namespace,
					},
					Spec: v1alpha1.JobSpec{
						Tasks: []v1alpha1.TaskSpec{
							{
								Name:     "task1",
								Replicas: 2,
							},
						},
					},
					Status: v1alpha1.JobStatus{
						State: v1alpha1.JobState{
							Phase: v1alpha1.Running,
						},
					},
				},
			},
			AddPod: map[string]*v1.Pod{
				"pod1": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodSucceeded,
					},
				},
				"pod2": {
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: namespace,
						Annotations: map[string]string{
							v1alpha1.JobNameKey:  "job1",
							v1alpha1.TaskSpecKey: "task1",
							v1alpha1.JobVersion:  "1",
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
			ExpectedVal: false,
		},
	}

	for i, testcase := range testcases {
		jobCache := New()

		for _, job := range testcase.JobsInCache {
			err := jobCache.Add(job)
			if err != nil {
				t.Errorf("Expected not to occur while adding job, but got error: %s in case %d", err, i)
			}
		}

		for _, pod := range testcase.AddPod {
			err := jobCache.AddPod(pod)
			if err != nil {
				t.Errorf("Expected Error not occur when adding Adding Pod in case %d", i)
			}
		}

		completed := jobCache.TaskCompleted(fmt.Sprintf("%s/%s", namespace, "job1"), "task1")
		if completed != testcase.ExpectedVal {
			t.Errorf("Expected Return Value to be: %t, but got: %t in case %d", testcase.ExpectedVal, completed, i)
		}
	}
}
