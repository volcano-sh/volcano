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

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestRestartingState_SyncJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		maxRetry      int32
		tasks         []vcbatch.TaskSpec
		retryCount    int32
		terminating   int32
		minAvailable  int32
		expectedPhase vcbatch.JobPhase
		expectChanged bool
	}{
		{
			name:          "Max retries exceeded: transitions to Failed",
			maxRetry:      3,
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			retryCount:    3,
			terminating:   0,
			minAvailable:  1,
			expectedPhase: vcbatch.Failed,
			expectChanged: true,
		},
		{
			name:          "Enough pods available: transitions to Pending",
			maxRetry:      5,
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			retryCount:    1,
			terminating:   1,
			minAvailable:  2,
			expectedPhase: vcbatch.Pending,
			expectChanged: true,
		},
		{
			name:          "Still waiting for pods to terminate: no phase change",
			maxRetry:      5,
			tasks:         []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			retryCount:    1,
			terminating:   3,
			minAvailable:  2,
			expectedPhase: "",
			expectChanged: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Restarting)
			jobInfo.Job.Spec.MaxRetry = tc.maxRetry
			jobInfo.Job.Spec.Tasks = tc.tasks

			origSyncJob := SyncJob
			t.Cleanup(func() { SyncJob = origSyncJob })

			var capturedPhase vcbatch.JobPhase
			var capturedChanged bool
			SyncJob = func(job *apis.JobInfo, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{
					RetryCount:   tc.retryCount,
					Terminating:  tc.terminating,
					MinAvailable: tc.minAvailable,
				}
				capturedChanged = fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &restartingState{job: jobInfo}
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

func TestRestartingState_RestartTaskAction(t *testing.T) {
	testcases := []struct {
		name          string
		action        v1alpha1.Action
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "RestartTaskAction: uses restartingUpdateStatus",
			action:        v1alpha1.RestartTaskAction,
			expectedPhase: vcbatch.Pending,
		},
		{
			name:          "RestartPodAction: uses restartingUpdateStatus",
			action:        v1alpha1.RestartPodAction,
			expectedPhase: vcbatch.Pending,
		},
		{
			name:          "RestartPartitionAction: uses restartingUpdateStatus",
			action:        v1alpha1.RestartPartitionAction,
			expectedPhase: vcbatch.Pending,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Restarting)
			jobInfo.Job.Spec.MaxRetry = 5
			jobInfo.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}}

			origKillTarget := KillTarget
			t.Cleanup(func() { KillTarget = origKillTarget })

			var capturedPhase vcbatch.JobPhase
			KillTarget = func(job *apis.JobInfo, target Target, fn UpdateStatusFn) error {
				// total=3, Terminating=0 → 3-0=3 >= MinAvailable=2 → Pending
				status := &vcbatch.JobStatus{
					RetryCount:   1,
					Terminating:  0,
					MinAvailable: 2,
				}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &restartingState{job: jobInfo}
			if err := s.Execute(Action{Action: tc.action}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestRestartingState_DefaultAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "Default action: KillJob with PodRetainPhaseNone and restartingUpdateStatus",
			expectedPhase: vcbatch.Pending,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Restarting)
			jobInfo.Job.Spec.MaxRetry = 5
			jobInfo.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}}

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedRetainPhase PhaseMap
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				capturedRetainPhase = podRetainPhase
				// total=3, Terminating=0 → 3-0=3 >= MinAvailable=2 → Pending
				status := &vcbatch.JobStatus{
					RetryCount:   1,
					Terminating:  0,
					MinAvailable: 2,
				}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &restartingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.AbortJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
			if len(capturedRetainPhase) != 0 {
				t.Errorf("expected PodRetainPhaseNone (empty), got %v", capturedRetainPhase)
			}
		})
	}
}
