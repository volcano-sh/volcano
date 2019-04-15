/*
Copyright 2017 The Volcano Authors.

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
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/apis"
)

type terminatingState struct {
	job *apis.JobInfo
}

func (ps *terminatingState) Execute(action vkv1.Action) error {
	return KillJob(ps.job, func(status *vkv1.JobStatus) {
		// If any "alive" pods, still in Terminating phase
		phase := vkv1.Terminated
		if status.Terminating != 0 || status.Pending != 0 || status.Running != 0 {
			phase = vkv1.Terminating
		}

		status.State.Phase = phase
	})
}
