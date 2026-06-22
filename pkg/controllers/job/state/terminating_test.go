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

func TestTerminatingState_Execute(t *testing.T) {
	tests := []struct {
		name          string
		initialStatus vcbatch.JobStatus
		wantPhase     vcbatch.JobPhase
		wantChanged   bool
	}{
		{
			name: "all pods drained -> Terminated",
			initialStatus: vcbatch.JobStatus{
				State: vcbatch.JobState{Phase: vcbatch.Terminating},
			},
			wantPhase:   vcbatch.Terminated,
			wantChanged: true,
		},
		{
			name: "still has Running pods -> stays Terminating",
			initialStatus: vcbatch.JobStatus{
				Running: 2,
				State:   vcbatch.JobState{Phase: vcbatch.Terminating},
			},
			wantPhase:   vcbatch.Terminating,
			wantChanged: false,
		},
		{
			name: "still has Pending pods -> stays Terminating",
			initialStatus: vcbatch.JobStatus{
				Pending: 1,
				State:   vcbatch.JobState{Phase: vcbatch.Terminating},
			},
			wantPhase:   vcbatch.Terminating,
			wantChanged: false,
		},
		{
			name: "still has Terminating pods -> stays Terminating",
			initialStatus: vcbatch.JobStatus{
				Terminating: 3,
				State:       vcbatch.JobState{Phase: vcbatch.Terminating},
			},
			wantPhase:   vcbatch.Terminating,
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Terminating)
			job.Job.Status = tt.initialStatus

			st := NewState(job)
			if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			call := m.KillJobCalls[0]
			if _, ok := call.RetainPhase[v1.PodSucceeded]; !ok {
				t.Errorf("RetainPhase missing PodSucceeded")
			}
			if _, ok := call.RetainPhase[v1.PodFailed]; !ok {
				t.Errorf("RetainPhase missing PodFailed")
			}
			if !call.HadStatusFn {
				t.Errorf("UpdateStatusFn should be non-nil (drain logic lives in it)")
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

func TestTerminatingState_Execute_ActionIsIgnored(t *testing.T) {
	actions := []busv1alpha1.Action{
		busv1alpha1.SyncJobAction,
		busv1alpha1.ResumeJobAction,
		busv1alpha1.RestartJobAction,
		busv1alpha1.AbortJobAction,
	}
	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			st := NewState(makeJob(vcbatch.Terminating))
			if err := st.Execute(Action{Action: action}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if len(m.KillJobCalls) != 1 {
				t.Errorf("KillJob calls = %d, want 1 for %q", len(m.KillJobCalls), action)
			}
		})
	}
}

func TestTerminatingState_Execute_PropagatesError(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	m.KillJobErr = errors.New("kill failed")

	st := NewState(makeJob(vcbatch.Terminating))
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err == nil || err.Error() != "kill failed" {
		t.Errorf("err = %v, want \"kill failed\"", err)
	}
}
