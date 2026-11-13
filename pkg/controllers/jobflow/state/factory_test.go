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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func newTestJobFlow(phase v1alpha1.Phase) *v1alpha1.JobFlow {
	return &v1alpha1.JobFlow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-jobflow",
			Namespace: "default",
		},
		Status: v1alpha1.JobFlowStatus{
			State: v1alpha1.State{
				Phase: phase,
			},
		},
	}
}

func TestNewState(t *testing.T) {
	testcases := []struct {
		name         string
		phase        v1alpha1.Phase
		expectedType string
		expectNil    bool
	}{
		{
			name:         "Empty phase defaults to pendingState",
			phase:        "",
			expectedType: "*state.pendingState",
		},
		{
			name:         "Pending phase returns pendingState",
			phase:        v1alpha1.Pending,
			expectedType: "*state.pendingState",
		},
		{
			name:         "Running phase returns runningState",
			phase:        v1alpha1.Running,
			expectedType: "*state.runningState",
		},
		{
			name:         "Succeed phase returns succeedState",
			phase:        v1alpha1.Succeed,
			expectedType: "*state.succeedState",
		},
		{
			name:         "Terminating phase returns terminatingState",
			phase:        v1alpha1.Terminating,
			expectedType: "*state.terminatingState",
		},
		{
			name:         "Failed phase returns failedState",
			phase:        v1alpha1.Failed,
			expectedType: "*state.failedState",
		},
		{
			name:      "Unknown phase returns nil",
			phase:     "UnknownPhase",
			expectNil: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobFlow := newTestJobFlow(tc.phase)
			s := NewState(jobFlow)
			if tc.expectNil {
				if s != nil {
					t.Errorf("expected nil for phase %q, got %T", tc.phase, s)
				}
				return
			}
			if s == nil {
				t.Fatalf("expected non-nil state for phase %q", tc.phase)
			}
			actualType := reflect.TypeOf(s).String()
			if actualType != tc.expectedType {
				t.Errorf("expected type %s, got %s", tc.expectedType, actualType)
			}
		})
	}
}
