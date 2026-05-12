/*
Copyright 2024 The Volcano Authors.

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
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
)

func TestAbortingState_Execute_ResumeTransitionsToRestarting(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Aborting)
	job.Job.Status.RetryCount = 1

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.ResumeJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	if len(m.KillJobCalls) != 1 {
		t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
	}
	call := m.KillJobCalls[0]
	if _, ok := call.RetainPhase[v1.PodSucceeded]; !ok {
		t.Errorf("RetainPhase should be PodRetainPhaseSoft")
	}
	if !call.PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Restarting {
		t.Errorf("Phase = %q, want Restarting", job.Job.Status.State.Phase)
	}
	if job.Job.Status.RetryCount != 2 {
		t.Errorf("RetryCount = %d, want 2", job.Job.Status.RetryCount)
	}
}

func TestAbortingState_Execute_DefaultDrainTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus vcbatch.JobStatus
		wantPhase     vcbatch.JobPhase
		wantChanged   bool
	}{
		{
			name: "all pods drained -> Aborted",
			initialStatus: vcbatch.JobStatus{
				State: vcbatch.JobState{Phase: vcbatch.Aborting},
			},
			wantPhase:   vcbatch.Aborted,
			wantChanged: true,
		},
		{
			name: "Terminating != 0 -> stays Aborting",
			initialStatus: vcbatch.JobStatus{
				Terminating: 1,
				State:       vcbatch.JobState{Phase: vcbatch.Aborting},
			},
			wantPhase:   vcbatch.Aborting,
			wantChanged: false,
		},
		{
			name: "Pending != 0 -> stays Aborting",
			initialStatus: vcbatch.JobStatus{
				Pending: 1,
				State:   vcbatch.JobState{Phase: vcbatch.Aborting},
			},
			wantPhase:   vcbatch.Aborting,
			wantChanged: false,
		},
		{
			name: "Running != 0 -> stays Aborting",
			initialStatus: vcbatch.JobStatus{
				Running: 2,
				State:   vcbatch.JobState{Phase: vcbatch.Aborting},
			},
			wantPhase:   vcbatch.Aborting,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Aborting)
			job.Job.Status = tt.initialStatus

			st := NewState(job)
			if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			call := m.KillJobCalls[0]
			if !call.HadStatusFn {
				t.Errorf("default branch should pass non-nil UpdateStatusFn for drain logic")
			}
			if call.PhaseChanged != tt.wantChanged {
				t.Errorf("PhaseChanged = %v, want %v", call.PhaseChanged, tt.wantChanged)
			}
			if job.Job.Status.State.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", job.Job.Status.State.Phase, tt.wantPhase)
			}
		})
	}
}

func TestAbortingState_Execute_NonResumeActionsAllUseDefault(t *testing.T) {
	actions := []busv1alpha1.Action{
		busv1alpha1.SyncJobAction,
		busv1alpha1.RestartJobAction,
		busv1alpha1.AbortJobAction,
		busv1alpha1.CompleteJobAction,
		busv1alpha1.TerminateJobAction,
	}
	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Aborting)
			st := NewState(job)
			if err := st.Execute(Action{Action: action}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			if job.Job.Status.RetryCount != 0 {
				t.Errorf("RetryCount = %d, want 0 (default branch should not increment)", job.Job.Status.RetryCount)
			}
		})
	}
}

func TestAbortingState_Execute_PropagatesError(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	m.KillJobErr = errors.New("kill failed")

	st := NewState(makeJob(vcbatch.Aborting))
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err == nil || err.Error() != "kill failed" {
		t.Errorf("err = %v, want \"kill failed\"", err)
	}
}
