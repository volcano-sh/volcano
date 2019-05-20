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
	"volcano.sh/volcano/pkg/controllers/apis"
)

type runningState struct {
	job *apis.JobInfo
}

func (ps *runningState) Execute(action vkv1.Action) error {
	switch action {
	case vkv1.RestartJobAction:
		return KillJob(ps.job, PodRetainPhaseNone, func(status *vkv1.JobStatus) bool {
			if status.Terminating != 0 {
				status.State.Phase = vkv1.Restarting
				status.RetryCount++
				return true
			}
			return false
		})
	case vkv1.AbortJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vkv1.JobStatus) bool {
			if status.Terminating != 0 {
				status.State.Phase = vkv1.Aborting
				return true
			}

			return false
		})
	case vkv1.TerminateJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vkv1.JobStatus) bool {
			if status.Terminating != 0 {
				status.State.Phase = vkv1.Terminating
				return true
			}

			return false
		})
	case vkv1.CompleteJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vkv1.JobStatus) bool {
			phase := vkv1.Completed
			if status.Terminating != 0 {
				phase = vkv1.Completing
			}

			status.State.Phase = phase
			return true
		})
	default:
		return SyncJob(ps.job, func(status *vkv1.JobStatus) bool {
			if status.Succeeded+status.Failed == TotalTasks(ps.job.Job) {
				status.State.Phase = vkv1.Completed
				return true
			}

			return false
		})
	}
}
