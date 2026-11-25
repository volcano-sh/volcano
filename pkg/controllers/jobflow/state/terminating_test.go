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
	"testing"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

func TestTerminatingState_Execute(t *testing.T) {
	testcases := []struct {
		name   string
		action v1alpha1.Action
	}{
		{
			name:   "SyncJobFlowAction: returns nil",
			action: v1alpha1.SyncJobFlowAction,
		},
		{
			name:   "Custom action: returns nil",
			action: v1alpha1.Action("CustomAction"),
		},
		{
			name:   "Empty action: returns nil",
			action: "",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			jobFlow := newTestJobFlow(v1alpha1.Terminating)
			state := &terminatingState{jobFlow: jobFlow}

			err := state.Execute(tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
