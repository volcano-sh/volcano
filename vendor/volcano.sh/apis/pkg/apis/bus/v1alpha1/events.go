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

// Event represent the phase of Job, e.g. pod-failed.
type Event string

const (

	// AnyEvent means all event
	AnyEvent Event = "*"

	// PodFailedEvent is triggered if Pod was failed
	PodFailedEvent Event = "PodFailed"

	// PodEvictedEvent is triggered if Pod was deleted
	PodEvictedEvent Event = "PodEvicted"

	// JobUnknownEvent These below are several events can lead to job 'Unknown'
	// 1. Task Unschedulable, this is triggered when part of
	//    pods can't be scheduled while some are already running in gang-scheduling case.
	JobUnknownEvent Event = "Unknown"

	// TaskCompletedEvent is triggered if the 'Replicas' amount of pods in one task are succeed
	TaskCompletedEvent Event = "TaskCompleted"

	// Note: events below are used internally, should not be used by users.

	// OutOfSyncEvent is triggered if Pod/Job is updated(add/update/delete)
	OutOfSyncEvent Event = "OutOfSync"

	// CommandIssuedEvent is triggered if a command is raised by user
	CommandIssuedEvent Event = "CommandIssued"

	// JobUpdatedEvent is triggered if Job is updated, currently only scale up/down
	JobUpdatedEvent Event = "JobUpdated"

	// TaskFailedEvent is triggered when task finished unexpected.
	TaskFailedEvent Event = "TaskFailed"
)
