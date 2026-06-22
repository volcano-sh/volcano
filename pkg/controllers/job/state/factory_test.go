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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func jobInfoWithPhase(phase vcbatch.JobPhase) *apis.JobInfo {
	return &apis.JobInfo{
		Job: &vcbatch.Job{
			Status: vcbatch.JobStatus{
				State: vcbatch.JobState{Phase: phase},
			},
		},
	}
}

func TestNewState_PhaseMapping(t *testing.T) {
	tests := []struct {
		phase    vcbatch.JobPhase
		wantType State
	}{
		{vcbatch.Pending, &pendingState{}},
		{vcbatch.Running, &runningState{}},
		{vcbatch.Restarting, &restartingState{}},
		{vcbatch.Terminated, &finishedState{}},
		{vcbatch.Completed, &finishedState{}},
		{vcbatch.Failed, &finishedState{}},
		{vcbatch.Terminating, &terminatingState{}},
		{vcbatch.Aborting, &abortingState{}},
		{vcbatch.Aborted, &abortedState{}},
		{vcbatch.Completing, &completingState{}},
	}

	for _, tt := range tests {
		t.Run(string(tt.phase), func(t *testing.T) {
			got := NewState(jobInfoWithPhase(tt.phase))
			if reflect.TypeOf(got) != reflect.TypeOf(tt.wantType) {
				t.Errorf("phase %q -> %T, want %T", tt.phase, got, tt.wantType)
			}
		})
	}
}

func TestNewState_UnknownPhaseDefaultsToPending(t *testing.T) {
	got := NewState(jobInfoWithPhase(vcbatch.JobPhase("totally-unknown-phase")))
	if _, ok := got.(*pendingState); !ok {
		t.Errorf("unknown phase -> %T, want *pendingState", got)
	}
}

func TestNewState_EmptyPhaseDefaultsToPending(t *testing.T) {
	got := NewState(jobInfoWithPhase(""))
	if _, ok := got.(*pendingState); !ok {
		t.Errorf("empty phase -> %T, want *pendingState", got)
	}
}

func TestNewState_StateHoldsTheGivenJobInfo(t *testing.T) {
	ji := jobInfoWithPhase(vcbatch.Running)
	st := NewState(ji).(*runningState)
	if st.job != ji {
		t.Errorf("state.job not set to provided JobInfo")
	}
}

func TestPodRetainPhaseConstants(t *testing.T) {
	if len(PodRetainPhaseNone) != 0 {
		t.Errorf("PodRetainPhaseNone should be empty, got %v", PodRetainPhaseNone)
	}
	if len(PodRetainPhaseSoft) != 2 {
		t.Errorf("PodRetainPhaseSoft should have 2 entries, got %d", len(PodRetainPhaseSoft))
	}
	if _, ok := PodRetainPhaseSoft[v1.PodSucceeded]; !ok {
		t.Errorf("PodRetainPhaseSoft missing PodSucceeded: %v", PodRetainPhaseSoft)
	}
	if _, ok := PodRetainPhaseSoft[v1.PodFailed]; !ok {
		t.Errorf("PodRetainPhaseSoft missing PodFailed: %v", PodRetainPhaseSoft)
	}
}
