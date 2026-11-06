/*
Copyright 2026 The Volcano Authors.

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
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

// call records how a state handed work over to the job controller.
type call struct {
	kind   string // "sync", "kill" or "killTarget"
	retain PhaseMap
	target Target
	fn     UpdateStatusFn
}

// fakeController replaces the package level SyncJob/KillJob/KillTarget hooks,
// which are normally wired to the job controller, and records every call.
// The originals are restored when the test finishes.
func fakeController(t *testing.T) *[]call {
	t.Helper()

	origSync, origKill, origKillTarget := SyncJob, KillJob, KillTarget
	t.Cleanup(func() { SyncJob, KillJob, KillTarget = origSync, origKill, origKillTarget })

	calls := &[]call{}
	SyncJob = func(_ *apis.JobInfo, fn UpdateStatusFn) error {
		*calls = append(*calls, call{kind: "sync", fn: fn})
		return nil
	}
	KillJob = func(_ *apis.JobInfo, retain PhaseMap, fn UpdateStatusFn) error {
		*calls = append(*calls, call{kind: "kill", retain: retain, fn: fn})
		return nil
	}
	KillTarget = func(_ *apis.JobInfo, target Target, fn UpdateStatusFn) error {
		*calls = append(*calls, call{kind: "killTarget", target: target, fn: fn})
		return nil
	}
	return calls
}

func newJobInfo(name string, phase vcbatch.JobPhase, spec vcbatch.JobSpec) *apis.JobInfo {
	return &apis.JobInfo{
		Namespace: "ns",
		Name:      name,
		Job: &vcbatch.Job{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: name},
			Spec:       spec,
			Status:     vcbatch.JobStatus{State: vcbatch.JobState{Phase: phase}},
		},
	}
}

// executeOnce runs a single action against the state of jobInfo and returns the
// one hand-over to the controller that the state is expected to make.
func executeOnce(t *testing.T, calls *[]call, jobInfo *apis.JobInfo, act Action) call {
	t.Helper()

	*calls = nil
	require.NoError(t, NewState(jobInfo).Execute(act))
	require.Len(t, *calls, 1, "a state must hand over to the controller exactly once")
	return (*calls)[0]
}

func TestNewState(t *testing.T) {
	tests := []struct {
		name  string
		phase vcbatch.JobPhase
		want  string
	}{
		{name: "pending", phase: vcbatch.Pending, want: "*state.pendingState"},
		{name: "running", phase: vcbatch.Running, want: "*state.runningState"},
		{name: "restarting", phase: vcbatch.Restarting, want: "*state.restartingState"},
		{name: "terminated", phase: vcbatch.Terminated, want: "*state.finishedState"},
		{name: "completed", phase: vcbatch.Completed, want: "*state.finishedState"},
		{name: "failed", phase: vcbatch.Failed, want: "*state.finishedState"},
		{name: "terminating", phase: vcbatch.Terminating, want: "*state.terminatingState"},
		{name: "aborting", phase: vcbatch.Aborting, want: "*state.abortingState"},
		{name: "aborted", phase: vcbatch.Aborted, want: "*state.abortedState"},
		{name: "completing", phase: vcbatch.Completing, want: "*state.completingState"},
		{name: "empty phase defaults to pending", phase: "", want: "*state.pendingState"},
		{name: "unknown phase defaults to pending", phase: "Unknown", want: "*state.pendingState"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewState(newJobInfo("job", tt.phase, vcbatch.JobSpec{}))
			assert.Equal(t, tt.want, fmt.Sprintf("%T", got))
		})
	}
}

func TestTotalTaskMinAvailable(t *testing.T) {
	tests := []struct {
		name  string
		tasks []vcbatch.TaskSpec
		want  int32
	}{
		{name: "no tasks", want: 0},
		{
			name:  "task without minAvailable counts all replicas",
			tasks: []vcbatch.TaskSpec{{Name: "a", Replicas: 3}},
			want:  3,
		},
		{
			name:  "task minAvailable overrides replicas",
			tasks: []vcbatch.TaskSpec{{Name: "a", Replicas: 5, MinAvailable: ptr.To[int32](2)}},
			want:  2,
		},
		{
			name: "explicit zero minAvailable is honoured",
			tasks: []vcbatch.TaskSpec{
				{Name: "a", Replicas: 4, MinAvailable: ptr.To[int32](0)},
				{Name: "b", Replicas: 1},
			},
			want: 1,
		},
		{
			name: "mixed tasks are summed",
			tasks: []vcbatch.TaskSpec{
				{Name: "a", Replicas: 5, MinAvailable: ptr.To[int32](2)},
				{Name: "b", Replicas: 3},
			},
			want: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &vcbatch.Job{Spec: vcbatch.JobSpec{Tasks: tt.tasks}}
			assert.Equal(t, tt.want, TotalTaskMinAvailable(job))
		})
	}
}

// TestKillTargetActions covers the Restart{Task,Pod,Partition} actions, which
// must be routed to KillTarget (not KillJob) with the target unchanged and move
// the job to Restarting while counting a retry.
func TestKillTargetActions(t *testing.T) {
	targets := map[v1alpha1.Action]Target{
		v1alpha1.RestartTaskAction:      {Type: TargetTypeTask, TaskName: "worker"},
		v1alpha1.RestartPodAction:       {Type: TargetTypePod, PodName: "job-worker-0", PodUID: "uid-0"},
		v1alpha1.RestartPartitionAction: {Type: TargetTypePartition, TaskName: "worker", PartitionName: "p0"},
	}

	for _, phase := range []vcbatch.JobPhase{vcbatch.Pending, vcbatch.Running} {
		for act, target := range targets {
			t.Run(fmt.Sprintf("%s/%s", phase, act), func(t *testing.T) {
				calls := fakeController(t)
				got := executeOnce(t, calls, newJobInfo("job", phase, vcbatch.JobSpec{}), Action{Action: act, Target: target})

				require.Equal(t, "killTarget", got.kind)
				assert.Equal(t, target, got.target)

				status := vcbatch.JobStatus{RetryCount: 1}
				require.True(t, got.fn(&status))
				assert.Equal(t, vcbatch.Restarting, status.State.Phase)
				assert.Equal(t, int32(2), status.RetryCount)
			})
		}
	}
}

// TestKillJobActions covers how job level actions are mapped to the retained
// pod phases and the resulting job phase for the states that accept them.
func TestKillJobActions(t *testing.T) {
	tests := []struct {
		name        string
		phase       vcbatch.JobPhase
		action      v1alpha1.Action
		wantRetain  PhaseMap
		wantPhase   vcbatch.JobPhase
		wantRetries int32
	}{
		{name: "pending restart", phase: vcbatch.Pending, action: v1alpha1.RestartJobAction, wantRetain: PodRetainPhaseNone, wantPhase: vcbatch.Restarting, wantRetries: 1},
		{name: "pending abort", phase: vcbatch.Pending, action: v1alpha1.AbortJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Aborting},
		{name: "pending complete", phase: vcbatch.Pending, action: v1alpha1.CompleteJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Completing},
		{name: "pending terminate", phase: vcbatch.Pending, action: v1alpha1.TerminateJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Terminating},
		{name: "running restart", phase: vcbatch.Running, action: v1alpha1.RestartJobAction, wantRetain: PodRetainPhaseNone, wantPhase: vcbatch.Restarting, wantRetries: 1},
		{name: "running abort", phase: vcbatch.Running, action: v1alpha1.AbortJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Aborting},
		{name: "running complete", phase: vcbatch.Running, action: v1alpha1.CompleteJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Completing},
		{name: "running terminate", phase: vcbatch.Running, action: v1alpha1.TerminateJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Terminating},
		{name: "aborting resume", phase: vcbatch.Aborting, action: v1alpha1.ResumeJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Restarting, wantRetries: 1},
		{name: "aborted resume", phase: vcbatch.Aborted, action: v1alpha1.ResumeJobAction, wantRetain: PodRetainPhaseSoft, wantPhase: vcbatch.Restarting, wantRetries: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeController(t)
			got := executeOnce(t, calls, newJobInfo("job", tt.phase, vcbatch.JobSpec{}), Action{Action: tt.action})

			require.Equal(t, "kill", got.kind)
			assert.Equal(t, tt.wantRetain, got.retain)

			status := vcbatch.JobStatus{State: vcbatch.JobState{Phase: tt.phase}}
			require.True(t, got.fn(&status))
			assert.Equal(t, tt.wantPhase, status.State.Phase)
			assert.Equal(t, tt.wantRetries, status.RetryCount)
		})
	}
}

func TestRestartingStateExecute(t *testing.T) {
	spec := vcbatch.JobSpec{
		MaxRetry: 3,
		Tasks:    []vcbatch.TaskSpec{{Name: "a", Replicas: 2}, {Name: "b", Replicas: 3}}, // 5 replicas in total
	}

	t.Run("SyncJob action syncs instead of killing", func(t *testing.T) {
		calls := fakeController(t)
		got := executeOnce(t, calls, newJobInfo("job", vcbatch.Restarting, spec), Action{Action: v1alpha1.SyncJobAction})
		assert.Equal(t, "sync", got.kind)
	})

	t.Run("RestartTask action kills the target", func(t *testing.T) {
		calls := fakeController(t)
		target := Target{Type: TargetTypeTask, TaskName: "a"}
		got := executeOnce(t, calls, newJobInfo("job", vcbatch.Restarting, spec), Action{Action: v1alpha1.RestartTaskAction, Target: target})
		assert.Equal(t, "killTarget", got.kind)
		assert.Equal(t, target, got.target)
	})

	t.Run("any other action kills every pod", func(t *testing.T) {
		calls := fakeController(t)
		got := executeOnce(t, calls, newJobInfo("job", vcbatch.Restarting, spec), Action{Action: v1alpha1.RestartJobAction})
		assert.Equal(t, "kill", got.kind)
		assert.Empty(t, got.retain, "a restart must not retain any pod phase")
	})

	tests := []struct {
		name      string
		status    vcbatch.JobStatus
		wantPhase vcbatch.JobPhase
		wantMoved bool
		wantFails float64
	}{
		{
			name:      "retries exhausted fails the job",
			status:    vcbatch.JobStatus{RetryCount: 3, MinAvailable: 1},
			wantPhase: vcbatch.Failed,
			wantMoved: true,
			wantFails: 1,
		},
		{
			name:      "retries beyond the limit also fail the job",
			status:    vcbatch.JobStatus{RetryCount: 4, MinAvailable: 1},
			wantPhase: vcbatch.Failed,
			wantMoved: true,
			wantFails: 1,
		},
		{
			name:      "enough pods gone returns the job to pending",
			status:    vcbatch.JobStatus{RetryCount: 1, MinAvailable: 3, Terminating: 2},
			wantPhase: vcbatch.Pending,
			wantMoved: true,
		},
		{
			name:      "terminating pods leave fewer than minAvailable so the job keeps restarting",
			status:    vcbatch.JobStatus{RetryCount: 1, MinAvailable: 4, Terminating: 2},
			wantPhase: vcbatch.Restarting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeController(t)
			jobInfo := newJobInfo(t.Name(), vcbatch.Restarting, spec)
			got := executeOnce(t, calls, jobInfo, Action{Action: v1alpha1.SyncJobAction})

			status := tt.status
			status.State.Phase = vcbatch.Restarting
			failedBefore := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/"+jobInfo.Name, ""))
			assert.Equal(t, tt.wantMoved, got.fn(&status))
			assert.Equal(t, tt.wantPhase, status.State.Phase)
			assert.Equal(t, tt.wantFails, testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/"+jobInfo.Name, ""))-failedBefore)
		})
	}
}

func TestPendingStateSync(t *testing.T) {
	tests := []struct {
		name         string
		minAvailable int32
		status       vcbatch.JobStatus
		wantPhase    vcbatch.JobPhase
		wantMoved    bool
	}{
		{
			name:         "not enough pods started keeps the job pending",
			minAvailable: 3,
			status:       vcbatch.JobStatus{Running: 2},
			wantPhase:    vcbatch.Pending,
		},
		{
			name:         "exactly minAvailable running starts the job",
			minAvailable: 3,
			status:       vcbatch.JobStatus{Running: 3},
			wantPhase:    vcbatch.Running,
			wantMoved:    true,
		},
		{
			name:         "succeeded and failed pods count toward minAvailable",
			minAvailable: 3,
			status:       vcbatch.JobStatus{Running: 1, Succeeded: 1, Failed: 1},
			wantPhase:    vcbatch.Running,
			wantMoved:    true,
		},
		{
			name:         "pending pods do not count toward minAvailable",
			minAvailable: 2,
			status:       vcbatch.JobStatus{Running: 1, Pending: 5},
			wantPhase:    vcbatch.Pending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeController(t)
			jobInfo := newJobInfo("job", vcbatch.Pending, vcbatch.JobSpec{MinAvailable: tt.minAvailable})
			got := executeOnce(t, calls, jobInfo, Action{Action: v1alpha1.SyncJobAction})
			require.Equal(t, "sync", got.kind)

			status := tt.status
			status.State.Phase = vcbatch.Pending
			assert.Equal(t, tt.wantMoved, got.fn(&status))
			assert.Equal(t, tt.wantPhase, status.State.Phase)
		})
	}
}

func TestRunningStateSync(t *testing.T) {
	twoTasks := func(a, b *int32) []vcbatch.TaskSpec {
		return []vcbatch.TaskSpec{
			{Name: "master", Replicas: 1, MinAvailable: a},
			{Name: "worker", Replicas: 3, MinAvailable: b},
		}
	}
	taskSucceeded := func(master, worker int32) map[string]vcbatch.TaskState {
		return map[string]vcbatch.TaskState{
			"master": {Phase: map[v1.PodPhase]int32{v1.PodSucceeded: master}},
			"worker": {Phase: map[v1.PodPhase]int32{v1.PodSucceeded: worker}},
		}
	}

	tests := []struct {
		name          string
		spec          vcbatch.JobSpec
		status        vcbatch.JobStatus
		wantPhase     vcbatch.JobPhase
		wantMoved     bool
		wantCompleted float64
		wantFailed    float64
	}{
		{
			name:      "scaled down to zero replicas keeps the phase",
			spec:      vcbatch.JobSpec{MinAvailable: 1},
			status:    vcbatch.JobStatus{Running: 1},
			wantPhase: vcbatch.Running,
		},
		{
			name:      "pods still running keeps the job running",
			spec:      vcbatch.JobSpec{MinAvailable: 4, Tasks: twoTasks(nil, nil)},
			status:    vcbatch.JobStatus{Running: 4},
			wantPhase: vcbatch.Running,
		},
		{
			name:          "minSuccess reached completes the job before all pods finish",
			spec:          vcbatch.JobSpec{MinAvailable: 4, MinSuccess: ptr.To[int32](2), Tasks: twoTasks(nil, nil)},
			status:        vcbatch.JobStatus{Running: 2, Succeeded: 2},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name:          "all pods succeeded completes the job",
			spec:          vcbatch.JobSpec{MinAvailable: 4, Tasks: twoTasks(nil, nil)},
			status:        vcbatch.JobStatus{Succeeded: 4},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name:          "all pods finished with succeeded >= minAvailable completes the job",
			spec:          vcbatch.JobSpec{MinAvailable: 3, Tasks: twoTasks(nil, nil)},
			status:        vcbatch.JobStatus{Succeeded: 3, Failed: 1},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name:       "all pods finished with succeeded < minAvailable fails the job",
			spec:       vcbatch.JobSpec{MinAvailable: 4, Tasks: twoTasks(nil, nil)},
			status:     vcbatch.JobStatus{Succeeded: 3, Failed: 1},
			wantPhase:  vcbatch.Failed,
			wantMoved:  true,
			wantFailed: 1,
		},
		{
			name:       "all pods finished with succeeded < minSuccess fails the job",
			spec:       vcbatch.JobSpec{MinAvailable: 1, MinSuccess: ptr.To[int32](3), Tasks: twoTasks(nil, nil)},
			status:     vcbatch.JobStatus{Succeeded: 2, Failed: 2},
			wantPhase:  vcbatch.Failed,
			wantMoved:  true,
			wantFailed: 1,
		},
		{
			name: "a task below its own minAvailable fails the job even if the job minAvailable is met",
			spec: vcbatch.JobSpec{MinAvailable: 3, Tasks: twoTasks(ptr.To[int32](1), ptr.To[int32](2))},
			status: vcbatch.JobStatus{
				Succeeded: 3, Failed: 1,
				TaskStatusCount: taskSucceeded(0, 3), // master has no succeeded pod, needs 1
			},
			wantPhase:  vcbatch.Failed,
			wantMoved:  true,
			wantFailed: 1,
		},
		{
			name: "every task meeting its own minAvailable completes the job",
			spec: vcbatch.JobSpec{MinAvailable: 3, Tasks: twoTasks(ptr.To[int32](1), ptr.To[int32](2))},
			status: vcbatch.JobStatus{
				Succeeded: 3, Failed: 1,
				TaskStatusCount: taskSucceeded(1, 2),
			},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name: "task minAvailable is ignored when the job minAvailable is lower than their sum",
			spec: vcbatch.JobSpec{MinAvailable: 1, Tasks: twoTasks(ptr.To[int32](1), ptr.To[int32](2))},
			status: vcbatch.JobStatus{
				Succeeded: 3, Failed: 1,
				TaskStatusCount: taskSucceeded(0, 3),
			},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name: "task with minAvailable but no recorded status is skipped",
			spec: vcbatch.JobSpec{MinAvailable: 4, Tasks: twoTasks(ptr.To[int32](1), nil)},
			status: vcbatch.JobStatus{
				Succeeded:       4,
				TaskStatusCount: map[string]vcbatch.TaskState{"worker": {Phase: map[v1.PodPhase]int32{v1.PodSucceeded: 3}}},
			},
			wantPhase:     vcbatch.Completed,
			wantMoved:     true,
			wantCompleted: 1,
		},
		{
			name:      "too many pending pods sends the job back to pending",
			spec:      vcbatch.JobSpec{MinAvailable: 3, Tasks: twoTasks(nil, nil)},
			status:    vcbatch.JobStatus{Running: 2, Pending: 2}, // 2 > 4-3
			wantPhase: vcbatch.Pending,
			wantMoved: true,
		},
		{
			name:      "pending pods within the tolerated amount keep the job running",
			spec:      vcbatch.JobSpec{MinAvailable: 3, Tasks: twoTasks(nil, nil)},
			status:    vcbatch.JobStatus{Running: 3, Pending: 1}, // 1 == 4-3
			wantPhase: vcbatch.Running,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := fakeController(t)
			jobInfo := newJobInfo(t.Name(), vcbatch.Running, tt.spec)
			got := executeOnce(t, calls, jobInfo, Action{Action: v1alpha1.SyncJobAction})
			require.Equal(t, "sync", got.kind)

			status := tt.status
			status.State.Phase = vcbatch.Running
			// The counters are process-wide and cumulative, so compare deltas.
			key := "ns/" + jobInfo.Name
			completedBefore := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues(key, ""))
			failedBefore := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues(key, ""))
			assert.Equal(t, tt.wantMoved, got.fn(&status))
			assert.Equal(t, tt.wantPhase, status.State.Phase)
			assert.Equal(t, tt.wantCompleted, testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues(key, ""))-completedBefore)
			assert.Equal(t, tt.wantFailed, testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues(key, ""))-failedBefore)
		})
	}
}
