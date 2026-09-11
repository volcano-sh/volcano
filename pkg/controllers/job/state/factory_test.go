/*
Copyright 2019 The Volcano Authors.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

// newTestJobInfo creates a minimal JobInfo for use in state machine tests.
func newTestJobInfo(phase vcbatch.JobPhase) *apis.JobInfo {
	return &apis.JobInfo{
		Job: &vcbatch.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-job",
				Namespace: "default",
			},
			Spec: vcbatch.JobSpec{
				MinAvailable: 1,
				Tasks: []vcbatch.TaskSpec{
					{
						Name:     "task",
						Replicas: 3,
					},
				},
			},
			Status: vcbatch.JobStatus{
				State: vcbatch.JobState{
					Phase: phase,
				},
			},
		},
	}
}

func TestNewState(t *testing.T) {
	testcases := []struct {
		name         string
		phase        vcbatch.JobPhase
		expectedType string
	}{
		{
			name:         "Pending phase returns pendingState",
			phase:        vcbatch.Pending,
			expectedType: "*state.pendingState",
		},
		{
			name:         "Running phase returns runningState",
			phase:        vcbatch.Running,
			expectedType: "*state.runningState",
		},
		{
			name:         "Restarting phase returns restartingState",
			phase:        vcbatch.Restarting,
			expectedType: "*state.restartingState",
		},
		{
			name:         "Terminated phase returns finishedState",
			phase:        vcbatch.Terminated,
			expectedType: "*state.finishedState",
		},
		{
			name:         "Completed phase returns finishedState",
			phase:        vcbatch.Completed,
			expectedType: "*state.finishedState",
		},
		{
			name:         "Failed phase returns finishedState",
			phase:        vcbatch.Failed,
			expectedType: "*state.finishedState",
		},
		{
			name:         "Terminating phase returns terminatingState",
			phase:        vcbatch.Terminating,
			expectedType: "*state.terminatingState",
		},
		{
			name:         "Aborting phase returns abortingState",
			phase:        vcbatch.Aborting,
			expectedType: "*state.abortingState",
		},
		{
			name:         "Aborted phase returns abortedState",
			phase:        vcbatch.Aborted,
			expectedType: "*state.abortedState",
		},
		{
			name:         "Completing phase returns completingState",
			phase:        vcbatch.Completing,
			expectedType: "*state.completingState",
		},
		{
			name:         "Empty phase defaults to pendingState",
			phase:        "",
			expectedType: "*state.pendingState",
		},
		{
			name:         "Unknown phase defaults to pendingState",
			phase:        "UnknownPhase",
			expectedType: "*state.pendingState",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobInfo := newTestJobInfo(tc.phase)
			s := NewState(jobInfo)
			actualType := reflect.TypeOf(s).String()
			if actualType != tc.expectedType {
				t.Errorf("expected type %s, got %s", tc.expectedType, actualType)
			}
		})
	}
}
