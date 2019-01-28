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
	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/controllers/job/apis"
)

type pendingState struct {
	job *apis.JobInfo
}

func (ps *pendingState) Execute(action vkv1.Action) error {
	switch action {
	case vkv1.RestartJobAction:
		return KillJob(ps.job, func(status vkv1.JobStatus) vkv1.JobState {
			phase := vkv1.Pending
			if status.Terminating != 0 {
				phase = vkv1.Restarting
			}

			return vkv1.JobState{
				Phase: phase,
			}
		})

	case vkv1.AbortJobAction:
		return KillJob(ps.job, func(status vkv1.JobStatus) vkv1.JobState {
			phase := vkv1.Pending
			if status.Terminating != 0 {
				phase = vkv1.Aborting
			}

			return vkv1.JobState{
				Phase: phase,
			}
		})
	default:
		return SyncJob(ps.job, func(status vkv1.JobStatus) vkv1.JobState {
			total := totalTasks(ps.job.Job)
			phase := vkv1.Pending

			if total == status.Running {
				phase = vkv1.Running
			}
			return vkv1.JobState{
				Phase: phase,
			}
		})
	}
}
