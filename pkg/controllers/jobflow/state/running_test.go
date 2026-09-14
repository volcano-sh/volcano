/*
Copyright 2025 The Volcano Authors.

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

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestRunningStateExecute(t *testing.T) {
	// Fake SyncJobFlow so the test exercises only the phase-decision closure.
	orig := SyncJobFlow
	defer func() { SyncJobFlow = orig }()
	SyncJobFlow = func(jobFlow *v1alpha1.JobFlow, fn UpdateJobFlowStatusFn) error {
		fn(&jobFlow.Status, len(jobFlow.Spec.Flows))
		return nil
	}

	// Execute only runs while the jobflow sits in Running, so every case starts there.
	running := v1alpha1.State{Phase: v1alpha1.Running}

	tests := []struct {
		name   string
		status v1alpha1.JobFlowStatus
		flows  int
		want   v1alpha1.Phase
	}{
		{
			name:   "all jobs completed settles to Succeed",
			status: v1alpha1.JobFlowStatus{State: running, CompletedJobs: []string{"job-a"}},
			flows:  1,
			want:   v1alpha1.Succeed,
		},
		{
			name:   "a failed job settles to Failed",
			status: v1alpha1.JobFlowStatus{State: running, FailedJobs: []string{"job-a"}},
			flows:  1,
			want:   v1alpha1.Failed,
		},
		{
			name:   "a terminated job settles to Failed",
			status: v1alpha1.JobFlowStatus{State: running, TerminatedJobs: []string{"job-a"}},
			flows:  1,
			want:   v1alpha1.Failed,
		},
		{
			name:   "not every job is done yet so it stays Running",
			status: v1alpha1.JobFlowStatus{State: running, CompletedJobs: []string{"job-a"}},
			flows:  2,
			want:   v1alpha1.Running,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobFlow := &v1alpha1.JobFlow{
				Spec:   v1alpha1.JobFlowSpec{Flows: make([]v1alpha1.Flow, tt.flows)},
				Status: tt.status,
			}
			if err := (&runningState{jobFlow: jobFlow}).Execute(v1alpha1.SyncJobFlowAction); err != nil {
				t.Fatalf("Execute returned an unexpected error: %v", err)
			}
			if got := jobFlow.Status.State.Phase; got != tt.want {
				t.Errorf("phase = %q, want %q", got, tt.want)
			}
		})
	}
}
