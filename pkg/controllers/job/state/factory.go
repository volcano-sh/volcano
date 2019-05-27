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
	"k8s.io/api/core/v1"

	vkv1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

type PhaseMap map[v1.PodPhase]struct{}
type UpdateStatusFn func(status *vkv1.JobStatus) (jobPhaseChanged bool)
type ActionFn func(job *apis.JobInfo, fn UpdateStatusFn) error
type KillActionFn func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error

var PodRetainPhaseNone = PhaseMap{}
var PodRetainPhaseSoft = PhaseMap{
	v1.PodSucceeded: {},
	v1.PodFailed:    {},
}

var (
	// SyncJob will create or delete Pods according to Job's spec.
	SyncJob ActionFn
	// KillJob kill all Pods of Job with phase not in podRetainPhase.
	KillJob KillActionFn
	// CreateJob will prepare to create Job.
	CreateJob ActionFn
)

type State interface {
	// Execute executes the actions based on current state.
	Execute(act vkv1.Action) error
}

func NewState(jobInfo *apis.JobInfo) State {
	job := jobInfo.Job
	switch job.Status.State.Phase {
	case vkv1.Pending:
		return &pendingState{job: jobInfo}
	case vkv1.Running:
		return &runningState{job: jobInfo}
	case vkv1.Restarting:
		return &restartingState{job: jobInfo}
	case vkv1.Terminated, vkv1.Completed, vkv1.Failed:
		return &finishedState{job: jobInfo}
	case vkv1.Terminating:
		return &terminatingState{job: jobInfo}
	case vkv1.Aborting:
		return &abortingState{job: jobInfo}
	case vkv1.Aborted:
		return &abortedState{job: jobInfo}
	case vkv1.Completing:
		return &completingState{job: jobInfo}
	case vkv1.Inqueue:
		return &inqueueState{job: jobInfo}
	}

	// It's pending by default.
	return &pendingState{job: jobInfo}
}
