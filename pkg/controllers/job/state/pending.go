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
	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

type pendingState struct {
	job *apis.JobInfo
}

func (ps *pendingState) Execute(action Action) error {
	switch action.Action {
	case v1alpha1.RestartJobAction:
		return KillJob(ps.job, PodRetainPhaseNone, func(status *vcbatch.JobStatus) bool {
			status.RetryCount++
			status.State.Phase = vcbatch.Restarting
			return true
		})
	case v1alpha1.RestartTaskAction, v1alpha1.RestartPodAction, v1alpha1.RestartPartitionAction:
		return KillTarget(ps.job, action.Target, func(status *vcbatch.JobStatus) bool {
			status.RetryCount++
			status.State.Phase = vcbatch.Restarting
			return true
		})
	case v1alpha1.AbortJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Aborting
			return true
		})
	case v1alpha1.CompleteJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Completing
			return true
		})
	case v1alpha1.TerminateJobAction:
		return KillJob(ps.job, PodRetainPhaseSoft, func(status *vcbatch.JobStatus) bool {
			status.State.Phase = vcbatch.Terminating
			return true
		})
	default:
		return SyncJob(ps.job, func(status *vcbatch.JobStatus) bool {
			if ps.job.Job.Spec.MinAvailable <= status.Running+status.Succeeded+status.Failed {
				status.State.Phase = vcbatch.Running
				return true
			}
			return false
		})
	}
}
