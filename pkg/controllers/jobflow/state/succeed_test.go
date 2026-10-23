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
	"errors"
	"testing"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestSucceedState_SyncJobFlowAction(t *testing.T) {
	origSyncJobFlow := SyncJobFlow
	t.Cleanup(func() { SyncJobFlow = origSyncJobFlow })

	testcases := []struct {
		name          string
		mockErr       error
		expectCall    bool
		expectedError bool
	}{
		{
			name:          "SyncJobFlowAction: successfully calls SyncJobFlow and executes callback",
			mockErr:       nil,
			expectCall:    true,
			expectedError: false,
		},
		{
			name:          "SyncJobFlowAction: propagates error returned from SyncJobFlow",
			mockErr:       errors.New("sync failed"),
			expectCall:    true,
			expectedError: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobFlow := newTestJobFlow(v1alpha1.Succeed)
			state := &succeedState{jobFlow: jobFlow}

			var called bool
			SyncJobFlow = func(jf *v1alpha1.JobFlow, fn UpdateJobFlowStatusFn) error {
				called = true
				if fn != nil {
					fn(&jf.Status, 0)
				}
				return tc.mockErr
			}

			err := state.Execute(v1alpha1.SyncJobFlowAction)
			if called != tc.expectCall {
				t.Errorf("expected called %v, got %v", tc.expectCall, called)
			}
			if (err != nil) != tc.expectedError {
				t.Errorf("expected error %v, got %v", tc.expectedError, err)
			}
		})
	}
}

func TestSucceedState_DefaultAction(t *testing.T) {
	origSyncJobFlow := SyncJobFlow
	t.Cleanup(func() { SyncJobFlow = origSyncJobFlow })

	testcases := []struct {
		name   string
		action v1alpha1.Action
	}{
		{
			name:   "Default action: does not call SyncJobFlow and returns nil",
			action: v1alpha1.Action("UnknownAction"),
		},
		{
			name:   "Empty action: does not call SyncJobFlow and returns nil",
			action: "",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobFlow := newTestJobFlow(v1alpha1.Succeed)
			state := &succeedState{jobFlow: jobFlow}

			var called bool
			SyncJobFlow = func(jf *v1alpha1.JobFlow, fn UpdateJobFlowStatusFn) error {
				called = true
				return nil
			}

			err := state.Execute(tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if called {
				t.Errorf("expected SyncJobFlow not to be called for action %q", tc.action)
			}
		})
	}
}
