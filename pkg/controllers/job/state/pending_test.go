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

func TestPendingState_RestartJobAction(t *testing.T) {
	testcases := []struct {
		name               string
		expectedPhase      vcbatch.JobPhase
		expectedRetryDelta int32
	}{
		{
			name:               "RestartJobAction: transitions to Restarting, increments RetryCount",
			expectedPhase:      vcbatch.Restarting,
			expectedRetryDelta: 1,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Pending)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedRetryCount int32
			var capturedRetainPhase PhaseMap
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				capturedRetainPhase = podRetainPhase
				status := &vcbatch.JobStatus{RetryCount: 0}
				fn(status)
				capturedPhase = status.State.Phase
				capturedRetryCount = status.RetryCount
				return nil
			}

			s := &pendingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.RestartJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
			if capturedRetryCount != tc.expectedRetryDelta {
				t.Errorf("expected RetryCount %d, got %d", tc.expectedRetryDelta, capturedRetryCount)
			}
			if len(capturedRetainPhase) != 0 {
				t.Errorf("expected PodRetainPhaseNone (empty), got %v", capturedRetainPhase)
			}
		})
	}
}

func TestPendingState_RestartTaskAction(t *testing.T) {
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
			jobInfo := newTestJobInfo(vcbatch.Pending)

			origKillTarget := KillTarget
			t.Cleanup(func() { KillTarget = origKillTarget })

			var capturedPhase vcbatch.JobPhase
			KillTarget = func(job *apis.JobInfo, target Target, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &pendingState{job: jobInfo}
			if err := s.Execute(Action{Action: tc.action}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestPendingState_AbortJobAction(t *testing.T) {
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
			jobInfo := newTestJobInfo(vcbatch.Pending)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedRetainPhase PhaseMap
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				capturedRetainPhase = podRetainPhase
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &pendingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.AbortJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
			if len(capturedRetainPhase) == 0 {
				t.Errorf("expected PodRetainPhaseSoft (non-empty), got empty map")
			}
		})
	}
}

func TestPendingState_CompleteJobAction(t *testing.T) {
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
			jobInfo := newTestJobInfo(vcbatch.Pending)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &pendingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.CompleteJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestPendingState_TerminateJobAction(t *testing.T) {
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
			jobInfo := newTestJobInfo(vcbatch.Pending)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{}
				fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &pendingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.TerminateJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
		})
	}
}

func TestPendingState_SyncJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		minAvailable  int32
		running       int32
		succeeded     int32
		failed        int32
		expectedPhase vcbatch.JobPhase
		expectChanged bool
	}{
		{
			name:          "MinAvailable met: transitions to Running",
			minAvailable:  2,
			running:       2,
			succeeded:     0,
			failed:        0,
			expectedPhase: vcbatch.Running,
			expectChanged: true,
		},
		{
			name:          "MinAvailable met with mix of pods: transitions to Running",
			minAvailable:  3,
			running:       1,
			succeeded:     1,
			failed:        1,
			expectedPhase: vcbatch.Running,
			expectChanged: true,
		},
		{
			name:          "MinAvailable not met: no phase change",
			minAvailable:  5,
			running:       2,
			succeeded:     0,
			failed:        0,
			expectedPhase: "",
			expectChanged: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Pending)
			jobInfo.Job.Spec.MinAvailable = tc.minAvailable

			origSyncJob := SyncJob
			t.Cleanup(func() { SyncJob = origSyncJob })

			var capturedPhase vcbatch.JobPhase
			var capturedChanged bool
			SyncJob = func(job *apis.JobInfo, fn UpdateStatusFn) error {
				status := &vcbatch.JobStatus{
					Running:   tc.running,
					Succeeded: tc.succeeded,
					Failed:    tc.failed,
				}
				capturedChanged = fn(status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &pendingState{job: jobInfo}
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
