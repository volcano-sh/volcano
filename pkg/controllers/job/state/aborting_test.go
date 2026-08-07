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

func TestAbortingState_ResumeJobAction(t *testing.T) {
	testcases := []struct {
		name          string
		expectedPhase vcbatch.JobPhase
	}{
		{
			name:          "ResumeJobAction: transitions to Restarting",
			expectedPhase: vcbatch.Restarting,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Aborting)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedRetryCount int32
			var capturedRetainPhase PhaseMap
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				capturedRetainPhase = podRetainPhase
				status := &vcbatch.JobStatus{RetryCount: 1}
				fn(status)
				capturedPhase = status.State.Phase
				capturedRetryCount = status.RetryCount
				return nil
			}

			s := &abortingState{job: jobInfo}
			if err := s.Execute(Action{Action: v1alpha1.ResumeJobAction}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %s, got %s", tc.expectedPhase, capturedPhase)
			}
			if capturedRetryCount != 2 {
				t.Errorf("expected RetryCount 2, got %d", capturedRetryCount)
			}
			if len(capturedRetainPhase) == 0 {
				t.Errorf("expected PodRetainPhaseSoft (non-empty), got empty map")
			}
		})
	}
}

func TestAbortingState_DefaultAction(t *testing.T) {
	testcases := []struct {
		name          string
		status        vcbatch.JobStatus
		expectedPhase vcbatch.JobPhase
		expectChanged bool
	}{
		{
			name:          "Has Terminating pods: stays Aborting",
			status:        vcbatch.JobStatus{Terminating: 2},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name:          "Has Pending pods: stays Aborting",
			status:        vcbatch.JobStatus{Pending: 1},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name:          "Has Running pods: stays Aborting",
			status:        vcbatch.JobStatus{Running: 3},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name:          "All pods done: transitions to Aborted",
			status:        vcbatch.JobStatus{Terminating: 0, Pending: 0, Running: 0},
			expectedPhase: vcbatch.Aborted,
			expectChanged: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Aborting)

			origKillJob := KillJob
			t.Cleanup(func() { KillJob = origKillJob })

			var capturedPhase vcbatch.JobPhase
			var capturedChanged bool
			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := tc.status
				capturedChanged = fn(&status)
				capturedPhase = status.State.Phase
				return nil
			}

			s := &abortingState{job: jobInfo}
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
