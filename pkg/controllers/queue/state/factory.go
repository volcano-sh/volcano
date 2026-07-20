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
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// State interface.
type State interface {
	// Execute executes the actions based on current state.
	Execute(action v1alpha1.Action) error
}

// UpdateQueueStatusFn updates the queue status.
type UpdateQueueStatusFn func(status *v1beta1.QueueStatus, podGroupList []string)

// QueueActionFn will open, close or sync queue.
type QueueActionFn func(queue *v1beta1.Queue, fn UpdateQueueStatusFn) error

var (
	// SyncQueue will sync queue status.
	SyncQueue QueueActionFn
	// OpenQueue will set state of queue to open
	OpenQueue QueueActionFn
	// CloseQueue will set state of queue to close
	CloseQueue QueueActionFn
	// ClearClosedByParentAnnotation marks the queue as explicitly closed, so
	// it is not automatically reopened when its parent queue reopens.
	ClearClosedByParentAnnotation func(queue *v1beta1.Queue) error
)

// NewState gets the state from queue status. event is the event of the request that
// triggered this action, used to tell an explicit user command (v1alpha1.CommandIssuedEvent)
// apart from an action the controller cascaded automatically (e.g. closing a child queue
// because its parent closed).
func NewState(queue *v1beta1.Queue, event v1alpha1.Event) State {
	switch queue.Status.State {
	case "", v1beta1.QueueStateOpen:
		return &openState{queue: queue}
	case v1beta1.QueueStateClosed:
		return &closedState{queue: queue, event: event}
	case v1beta1.QueueStateClosing:
		return &closingState{queue: queue, event: event}
	case v1beta1.QueueStateUnknown:
		return &unknownState{queue: queue}
	}

	return nil
}
