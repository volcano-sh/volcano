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

type unknownState struct {
	queue *vcschedulingv1.Queue
}

func (us *unknownState) Execute(action vcbusv1.Action) error {
	switch action {
	case vcbusv1.OpenQueueAction:
		return OpenQueue(us.queue, func(status *vcschedulingv1.QueueStatus, podGroupList []string) {
			status.State = vcschedulingv1.QueueStateOpen
		})
	case vcbusv1.CloseQueueAction:
		return CloseQueue(us.queue, func(status *vcschedulingv1.QueueStatus, podGroupList []string) {
			if len(podGroupList) == 0 {
				status.State = vcschedulingv1.QueueStateClosed
				return
			}
			status.State = vcschedulingv1.QueueStateClosing
		})
	default:
		return SyncQueue(us.queue, func(status *vcschedulingv1.QueueStatus, podGroupList []string) {
			specState := us.queue.Status.State
			if specState == vcschedulingv1.QueueStateOpen {
				status.State = vcschedulingv1.QueueStateOpen
				return
			}

			if specState == vcschedulingv1.QueueStateClosed {
				if len(podGroupList) == 0 {
					status.State = vcschedulingv1.QueueStateClosed
					return
				}
				status.State = vcschedulingv1.QueueStateClosing

				return
			}

			status.State = vcschedulingv1.QueueStateUnknown
		})
	}
}
