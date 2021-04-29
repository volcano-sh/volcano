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
	v1 "k8s.io/api/core/v1"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

//PhaseMap to store the pod phases.
type PhaseMap map[v1.PodPhase]struct{}

//UpdateStatusFn updates the job status.
type UpdateStatusFn func(status *vcbatch.JobStatus) (jobPhaseChanged bool)

//ActionFn will create or delete Pods according to Job's spec.
type ActionFn func(job *apis.JobInfo, fn UpdateStatusFn) error

//KillActionFn kill all Pods of Job with phase not in podRetainPhase.
type KillActionFn func(job *apis.JobInfo, podRetainPhase PhaseMap, fn UpdateStatusFn) error

//PodRetainPhaseNone stores no phase.
var PodRetainPhaseNone = PhaseMap{}

// PodRetainPhaseSoft stores PodSucceeded and PodFailed Phase.
var PodRetainPhaseSoft = PhaseMap{
	v1.PodSucceeded: {},
	v1.PodFailed:    {},
}

var (
	// SyncJob will create or delete Pods according to Job's spec.
	SyncJob ActionFn
	// KillJob kill all Pods of Job with phase not in podRetainPhase.
	KillJob KillActionFn
)

//State interface.
type State interface {
	// Execute executes the actions based on current state.
	Execute(act v1alpha1.Action) error
}

// NewState gets the state from the volcano job Phase.
func NewState(jobInfo *apis.JobInfo) State {
	job := jobInfo.Job
	switch job.Status.State.Phase {
	case vcbatch.Pending:
		return &pendingState{job: jobInfo}
	case vcbatch.Running:
		return &runningState{job: jobInfo}
	case vcbatch.Restarting:
		return &restartingState{job: jobInfo}
	case vcbatch.Terminated, vcbatch.Completed, vcbatch.Failed:
		return &finishedState{job: jobInfo}
	case vcbatch.Terminating:
		return &terminatingState{job: jobInfo}
	case vcbatch.Aborting:
		return &abortingState{job: jobInfo}
	case vcbatch.Aborted:
		return &abortedState{job: jobInfo}
	case vcbatch.Completing:
		return &completingState{job: jobInfo}
	}

	// It's pending by default.
	return &pendingState{job: jobInfo}
}
