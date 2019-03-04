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
	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/apis"
)

type pendingState struct {
	job *apis.JobInfo
}

func (ps *pendingState) Execute(action vkv1.Action) error {
	switch action {
	case vkv1.RestartJobAction:
		newJob := ps.job.Clone()
		newJob.StartNewRound(
			vkv1.Restarting,
			fmt.Sprintf("Job Restarted"),
			fmt.Sprintf("Job is restarted in pending state."))
		return ConfigJob(newJob)
	case vkv1.AbortJobAction:
		newJob := ps.job.Clone()
		newJob.AbortCurrentRound(
			vkv1.Aborting,
			fmt.Sprintf("Job aborted"),
			fmt.Sprintf("Job is aborted in pending state."))
		return ConfigJob(newJob)
	default:
		return SyncJob(ps.job, func(status vkv1.JobStatus) vkv1.JobState {
			phase := vkv1.Pending

			if ps.job.Job.Spec.MinAvailable <= status.Running {
				phase = vkv1.Running
			}
			return vkv1.JobState{
				Phase: phase,
			}
		})
	}
}
