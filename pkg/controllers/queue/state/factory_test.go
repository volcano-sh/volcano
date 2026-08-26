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
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestNewState(t *testing.T) {
	testcases := []struct {
		name         string
		queueState   schedulingv1beta1.QueueState
		expectedType string
	}{
		{
			name:         "Empty state returns openState",
			queueState:   "",
			expectedType: "*state.openState",
		},
		{
			name:         "Open state returns openState",
			queueState:   schedulingv1beta1.QueueStateOpen,
			expectedType: "*state.openState",
		},
		{
			name:         "Closed state returns closedState",
			queueState:   schedulingv1beta1.QueueStateClosed,
			expectedType: "*state.closedState",
		},
		{
			name:         "Closing state returns closingState",
			queueState:   schedulingv1beta1.QueueStateClosing,
			expectedType: "*state.closingState",
		},
		{
			name:         "Unknown state returns unknownState",
			queueState:   schedulingv1beta1.QueueStateUnknown,
			expectedType: "*state.unknownState",
		},
		{
			name:         "Unrecognised state returns nil",
			queueState:   "UnseenState",
			expectedType: "<nil>",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			queue := &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status: schedulingv1beta1.QueueStatus{
					State: tc.queueState,
				},
			}

			s := NewState(queue)

			var actualType string
			if s == nil {
				actualType = "<nil>"
			} else {
				actualType = reflect.TypeOf(s).String()
			}

			if actualType != tc.expectedType {
				t.Errorf("expected %s, got %s", tc.expectedType, actualType)
			}
		})
	}
}
