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

	"github.com/prometheus/client_golang/prometheus/testutil"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
)

func TestRestartingState_Execute_MaxRetryExceeded_FailsAndEmitsMetric(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	jobFailedPhaseCount.Reset()

	job := makeJob(vcbatch.Restarting)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MaxRetry = 3
	job.Job.Status.RetryCount = 3 // equal => exceed

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}

	if len(m.KillJobCalls) != 1 {
		t.Fatalf("KillJob calls = %d, want 1 (default branch)", len(m.KillJobCalls))
	}
	if !m.KillJobCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Failed {
		t.Errorf("Phase = %q, want Failed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("failed metric = %v, want 1", got)
	}
}

func TestRestartingState_Execute_RetryCountStrictlyGreaterAlsoFails(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	jobFailedPhaseCount.Reset()

	job := makeJob(vcbatch.Restarting)
	job.Job.Spec.MaxRetry = 2
	job.Job.Status.RetryCount = 5

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !m.KillJobCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Failed {
		t.Errorf("Phase = %q, want Failed", job.Job.Status.State.Phase)
	}
}

func TestRestartingState_Execute_TransitionsToPendingWhenDrained(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Restarting)
	job.Job.Spec.MaxRetry = 5
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{
		{Name: "t1", Replicas: 2},
		{Name: "t2", Replicas: 1},
	}
	job.Job.Status.RetryCount = 1
	job.Job.Status.MinAvailable = 2
	job.Job.Status.Terminating = 1 // total(3) - terminating(1) = 2 >= minAvailable(2)

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	call := m.KillJobCalls[0]
	if !call.PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Pending {
		t.Errorf("Phase = %q, want Pending", job.Job.Status.State.Phase)
	}
}

func TestRestartingState_Execute_StaysRestartingWhenNotDrained(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Restarting)
	job.Job.Spec.MaxRetry = 5
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{
		{Name: "t1", Replicas: 3},
	}
	job.Job.Status.RetryCount = 1
	job.Job.Status.MinAvailable = 2
	job.Job.Status.Terminating = 2 // 3-2 = 1 < 2 => not yet

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	call := m.KillJobCalls[0]
	if call.PhaseChanged {
		t.Errorf("PhaseChanged = true, want false")
	}
	if job.Job.Status.State.Phase != vcbatch.Restarting {
		t.Errorf("Phase = %q, want unchanged Restarting", job.Job.Status.State.Phase)
	}
}

func TestRestartingState_Execute_SyncJobActionRoutesToSyncJob(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Restarting)
	job.Job.Spec.MaxRetry = 5
	job.Job.Status.RetryCount = 1
	job.Job.Status.MinAvailable = 0
	// total = 0, terminating = 0 -> 0 >= 0 -> Pending

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(m.SyncCalls) != 1 {
		t.Errorf("SyncJob calls = %d, want 1", len(m.SyncCalls))
	}
	if len(m.KillJobCalls) != 0 {
		t.Errorf("KillJob calls = %d, want 0", len(m.KillJobCalls))
	}
	if job.Job.Status.State.Phase != vcbatch.Pending {
		t.Errorf("Phase = %q, want Pending", job.Job.Status.State.Phase)
	}
}

func TestRestartingState_Execute_RestartTargetActionsRouteToKillTarget(t *testing.T) {
	targetActions := []busv1alpha1.Action{
		busv1alpha1.RestartTaskAction,
		busv1alpha1.RestartPodAction,
		busv1alpha1.RestartPartitionAction,
	}
	for _, action := range targetActions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Restarting)
			job.Job.Spec.MaxRetry = 5
			job.Job.Status.RetryCount = 0
			target := Target{TaskName: "tx", Type: TargetTypeTask}

			st := NewState(job)
			if err := st.Execute(Action{Action: action, Target: target}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if len(m.KillTargetCalls) != 1 {
				t.Errorf("KillTarget calls = %d, want 1", len(m.KillTargetCalls))
			}
			if m.KillTargetCalls[0].Target != target {
				t.Errorf("Target = %+v, want %+v", m.KillTargetCalls[0].Target, target)
			}
			if !m.KillTargetCalls[0].HadStatusFn {
				t.Errorf("restartingUpdateStatus should be passed as UpdateStatusFn")
			}
		})
	}
}

func TestRestartingState_Execute_DefaultUsesPodRetainPhaseNone(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Restarting)
	job.Job.Spec.MaxRetry = 5

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.AbortJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(m.KillJobCalls) != 1 {
		t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
	}
	if len(m.KillJobCalls[0].RetainPhase) != 0 {
		t.Errorf("RetainPhase should be empty (PodRetainPhaseNone), got %v", m.KillJobCalls[0].RetainPhase)
	}
}

func TestRestartingState_Execute_ErrorPropagation(t *testing.T) {
	cases := []struct {
		name   string
		action busv1alpha1.Action
		setup  func(*mockOps)
	}{
		{"sync err", busv1alpha1.SyncJobAction, func(m *mockOps) { m.SyncErr = errors.New("e") }},
		{"kill target err", busv1alpha1.RestartTaskAction, func(m *mockOps) { m.KillTargetErr = errors.New("e") }},
		{"kill job err (default)", busv1alpha1.RestartJobAction, func(m *mockOps) { m.KillJobErr = errors.New("e") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()
			c.setup(m)

			st := NewState(makeJob(vcbatch.Restarting))
			if err := st.Execute(Action{Action: c.action}); err == nil || err.Error() != "e" {
				t.Errorf("err = %v, want \"e\"", err)
			}
		})
	}
}
