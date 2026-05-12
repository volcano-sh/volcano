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

func TestAbortedState_Execute_ResumeTransitionsToRestarting(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Aborted)
	job.Job.Status.RetryCount = 2

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
	if job.Job.Status.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3 (incremented from 2)", job.Job.Status.RetryCount)
	}
}

func TestAbortedState_Execute_OtherActionsKeepAborted(t *testing.T) {
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

			job := makeJob(vcbatch.Aborted)
			st := NewState(job)
			if err := st.Execute(Action{Action: action}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			call := m.KillJobCalls[0]
			if call.HadStatusFn {
				t.Errorf("non-Resume action should pass nil UpdateStatusFn")
			}
			if job.Job.Status.State.Phase != vcbatch.Aborted {
				t.Errorf("Phase = %q, want unchanged (Aborted)", job.Job.Status.State.Phase)
			}
		})
	}
}

func TestAbortedState_Execute_PropagatesError(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	m.KillJobErr = errors.New("kill failed")

	st := NewState(makeJob(vcbatch.Aborted))
	if err := st.Execute(Action{Action: busv1alpha1.ResumeJobAction}); err == nil || err.Error() != "kill failed" {
		t.Errorf("err = %v, want \"kill failed\"", err)
	}
}
