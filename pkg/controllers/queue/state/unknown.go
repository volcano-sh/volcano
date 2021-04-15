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

type unknownState struct {
	queue *v1beta1.Queue
}

func (us *unknownState) Execute(action v1alpha1.Action) error {
	switch action {
	case v1alpha1.OpenQueueAction:
		return OpenQueue(us.queue, func(status *v1beta1.QueueStatus, podGroupList []string) {
			status.State = v1beta1.QueueStateOpen
		})
	case v1alpha1.CloseQueueAction:
		return CloseQueue(us.queue, func(status *v1beta1.QueueStatus, podGroupList []string) {
			if len(podGroupList) == 0 {
				status.State = v1beta1.QueueStateClosed
				return
			}
			status.State = v1beta1.QueueStateClosing
		})
	default:
		return SyncQueue(us.queue, func(status *v1beta1.QueueStatus, podGroupList []string) {
			specState := us.queue.Status.State
			if specState == v1beta1.QueueStateOpen {
				status.State = v1beta1.QueueStateOpen
				return
			}

			if specState == v1beta1.QueueStateClosed {
				if len(podGroupList) == 0 {
					status.State = v1beta1.QueueStateClosed
					return
				}
				status.State = v1beta1.QueueStateClosing

				return
			}

			status.State = v1beta1.QueueStateUnknown
		})
	}
}
