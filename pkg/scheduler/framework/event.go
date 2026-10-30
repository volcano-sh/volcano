/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2024 The Volcano Authors.

Modifications made by Volcano authors:
- Added Err field to Event structure for error handling

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

package framework

import (
	"volcano.sh/volcano/pkg/scheduler/api"
)

// Event structure
type Event struct {
	Task *api.TaskInfo
	Err  error
	// Restore is set when the event puts a task back after an operation was
	// rolled back, for example when a discarded statement unevicts a victim.
	// The task is still bound to the node it was running on, so handlers must
	// restore their own state only and skip the work they did when the task
	// was first admitted to the node.
	Restore bool
}

// EventHandler structure
type EventHandler struct {
	AllocateFunc   func(event *Event)
	DeallocateFunc func(event *Event)
}
