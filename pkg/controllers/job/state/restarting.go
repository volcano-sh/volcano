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
	"fmt"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

type restartingState struct {
	job *apis.JobInfo
}

func (ps *restartingState) restartingUpdateStatus(status *vcbatch.JobStatus) bool {
	// Get the maximum number of retries.
	maxRetry := ps.job.Job.Spec.MaxRetry

	if status.RetryCount >= maxRetry {
		// Failed is the phase that the job is restarted failed reached the maximum number of retries.
		status.State.Phase = vcbatch.Failed
		UpdateJobFailed(fmt.Sprintf("%s/%s", ps.job.Job.Namespace, ps.job.Job.Name), ps.job.Job.Spec.Queue)
		return true
	}
	total := int32(0)
	for _, task := range ps.job.Job.Spec.Tasks {
		total += task.Replicas
	}

	if total-status.Terminating >= status.MinAvailable {
		status.State.Phase = vcbatch.Pending
		return true
	}

	return false
}

func (ps *restartingState) Execute(action Action) error {
	switch action.Action {
	case v1alpha1.SyncJobAction:
		return SyncJob(ps.job, ps.restartingUpdateStatus)
	case v1alpha1.RestartTaskAction, v1alpha1.RestartPodAction, v1alpha1.RestartPartitionAction:
		return KillTarget(ps.job, action.Target, ps.restartingUpdateStatus)
	default:
		// A late/duplicate RestartJob (e.g. from PodEvicted) must not re-run a
		// full KillJob once pods are already gone — that used to delete a
		// freshly recreated PodGroup and permanently wedge the job (#6037).
		if !hasLivePods(ps.job) {
			return SyncJob(ps.job, ps.restartingUpdateStatus)
		}
		return KillJob(ps.job, PodRetainPhaseNone, ps.restartingUpdateStatus)
	}
}

// hasLivePods reports whether the job still has pods that are not terminating.
func hasLivePods(job *apis.JobInfo) bool {
	for _, pods := range job.Pods {
		for _, pod := range pods {
			if pod != nil && pod.DeletionTimestamp == nil {
				return true
			}
		}
	}
	return false
}
