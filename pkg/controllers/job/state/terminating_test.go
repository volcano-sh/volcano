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

func TestTerminatingState_Execute(t *testing.T) {
	origKillJob := KillJob
	t.Cleanup(func() { KillJob = origKillJob })

	testcases := []struct {
		name          string
		status        vcbatch.JobStatus
		expectedPhase vcbatch.JobPhase
		expectChanged bool
	}{
		{
			name: "Has Terminating pods: stays Terminating",
			status: vcbatch.JobStatus{
				Terminating: 2,
			},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name: "Has Pending pods: stays Terminating",
			status: vcbatch.JobStatus{
				Pending: 1,
			},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name: "Has Running pods: stays Terminating",
			status: vcbatch.JobStatus{
				Running: 3,
			},
			expectedPhase: "",
			expectChanged: false,
		},
		{
			name:          "All pods done: transitions to Terminated",
			status:        vcbatch.JobStatus{},
			expectedPhase: vcbatch.Terminated,
			expectChanged: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(vcbatch.Terminating)
			state := &terminatingState{job: jobInfo}

			var capturedPhase vcbatch.JobPhase
			var changed bool

			KillJob = func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error {
				status := tc.status
				if fn != nil {
					changed = fn(&status)
					capturedPhase = status.State.Phase
				}
				return nil
			}

			err := state.Execute(Action{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if changed != tc.expectChanged {
				t.Errorf("expected changed %v, got %v", tc.expectChanged, changed)
			}
			if capturedPhase != tc.expectedPhase {
				t.Errorf("expected phase %v, got %v", tc.expectedPhase, capturedPhase)
			}
		})
	}
}
