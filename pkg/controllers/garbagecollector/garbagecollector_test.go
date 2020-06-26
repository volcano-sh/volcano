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

package garbagecollector

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	volcanoclient "volcano.sh/volcano/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func TestGarbageCollector_ProcessJob(t *testing.T) {

}

func TestGarbageCollector_ProcessTTL(t *testing.T) {
	namespace := "test"
	var ttlSecond int32 = 3
	var ttlSecondZero int32
	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		ExpectedVal bool
		ExpectedErr error
	}{
		{
			Name: "False Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						LastTransitionTime: metav1.NewTime(time.Now()),
						Phase:              v1alpha1.Completed,
					},
				},
			},
			ExpectedVal: false,
			ExpectedErr: nil,
		},
		{
			Name: "True Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecondZero,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						LastTransitionTime: metav1.NewTime(time.Now()),
						Phase:              v1alpha1.Completed,
					},
				},
			},
			ExpectedVal: true,
			ExpectedErr: nil,
		},
	}
	for i, testcase := range testcases {
		gc := &gccontroller{}
		gc.Initialize(&framework.ControllerOption{
			VolcanoClient: volcanoclient.NewSimpleClientset(),
		})

		expired, err := gc.processTTL(testcase.Job)
		if err != nil {
			t.Error("Did not expect error")
		}
		if expired != testcase.ExpectedVal {
			t.Errorf("Expected Return Value to be %t, but got %t in case %d", testcase.ExpectedVal, expired, i)
		}
	}
}

func TestGarbageCollector_NeedsCleanup(t *testing.T) {
	namespace := "test"

	var ttlSecond int32 = 3

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		ExpectedVal bool
	}{
		{
			Name: "Success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Completed,
					},
				},
			},
			ExpectedVal: true,
		},
		{
			Name: "Failure Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase: v1alpha1.Running,
					},
				},
			},
			ExpectedVal: false,
		},
	}

	for i, testcase := range testcases {
		finished := needsCleanup(testcase.Job)
		if finished != testcase.ExpectedVal {
			t.Errorf("Expected value to be %t, but got: %t in case %d", testcase.ExpectedVal, finished, i)
		}
	}
}

func TestGarbageCollector_IsJobFinished(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		ExpectedVal bool
	}{
		{
			Name: "True Case",
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
			ExpectedVal: true,
		},
		{
			Name: "False Case",
			Job: &v1alpha1.Job{
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
			ExpectedVal: false,
		},
	}

	for i, testcase := range testcases {
		finished := isJobFinished(testcase.Job)
		if finished != testcase.ExpectedVal {
			t.Errorf("Expected value to be %t, but got: %t in case %d", testcase.ExpectedVal, finished, i)
		}
	}
}

func TestGarbageCollector_GetFinishAndExpireTime(t *testing.T) {
	namespace := "test"

	var ttlSecond int32 = 3
	var ttlSecondFail int32 = 2

	testTime := time.Date(1, 1, 1, 1, 1, 1, 0, time.UTC)

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		ExpectedErr error
	}{
		{
			Name: "Success case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase:              v1alpha1.Completed,
						LastTransitionTime: metav1.NewTime(testTime),
					},
				},
			},
			ExpectedErr: nil,
		},
		{
			Name: "Failure case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecondFail,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase:              v1alpha1.Completed,
						LastTransitionTime: metav1.NewTime(testTime),
					},
				},
			},
			ExpectedErr: nil,
		},
	}

	for i, testcase := range testcases {
		finishTime, expireTime, err := getFinishAndExpireTime(testcase.Job)
		if err != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected Error to be: %s but got: %s in case %d", testcase.ExpectedErr, err, i)
		}

		if finishTime != nil && metav1.NewTime(*finishTime) != testcase.Job.Status.State.LastTransitionTime {
			t.Errorf("Expected value to be: %v, but got: %v in case %d", testcase.Job.Status.State.LastTransitionTime, metav1.NewTime(*finishTime), i)
		}

		if expireTime != nil && metav1.NewTime(*expireTime) != metav1.NewTime(testcase.Job.Status.State.LastTransitionTime.Add(time.Duration(*testcase.Job.Spec.TTLSecondsAfterFinished)*time.Second)) {
			t.Errorf("Expected value to be: %v, but got: %v in case %d", testcase.Job.Status.State.LastTransitionTime.Add(time.Duration(*testcase.Job.Spec.TTLSecondsAfterFinished)*time.Second), metav1.NewTime(*expireTime), i)
		}
	}
}

func TestGarbageCollector_TimeLeft(t *testing.T) {
	namespace := "test"

	var ttlSecond int32 = 3

	testTime := time.Date(1, 1, 1, 1, 1, 1, 0, time.UTC)

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		Time        *time.Time
		ExpectedVal time.Duration
		ExpectedErr error
	}{
		{
			Name: "Success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						Phase:              v1alpha1.Completed,
						LastTransitionTime: metav1.NewTime(testTime),
					},
				},
			},
			Time:        &testTime,
			ExpectedVal: time.Duration(3),
			ExpectedErr: nil,
		},
		{
			Name: "Failure Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Spec: v1alpha1.JobSpec{
					TTLSecondsAfterFinished: &ttlSecond,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						LastTransitionTime: metav1.NewTime(testTime),
					},
				},
			},
			Time:        &testTime,
			ExpectedVal: time.Duration(3),
			ExpectedErr: fmt.Errorf("job %s/%s should not be cleaned up", "test", "job1"),
		},
	}

	for i, testcase := range testcases {
		timeDuration, err := timeLeft(testcase.Job, testcase.Time)
		if err != nil && err.Error() != testcase.ExpectedErr.Error() {
			t.Errorf("Expected Error to be: %s but got: %s in case %d", testcase.ExpectedErr, err, i)
		}

		if timeDuration != nil && timeDuration.Seconds() != float64(testcase.ExpectedVal*time.Second)/1e9 {
			t.Errorf("Expected Value to be: %v but got: %f in case %d", testcase.ExpectedVal, timeDuration.Seconds(), i)
		}
	}
}

func TestGarbageCollector_JobFinishTime(t *testing.T) {
	namespace := "test"

	testcases := []struct {
		Name        string
		Job         *v1alpha1.Job
		ExpectedVal error
	}{
		{
			Name: "Success Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
				Status: v1alpha1.JobStatus{
					State: v1alpha1.JobState{
						LastTransitionTime: metav1.NewTime(time.Now()),
					},
				},
			},
			ExpectedVal: nil,
		},
		{
			Name: "Failure Case",
			Job: &v1alpha1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "job1",
					Namespace: namespace,
				},
			},
			ExpectedVal: fmt.Errorf("unable to find the time when the Job %s/%s finished", "test", "job1"),
		},
	}

	for i, testcase := range testcases {
		_, err := jobFinishTime(testcase.Job)
		if err != nil && err.Error() != testcase.ExpectedVal.Error() {
			t.Errorf("Expected Error to be: %s but got: %s in case %d", testcase.ExpectedVal, err, i)
		}
	}
}
