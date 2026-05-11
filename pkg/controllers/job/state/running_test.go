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
	v1 "k8s.io/api/core/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
)

func TestRunningState_Execute_RestartJob(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Running)
	job.Job.Status.RetryCount = 2

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.RestartJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(m.KillJobCalls) != 1 {
		t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
	}
	if len(m.KillJobCalls[0].RetainPhase) != 0 {
		t.Errorf("RetainPhase should be PodRetainPhaseNone, got %v", m.KillJobCalls[0].RetainPhase)
	}
	if job.Job.Status.State.Phase != vcbatch.Restarting {
		t.Errorf("Phase = %q, want Restarting", job.Job.Status.State.Phase)
	}
	if job.Job.Status.RetryCount != 3 {
		t.Errorf("RetryCount = %d, want 3", job.Job.Status.RetryCount)
	}
}

func TestRunningState_Execute_RestartTargets(t *testing.T) {
	targetActions := []busv1alpha1.Action{
		busv1alpha1.RestartTaskAction,
		busv1alpha1.RestartPodAction,
		busv1alpha1.RestartPartitionAction,
	}
	for _, action := range targetActions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Running)
			target := Target{TaskName: "tx", Type: TargetTypeTask}

			st := NewState(job)
			if err := st.Execute(Action{Action: action, Target: target}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if len(m.KillTargetCalls) != 1 {
				t.Errorf("KillTarget calls = %d, want 1", len(m.KillTargetCalls))
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

func TestRunningState_Execute_AbortCompleteTerminate(t *testing.T) {
	tests := []struct {
		action    busv1alpha1.Action
		wantPhase vcbatch.JobPhase
	}{
		{busv1alpha1.AbortJobAction, vcbatch.Aborting},
		{busv1alpha1.TerminateJobAction, vcbatch.Terminating},
		{busv1alpha1.CompleteJobAction, vcbatch.Completing},
	}
	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			job := makeJob(vcbatch.Running)
			st := NewState(job)
			if err := st.Execute(Action{Action: tt.action}); err != nil {
				t.Fatalf("Execute err: %v", err)
			}
			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			if _, ok := m.KillJobCalls[0].RetainPhase[v1.PodSucceeded]; !ok {
				t.Errorf("RetainPhase should be Soft")
			}
			if job.Job.Status.State.Phase != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", job.Job.Status.State.Phase, tt.wantPhase)
			}
		})
	}
}

func TestRunningState_Execute_ZeroReplicasKeepsRunning(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Running)
	// no tasks -> TotalTasks == 0
	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if len(m.SyncCalls) != 1 {
		t.Fatalf("SyncJob calls = %d, want 1", len(m.SyncCalls))
	}
	if m.SyncCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = true, want false (zero replicas should keep phase)")
	}
	if job.Job.Status.State.Phase != vcbatch.Running {
		t.Errorf("Phase = %q, want unchanged Running", job.Job.Status.State.Phase)
	}
}

func TestRunningState_Execute_MinSuccessReachedEarly(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	jobCompletedPhaseCount.Reset()

	min := int32(2)
	job := makeJob(vcbatch.Running)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MinSuccess = &min
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 5}}
	job.Job.Status.Succeeded = 3

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !m.SyncCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Completed {
		t.Errorf("Phase = %q, want Completed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("completed metric = %v, want 1", got)
	}
}

func TestRunningState_Execute_AllPodsDone_CompletedBySpecMinAvailable(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	jobCompletedPhaseCount.Reset()

	job := makeJob(vcbatch.Running)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MinAvailable = 2
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 3}}
	job.Job.Status.Succeeded = 2
	job.Job.Status.Failed = 1

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !m.SyncCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Completed {
		t.Errorf("Phase = %q, want Completed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("completed metric = %v, want 1", got)
	}
}

func TestRunningState_Execute_AllPodsDone_FailedBecauseUnderMinAvailable(t *testing.T) {
	_, cleanup := newMock()
	defer cleanup()
	jobFailedPhaseCount.Reset()

	job := makeJob(vcbatch.Running)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MinAvailable = 3
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 3}}
	job.Job.Status.Succeeded = 1
	job.Job.Status.Failed = 2

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if job.Job.Status.State.Phase != vcbatch.Failed {
		t.Errorf("Phase = %q, want Failed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("failed metric = %v, want 1", got)
	}
}

func TestRunningState_Execute_AllPodsDone_FailedBecauseTaskMinAvailableUnmet(t *testing.T) {
	_, cleanup := newMock()
	defer cleanup()
	jobFailedPhaseCount.Reset()

	taskMin := int32(2)
	job := makeJob(vcbatch.Running)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MinAvailable = 3
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{
		{Name: "t1", Replicas: 3, MinAvailable: &taskMin},
	}
	job.Job.Status.Succeeded = 1
	job.Job.Status.Failed = 2
	job.Job.Status.TaskStatusCount = map[string]vcbatch.TaskState{
		"t1": {Phase: map[v1.PodPhase]int32{v1.PodSucceeded: 1, v1.PodFailed: 2}},
	}

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if job.Job.Status.State.Phase != vcbatch.Failed {
		t.Errorf("Phase = %q, want Failed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("failed metric = %v, want 1", got)
	}
}

func TestRunningState_Execute_AllPodsDone_FailedBecauseUnderMinSuccess(t *testing.T) {
	_, cleanup := newMock()
	defer cleanup()
	jobFailedPhaseCount.Reset()

	minSuccess := int32(3)
	job := makeJob(vcbatch.Running)
	job.Job.Namespace = "ns"
	job.Job.Name = "j1"
	job.Job.Spec.Queue = "q1"
	job.Job.Spec.MinAvailable = 1
	job.Job.Spec.MinSuccess = &minSuccess
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 3}}
	job.Job.Status.Succeeded = 2
	job.Job.Status.Failed = 1

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if job.Job.Status.State.Phase != vcbatch.Failed {
		t.Errorf("Phase = %q, want Failed", job.Job.Status.State.Phase)
	}
	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/j1", "q1")); got != 1 {
		t.Errorf("failed metric = %v, want 1", got)
	}
}

func TestRunningState_Execute_BackToPendingWhenPendingExceedsThreshold(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Running)
	job.Job.Spec.MinAvailable = 3
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 5}}
	// jobReplicas - MinAvailable = 5 - 3 = 2; Pending(3) > 2 => Pending
	job.Job.Status.Pending = 3

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if !m.SyncCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = false, want true")
	}
	if job.Job.Status.State.Phase != vcbatch.Pending {
		t.Errorf("Phase = %q, want Pending", job.Job.Status.State.Phase)
	}
}

func TestRunningState_Execute_KeepsRunningWhenInFlight(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()

	job := makeJob(vcbatch.Running)
	job.Job.Spec.MinAvailable = 3
	job.Job.Spec.Tasks = []vcbatch.TaskSpec{{Name: "t1", Replicas: 5}}
	// no terminal condition met, pending threshold not crossed
	job.Job.Status.Running = 3
	job.Job.Status.Pending = 2 // 2 <= 5-3 = 2

	st := NewState(job)
	if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if m.SyncCalls[0].PhaseChanged {
		t.Errorf("PhaseChanged = true, want false")
	}
	if job.Job.Status.State.Phase != vcbatch.Running {
		t.Errorf("Phase = %q, want unchanged Running", job.Job.Status.State.Phase)
	}
}

func TestRunningState_Execute_ErrorPropagation(t *testing.T) {
	cases := []struct {
		name   string
		action busv1alpha1.Action
		setup  func(*mockOps)
	}{
		{"kill job err", busv1alpha1.RestartJobAction, func(m *mockOps) { m.KillJobErr = errors.New("e") }},
		{"kill target err", busv1alpha1.RestartTaskAction, func(m *mockOps) { m.KillTargetErr = errors.New("e") }},
		{"sync err (default)", busv1alpha1.SyncJobAction, func(m *mockOps) { m.SyncErr = errors.New("e") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()
			c.setup(m)

			st := NewState(makeJob(vcbatch.Running))
			if err := st.Execute(Action{Action: c.action}); err == nil || err.Error() != "e" {
				t.Errorf("err = %v, want \"e\"", err)
			}
		})
	}
}
