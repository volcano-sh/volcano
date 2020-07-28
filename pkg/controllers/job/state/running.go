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
	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

type runningState struct {
	job *apis.JobInfo
}

func (ps *runningState) Execute(action v1alpha1.Action) error {
	switch action {
	case v1alpha1.RestartJobAction:
		return KillJob(ps.job, PodRetainPhaseNone, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Restarting
			status.RetryCount++
			return true
		})
	case v1alpha1.AbortJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Aborting
			return true
		})
	case v1alpha1.TerminateJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Terminating
			return true
		})
	case v1alpha1.CompleteJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Completing
			return true
		})
	default:
		return SyncJob(ps.job, func(status *vcbatch.JobStatus) bool {
			jobReplicas := TotalTasks(ps.job.Job)
			if jobReplicas == 0 {
				// when scale down to zero, keep the current job phase
				return false
			}
			if status.Succeeded+status.Failed == jobReplicas {
				if status.Succeeded >= ps.job.Job.Spec.MinAvailable {
					status.State.Phase = vcbatch.Completed
				} else {
					status.State.Phase = vcbatch.Failed
				}
				return true
			}
			return false
		})
	}
}
