/*
Copyright 2022 The Volcano Authors.

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
	jobflowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
)

type pendingState struct {
	jobFlow *jobflowv1alpha1.JobFlow
}

func (p *pendingState) Execute(action jobflowv1alpha1.Action) error {
	switch action {
	case jobflowv1alpha1.SyncJobFlowAction:
		return SyncJobFlow(p.jobFlow, func(status *jobflowv1alpha1.JobFlowStatus, allJobList int) {
			switch {
			// Short jobs can finish before any sync observes them running, and the
			// workqueue collapses the two job events into one. Settling on Running
			// there would be terminal: a status write only re-enqueues the jobflow
			// once the phase is already Succeed, and a finished job sends no more
			// events. The running state reaches the same conclusion from the same
			// counts.
			case len(status.CompletedJobs) == allJobList:
				UpdateJobFlowSucceed(p.jobFlow.Namespace)
				status.State.Phase = jobflowv1alpha1.Succeed
			case len(status.FailedJobs) > 0 || len(status.TerminatedJobs) > 0: // TODO(dongjiang1989) Modify it when the if condition judgment is implemented
				UpdateJobFlowFailed(p.jobFlow.Namespace)
				status.State.Phase = jobflowv1alpha1.Failed
			case len(status.RunningJobs) > 0 || len(status.CompletedJobs) > 0:
				status.State.Phase = jobflowv1alpha1.Running
			default:
				status.State.Phase = jobflowv1alpha1.Pending
			}
		})
	}
	return nil
}
