/*
Copyright 2020 The Volcano Authors.

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

package v1alpha1

// Action is the action that Job controller will take according to the event.
type Action string

const (

	// AbortJobAction if this action is set, the whole job will be aborted:
	// all Pod of Job will be evicted, and no Pod will be recreated
	AbortJobAction Action = "AbortJob"

	// RestartJobAction if this action is set, the whole job will be restarted
	RestartJobAction Action = "RestartJob"

	// RestartTaskAction if this action is set, only the task will be restarted; default action.
	// This action can not work together with job level events, e.g. JobUnschedulable
	RestartTaskAction Action = "RestartTask"

	// TerminateJobAction if this action is set, the whole job wil be terminated
	// and can not be resumed: all Pod of Job will be evicted, and no Pod will be recreated.
	TerminateJobAction Action = "TerminateJob"

	// CompleteJobAction if this action is set, the unfinished pods will be killed, job completed.
	CompleteJobAction Action = "CompleteJob"

	// ResumeJobAction is the action to resume an aborted job.
	ResumeJobAction Action = "ResumeJob"

	// Note: actions below are only used internally, should not be used by users.

	// SyncJobAction is the action to sync Job/Pod status.
	SyncJobAction Action = "SyncJob"

	// EnqueueAction is the action to sync Job inqueue status.
	EnqueueAction Action = "EnqueueJob"

	// SyncQueueAction is the action to sync queue status.
	SyncQueueAction Action = "SyncQueue"

	// OpenQueueAction is the action to open queue
	OpenQueueAction Action = "OpenQueue"

	// CloseQueueAction is the action to close queue
	CloseQueueAction Action = "CloseQueue"
)
