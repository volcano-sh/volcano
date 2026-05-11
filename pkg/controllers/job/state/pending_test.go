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

func TestPendingState_Execute_RestartJob(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Pending)
	job.Job.Status.RetryCount = 4

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	if len(m.KillJobCalls) != 1 {
		t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
	}
	call := m.KillJobCalls[0]
	if len(call.RetainPhase) != 0 {
		t.Errorf("RetainPhase should be PodRetainPhaseNone (empty), got %v", call.RetainPhase)
	}
	if job.Job.Status.State.Phase != vcbatch.Restarting {
		t.Errorf("Phase = %q, want Restarting", job.Job.Status.State.Phase)
	}
	if job.Job.Status.RetryCount != 5 {
		t.Errorf("RetryCount = %d, want 5", job.Job.Status.RetryCount)
	}
}

func TestPendingState_Execute_RestartTargetActions(t *testing.T) {
	targetActions := []busv1alpha1.Action{
		busv1alpha1.RestartTaskAction,
		busv1alpha1.RestartPodAction,
		busv1alpha1.RestartPartitionAction,
	}
	for _, action := range targetActions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Pending)
			job.Job.Status.RetryCount = 0
			target := Target{TaskName: "task-x", Type: TargetTypeTask}

			st := NewState(job)
			if err := st.Execute(Action{Action: action, Target: target}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillTargetCalls) != 1 {
				t.Fatalf("KillTarget calls = %d, want 1", len(m.KillTargetCalls))
			}
			call := m.KillTargetCalls[0]
			if call.Target != target {
				t.Errorf("Target = %+v, want %+v", call.Target, target)
			}
			if job.Job.Status.State.Phase != vcbatch.Restarting {
				t.Errorf("Phase = %q, want Restarting", job.Job.Status.State.Phase)
			}
			if job.Job.Status.RetryCount != 1 {
				t.Errorf("RetryCount = %d, want 1", job.Job.Status.RetryCount)
			}
		})
	}
}

func TestPendingState_Execute_AbortCompleteTerminate(t *testing.T) {
	tests := []struct {
		action    busv1alpha1.Action
		wantPhase vcbatch.JobPhase
	}{
		{busv1alpha1.AbortJobAction, vcbatch.Aborting},
		{busv1alpha1.CompleteJobAction, vcbatch.Completing},
		{busv1alpha1.TerminateJobAction, vcbatch.Terminating},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Pending)
			job.Job.Status.RetryCount = 7

			st := NewState(job)
			if err := st.Execute(Action{Action: tt.action}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			call := m.KillJobCalls[0]
			if _, ok := call.RetainPhase[v1.PodSucceeded]; !ok {
				t.Errorf("RetainPhase should retain PodSucceeded")
			}
			if job.Job.Status.State.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", job.Job.Status.State.Phase, tt.wantPhase)
			}
			if job.Job.Status.RetryCount != 7 {
				t.Errorf("RetryCount = %d, want 7 (no inc on abort/complete/terminate)", job.Job.Status.RetryCount)
			}
		})
	}
}

func TestPendingState_Execute_SyncDefault(t *testing.T) {
	tests := []struct {
		name          string
		spec          vcbatch.JobSpec
		initialStatus vcbatch.JobStatus
		wantPhase     vcbatch.JobPhase
		wantChanged   bool
	}{
		{
			name: "enough pods alive -> transition to Running",
			spec: vcbatch.JobSpec{MinAvailable: 3},
			initialStatus: vcbatch.JobStatus{
				Running:   2,
				Succeeded: 1,
				State:     vcbatch.JobState{Phase: vcbatch.Pending},
			},
			wantPhase:   vcbatch.Running,
			wantChanged: true,
		},
		{
			name: "enough via failed pods too -> transition to Running",
			spec: vcbatch.JobSpec{MinAvailable: 3},
			initialStatus: vcbatch.JobStatus{
				Running:   1,
				Succeeded: 1,
				Failed:    1,
				State:     vcbatch.JobState{Phase: vcbatch.Pending},
			},
			wantPhase:   vcbatch.Running,
			wantChanged: true,
		},
		{
			name: "not enough pods alive -> stays Pending",
			spec: vcbatch.JobSpec{MinAvailable: 3},
			initialStatus: vcbatch.JobStatus{
				Running: 2,
				State:   vcbatch.JobState{Phase: vcbatch.Pending},
			},
			wantPhase:   vcbatch.Pending,
			wantChanged: false,
		},
		{
			name: "MinAvailable 0 with no pods -> transitions to Running (0 <= 0)",
			spec: vcbatch.JobSpec{MinAvailable: 0},
			initialStatus: vcbatch.JobStatus{
				State: vcbatch.JobState{Phase: vcbatch.Pending},
			},
			wantPhase:   vcbatch.Running,
			wantChanged: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Pending)
			job.Job.Spec = tt.spec
			job.Job.Status = tt.initialStatus

			st := NewState(job)
			if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}

			if len(m.SyncCalls) != 1 {
				t.Fatalf("SyncJob calls = %d, want 1", len(m.SyncCalls))
			}
			call := m.SyncCalls[0]
			if call.PhaseChanged != tt.wantChanged {
				t.Errorf("PhaseChanged = %v, want %v", call.PhaseChanged, tt.wantChanged)
			}
			if job.Job.Status.State.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", job.Job.Status.State.Phase, tt.wantPhase)
			}
		})
	}
}

func TestPendingState_Execute_UnknownActionFallsThroughToSync(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Pending)
	job.Job.Spec.MinAvailable = 1
	job.Job.Status.Running = 1

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.EnqueueAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	if len(m.SyncCalls) != 1 {
		t.Errorf("SyncJob calls = %d, want 1 (Enqueue should hit default)", len(m.SyncCalls))
	}
	if len(m.KillJobCalls) != 0 {
		t.Errorf("KillJob calls = %d, want 0", len(m.KillJobCalls))
	}
}

func TestPendingState_Execute_ErrorPropagation(t *testing.T) {
	t.Run("from KillJob (RestartJobAction)", func(t *testing.T) {
		m, cleanup := newMock()
		defer cleanup()
		m.KillJobErr = errors.New("kj")

		st := NewState(makeJob(vcbatch.Pending))
		if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err == nil || err.Error() != "kj" {
			t.Errorf("err = %v, want \"kj\"", err)
		}
	})
	t.Run("from KillTarget", func(t *testing.T) {
		m, cleanup := newMock()
		defer cleanup()
		m.KillTargetErr = errors.New("kt")

		st := NewState(makeJob(vcbatch.Pending))
		if err := st.Execute(Action{Action: busv1alpha1.RestartTaskAction}); err == nil || err.Error() != "kt" {
			t.Errorf("err = %v, want \"kt\"", err)
		}
	})
	t.Run("from SyncJob (default)", func(t *testing.T) {
		m, cleanup := newMock()
		defer cleanup()
		m.SyncErr = errors.New("sj")

		st := NewState(makeJob(vcbatch.Pending))
		if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err == nil || err.Error() != "sj" {
			t.Errorf("err = %v, want \"sj\"", err)
		}
	})
}
