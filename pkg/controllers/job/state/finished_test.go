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

func TestFinishedState_Execute_AllPhasesCallKillJob(t *testing.T) {
	terminalPhases := []vcbatch.JobPhase{
		vcbatch.Completed,
		vcbatch.Terminated,
		vcbatch.Failed,
	}

	for _, phase := range terminalPhases {
		t.Run(string(phase), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			st := NewState(makeJob(phase))
			if err := st.Execute(Action{Action: busv1alpha1.SyncJobAction}); err != nil {
				t.Fatalf("Execute returned err: %v", err)
			}

			if len(m.KillJobCalls) != 1 {
				t.Fatalf("KillJob calls = %d, want 1", len(m.KillJobCalls))
			}
			if len(m.SyncCalls) != 0 || len(m.KillTargetCalls) != 0 {
				t.Errorf("unexpected calls: sync=%d killTarget=%d", len(m.SyncCalls), len(m.KillTargetCalls))
			}

			call := m.KillJobCalls[0]
			if _, ok := call.RetainPhase[v1.PodSucceeded]; !ok {
				t.Errorf("RetainPhase missing PodSucceeded: %v", call.RetainPhase)
			}
			if _, ok := call.RetainPhase[v1.PodFailed]; !ok {
				t.Errorf("RetainPhase missing PodFailed: %v", call.RetainPhase)
			}
			if call.HadStatusFn {
				t.Errorf("finishedState should pass nil UpdateStatusFn")
			}
		})
	}
}

func TestFinishedState_Execute_AnyActionStillKillsJob(t *testing.T) {
	actions := []busv1alpha1.Action{
		busv1alpha1.SyncJobAction,
		busv1alpha1.RestartJobAction,
		busv1alpha1.AbortJobAction,
		busv1alpha1.CompleteJobAction,
		busv1alpha1.TerminateJobAction,
		busv1alpha1.ResumeJobAction,
		busv1alpha1.RestartTaskAction,
	}

	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			m, cleanup := newMock()
			defer cleanup()

			st := NewState(makeJob(vcbatch.Completed))
			if err := st.Execute(Action{Action: action}); err != nil {
				t.Fatalf("Execute returned err: %v", err)
			}
			if len(m.KillJobCalls) != 1 {
				t.Errorf("KillJob calls = %d, want 1 for action %q", len(m.KillJobCalls), action)
			}
		})
	}
}

func TestFinishedState_Execute_PropagatesError(t *testing.T) {
	m, cleanup := newMock()
	defer cleanup()
	m.KillJobErr = errors.New("boom")

	st := NewState(makeJob(vcbatch.Failed))
	err := st.Execute(Action{Action: busv1alpha1.SyncJobAction})
	if err == nil || err.Error() != "boom" {
		t.Errorf("Execute err = %v, want \"boom\"", err)
	}
}
