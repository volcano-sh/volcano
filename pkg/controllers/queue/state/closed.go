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
	"volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
)

type closedState struct {
	queue *v1alpha2.Queue
}

func (cs *closedState) Execute(action v1alpha1.Action) error {
	switch action {
	case v1alpha1.OpenQueueAction:
		return OpenQueue(cs.queue, func(status *v1alpha2.QueueStatus, podGroupList []string) {
			status.State = v1alpha2.QueueStateOpen
			return
		})
	case v1alpha1.CloseQueueAction:
		return SyncQueue(cs.queue, func(status *v1alpha2.QueueStatus, podGroupList []string) {
			status.State = v1alpha2.QueueStateClosed
			return
		})
	default:
		return SyncQueue(cs.queue, func(status *v1alpha2.QueueStatus, podGroupList []string) {
			specState := cs.queue.Spec.State
			if specState == v1alpha2.QueueStateOpen {
				status.State = v1alpha2.QueueStateOpen
				return
			}

			if specState == v1alpha2.QueueStateClosed {
				status.State = v1alpha2.QueueStateClosed
				return
			}

			status.State = v1alpha2.QueueStateUnknown
			return
		})
	}

	return nil
}
