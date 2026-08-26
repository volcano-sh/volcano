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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestUnknownState_OpenQueueAction(t *testing.T) {
	testcases := []struct {
		name          string
		queue         *schedulingv1beta1.Queue
		podGroups     []string
		expectedState schedulingv1beta1.QueueState
	}{
		{
			name: "OpenQueueAction: Unknown state",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateUnknown},
			},
			podGroups:     []string{},
			expectedState: schedulingv1beta1.QueueStateOpen,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			origOpenQueue := OpenQueue
			t.Cleanup(func() { OpenQueue = origOpenQueue })

			var capturedState schedulingv1beta1.QueueState
			OpenQueue = func(queue *schedulingv1beta1.Queue, fn UpdateQueueStatusFn) error {
				if queue != tc.queue {
					t.Errorf("expected queue %v, got %v", tc.queue, queue)
				}
				fakeStatus := &schedulingv1beta1.QueueStatus{}
				fn(fakeStatus, tc.podGroups)
				capturedState = fakeStatus.State
				return nil
			}

			s := &unknownState{queue: tc.queue}
			err := s.Execute(busv1alpha1.OpenQueueAction)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if capturedState != tc.expectedState {
				t.Errorf("expected state %q got %q", tc.expectedState, capturedState)
			}
		})
	}
}

func TestUnknownState_CloseQueueAction(t *testing.T) {
	testcases := []struct {
		name          string
		queue         *schedulingv1beta1.Queue
		podGroups     []string
		expectedState schedulingv1beta1.QueueState
	}{
		{
			name: "CloseQueueAction: Unknown state queue with no podgroups",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateUnknown},
			},
			podGroups:     []string{},
			expectedState: schedulingv1beta1.QueueStateClosed,
		},
		{
			name: "CloseQueueAction: Unknown state queue with running podgroups",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateUnknown},
			},
			podGroups:     []string{"pg1"},
			expectedState: schedulingv1beta1.QueueStateClosing,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			origCloseQueue := CloseQueue
			t.Cleanup(func() { CloseQueue = origCloseQueue })

			var capturedState schedulingv1beta1.QueueState
			CloseQueue = func(queue *schedulingv1beta1.Queue, fn UpdateQueueStatusFn) error {
				if queue != tc.queue {
					t.Errorf("expected queue %v, got %v", tc.queue, queue)
				}
				fakeStatus := &schedulingv1beta1.QueueStatus{}
				fn(fakeStatus, tc.podGroups)
				capturedState = fakeStatus.State
				return nil
			}

			s := &unknownState{queue: tc.queue}
			err := s.Execute(busv1alpha1.CloseQueueAction)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if capturedState != tc.expectedState {
				t.Errorf("expected state %q got %q", tc.expectedState, capturedState)
			}
		})
	}
}

func TestUnknownState_SyncQueueAction(t *testing.T) {
	testcases := []struct {
		name          string
		queue         *schedulingv1beta1.Queue
		podGroups     []string
		expectedState schedulingv1beta1.QueueState
	}{
		{
			name: "SyncQueueAction: Spec state open",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			podGroups:     []string{},
			expectedState: schedulingv1beta1.QueueStateOpen,
		},
		{
			name: "SyncQueueAction: Spec state closed with no podgroups",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			podGroups:     []string{},
			expectedState: schedulingv1beta1.QueueStateClosed,
		},
		{
			name: "SyncQueueAction: Spec state closed with running podgroups",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			podGroups:     []string{"pg1"},
			expectedState: schedulingv1beta1.QueueStateClosing,
		},
		{
			name: "SyncQueueAction: Spec state unknown/garbage",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "test-queue"},
				Status:     schedulingv1beta1.QueueStatus{State: "unexpected-state"},
			},
			podGroups:     []string{},
			expectedState: schedulingv1beta1.QueueStateUnknown,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			origSyncQueue := SyncQueue
			t.Cleanup(func() { SyncQueue = origSyncQueue })

			var capturedState schedulingv1beta1.QueueState
			SyncQueue = func(queue *schedulingv1beta1.Queue, fn UpdateQueueStatusFn) error {
				if queue != tc.queue {
					t.Errorf("expected queue %v, got %v", tc.queue, queue)
				}
				fakeStatus := &schedulingv1beta1.QueueStatus{}
				fn(fakeStatus, tc.podGroups)
				capturedState = fakeStatus.State
				return nil
			}

			s := &unknownState{queue: tc.queue}
			err := s.Execute(busv1alpha1.SyncQueueAction)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if capturedState != tc.expectedState {
				t.Errorf("expected state %q got %q", tc.expectedState, capturedState)
			}
		})
	}
}
