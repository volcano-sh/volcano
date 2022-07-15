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
	vcbusv1 "volcano.sh/apis/pkg/apis/bus/v1"
	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1"
)

// State interface.
type State interface {
	// Execute executes the actions based on current state.
	Execute(action vcbusv1.Action) error
}

// UpdateQueueStatusFn updates the queue status.
type UpdateQueueStatusFn func(status *vcschedulingv1.QueueStatus, podGroupList []string)

// QueueActionFn will open, close or sync queue.
type QueueActionFn func(queue *vcschedulingv1.Queue, fn UpdateQueueStatusFn) error

var (
	// SyncQueue will sync queue status.
	SyncQueue QueueActionFn
	// OpenQueue will set state of queue to open
	OpenQueue QueueActionFn
	// CloseQueue will set state of queue to close
	CloseQueue QueueActionFn
)

// NewState gets the state from queue status.
func NewState(queue *vcschedulingv1.Queue) State {
	switch queue.Status.State {
	case "", vcschedulingv1.QueueStateOpen:
		return &openState{queue: queue}
	case vcschedulingv1.QueueStateClosed:
		return &closedState{queue: queue}
	case vcschedulingv1.QueueStateClosing:
		return &closingState{queue: queue}
	case vcschedulingv1.QueueStateUnknown:
		return &unknownState{queue: queue}
	}

	return nil
}
