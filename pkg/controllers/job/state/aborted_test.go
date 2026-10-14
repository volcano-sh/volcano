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
	v1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestAbortedState_ResumeJobAction(t *testing.T) {
	origKillJob := KillJob
	t.Cleanup(func() { KillJob = origKillJob })

	testcases := []struct {
		name          string
		action        Action
		initialStatus vcbatch.JobStatus
		expectedPhase vcbatch.JobPhase
		expectedRetry int32
		expectChanged bool
	}{
		{
			name: "ResumeJobAction: transitions to Restarting",
			action: Action{
				Action: v1alpha1.ResumeJobAction,
			},
			initialStatus: vcbatch.JobStatus{
				RetryCount: 2,
			},
			expectedPhase: vcbatch.Restarting,
			expectedRetry: 3,
			expectChanged: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Aborted)
			state := &abortedState{job: jobInfo}

			var capturedStatus vcbatch.JobStatus
			var changed bool

			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := tc.initialStatus
				if fn != nil {
					changed = fn(&status)
					capturedStatus = status
				}
				return nil
			}

			err := state.Execute(tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if changed != tc.expectChanged {
				t.Errorf("expected changed %v, got %v", tc.expectChanged, changed)
			}
			if capturedStatus.State.Phase != tc.expectedPhase {
				t.Errorf("expected phase %v, got %v", tc.expectedPhase, capturedStatus.State.Phase)
			}
			if capturedStatus.RetryCount != tc.expectedRetry {
				t.Errorf("expected RetryCount %d, got %d", tc.expectedRetry, capturedStatus.RetryCount)
			}
		})
	}
}

func TestAbortedState_DefaultAction(t *testing.T) {
	origKillJob := KillJob
	t.Cleanup(func() { KillJob = origKillJob })

	testcases := []struct {
		name   string
		action Action
	}{
		{
			name: "Default action: calls KillJob with nil fn",
			action: Action{
				Action: v1alpha1.SyncJobAction,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Aborted)
			state := &abortedState{job: jobInfo}

			var killJobCalled bool
			var capturedFn UpdateStatusFn

			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				killJobCalled = true
				capturedFn = fn
				return nil
			}

			err := state.Execute(tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !killJobCalled {
				t.Errorf("expected KillJob to be called")
			}
			if capturedFn != nil {
				t.Errorf("expected fn to be nil, got %v", capturedFn)
			}
		})
	}
}
