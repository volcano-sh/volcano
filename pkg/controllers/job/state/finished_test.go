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
	"volcano.sh/volcano/pkg/controllers/apis"
)

func TestFinishedState_Execute(t *testing.T) {
	origKillJob := KillJob
	t.Cleanup(func() { KillJob = origKillJob })

	testcases := []struct {
		name  string
		phase vcbatch.JobPhase
	}{
		{
			name:  "Completed phase: calls KillJob",
			phase: vcbatch.Completed,
		},
		{
			name:  "Terminated phase: calls KillJob",
			phase: vcbatch.Terminated,
		},
		{
			name:  "Failed phase: calls KillJob",
			phase: vcbatch.Failed,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(tc.phase)
			state := &finishedState{job: jobInfo}

			var killJobCalled bool
			var capturedFn UpdateStatusFn
			var capturedRetainPhase PhaseMap

			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				killJobCalled = true
				capturedRetainPhase = podRetainPhase
				capturedFn = fn
				return nil
			}

			err := state.Execute(Action{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !killJobCalled {
				t.Errorf("expected KillJob to be called")
			}
			if capturedFn != nil {
				t.Errorf("expected fn to be nil, got %v", capturedFn)
			}
			if len(capturedRetainPhase) == 0 {
				t.Errorf("expected PodRetainPhaseSoft to be passed, got empty PhaseMap")
			}
		})
	}
}
