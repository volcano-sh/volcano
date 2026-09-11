/*
Copyright 2017 The Volcano Authors.

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

package state

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestRunningState_RestartJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "RestartJobAction: transitions to Restarting",
			expectedPhase: vcbatch.Restarting,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Running)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedRetryCount int32
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{RetryCount: 2}
				fn(status)
				capturedPhase = status.State.Phase
				capturedRetryCount = status.RetryCount
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.RestartJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
			if capturedRetryCount != 3 {
				t.Errorf("expected RetryCount 3, got %d", capturedRetryCount)
			}
		})
	}
}

func TestRunningState_RestartTaskAction(t *testing.T) {
	testcases := []struct {
		name          string
		action        v1alpha1.Action
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "RestartTaskAction: transitions to Restarting",
			action:        v1alpha1.RestartTaskAction,
			expectedPhase: vcbatch.Restarting,
		},
		{
			name:          "RestartPodAction: transitions to Restarting",
			action:        v1alpha1.RestartPodAction,
			expectedPhase: vcbatch.Restarting,
		},
		{
			name:          "RestartPartitionAction: transitions to Restarting",
			action:        v1alpha1.RestartPartitionAction,
			expectedPhase: vcbatch.Restarting,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Running)

			origKillTarget := KillTarget
			t.Cleanup(func() { KillTarget = origKillTarget })

			var capturedPhase vcbatch.JobPhase
			KillTarget = func(job *apis.JobInfo, target Target, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: tc.action}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestRunningState_AbortJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "AbortJobAction: transitions to Aborting",
			expectedPhase: vcbatch.Aborting,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Running)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.AbortJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestRunningState_TerminateJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "TerminateJobAction: transitions to Terminating",
			expectedPhase: vcbatch.Terminating,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Running)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.TerminateJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestRunningState_CompleteJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "CompleteJobAction: transitions to Completing",
			expectedPhase: vcbatch.Completing,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Running)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.CompleteJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestRunningState_SyncJobAction(t *testing.T) {
	int32Ptr := func(n int32) *int32 { return &n }

	testcases := []struct {
		name          string
		tasks         []vcbatch.TaskSpec
		minAvailable  int32
		minSuccess    *int32
		status        vcbatch.JobStatus
		expectedPhase vcbatch.JobPhase
		expectChanged bool
	}{
		{
			name:          "Scale-to-zero: no tasks, no phase change",
			tasks:         []vcbatch.TaskSpec{},
			minAvailable:  0,
			status:        vcbatch.JobStatus{},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name:          "MinSuccess met: transitions to Completed",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 5}},
			minAvailable:  1,
			minSuccess:    int32Ptr(3),
			status:        vcbatch.JobStatus{Succeeded: 3},
			expectedPhase: vcbatch.Completed,
			expectChanged: true,
		},
		{
			name: "All done, task MinAvailable not met: transitions to Failed",
			tasks: []vcbatch.TaskSpec{
				{Name: "master", Replicas: 1, MinAvailable: int32Ptr(1)},
				{Name: "worker", Replicas: 1, MinAvailable: int32Ptr(1)},
			},
			minAvailable: 2,
			status: vcbatch.JobStatus{
				Succeeded: 1,
				Failed:    1,
				TaskStatusCount: map[string]vcbatch.TaskState{
					"worker": {Phase: map[v1.PodPhase]int32{v1.PodSucceeded: 0}},
				},
			},
			expectedPhase: vcbatch.Failed,
			expectChanged: true,
		},
		{
			name:          "All done, MinSuccess not met: transitions to Failed",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 5}},
			minAvailable:  5,
			minSuccess:    int32Ptr(3),
			status:        vcbatch.JobStatus{Succeeded: 2, Failed: 3},
			expectedPhase: vcbatch.Failed,
			expectChanged: true,
		},
		{
			name:          "All done, Succeeded >= MinAvailable: transitions to Completed",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			minAvailable:  2,
			status:        vcbatch.JobStatus{Succeeded: 2, Failed: 1},
			expectedPhase: vcbatch.Completed,
			expectChanged: true,
		},
		{
			name:          "All done, Succeeded < MinAvailable: transitions to Failed",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			minAvailable:  3,
			status:        vcbatch.JobStatus{Succeeded: 1, Failed: 2},
			expectedPhase: vcbatch.Failed,
			expectChanged: true,
		},
		{
			name:          "Too many pending pods: transitions to Pending",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 5}},
			minAvailable:  3,
			status:        vcbatch.JobStatus{Pending: 3, Running: 0},
			expectedPhase: vcbatch.Pending,
			expectChanged: true,
		},
		{
			name:          "Normal running: no phase change",
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 5}},
			minAvailable:  3,
			status:        vcbatch.JobStatus{Running: 4, Pending: 1},
			expectedPhase: "",
			expectChanged: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := &apis.JobInfo{
				Job: &vcbatch.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-job",
						Namespace: "default",
					},
					Spec: vcbatch.JobSpec{
						Tasks:        tc.tasks,
						MinAvailable: tc.minAvailable,
						MinSuccess:   tc.minSuccess,
					},
					Status: vcbatch.JobStatus{
						State: vcbatch.JobState{Phase: vcbatch.Running},
					},
				},
			}

			origSyncJob := SyncJob
			t.Cleanup(func() { SyncJob = origSyncJob })

			var capturedPhase vcbatch.JobPhase
			var capturedChanged bool
			SyncJob = func(job *apis.JobInfo, fn UpdateStatusFn) error {
				status := tc.status
				capturedChanged = fn(&status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &runningState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.SyncJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedChanged != tc.expectChanged {
				t.Errorf("expected phaseChanged=%v, got %v", tc.expectChanged, capturedChanged)
			}
			if tc.expectChanged && capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}
