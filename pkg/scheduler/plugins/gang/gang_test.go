/*
Copyright 2026 The Volcano Authors.

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

package gang

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestShouldSkipUnschedulableForReleasing(t *testing.T) {
	newTask := func(uid string, status api.TaskStatus, nodeName string) *api.TaskInfo {
		return &api.TaskInfo{
			UID: api.TaskID(uid),
			TransactionContext: api.TransactionContext{
				Status:   status,
				NodeName: nodeName,
			},
			Resreq: api.EmptyResource(),
		}
	}

	tests := []struct {
		name     string
		min      int32
		tasks    []*api.TaskInfo
		expected bool
	}{
		{
			name: "partial termination",
			min:  2,
			tasks: []*api.TaskInfo{
				newTask("running", api.Running, "node-1"),
				newTask("releasing", api.Releasing, "node-2"),
			},
			expected: true,
		},
		{
			name: "pending replacement still needs condition",
			min:  2,
			tasks: []*api.TaskInfo{
				newTask("running", api.Running, "node-1"),
				newTask("releasing", api.Releasing, "node-2"),
				newTask("replacement", api.Pending, ""),
			},
		},
		{
			name: "pipelined replacement still needs condition",
			min:  2,
			tasks: []*api.TaskInfo{
				newTask("running", api.Running, "node-1"),
				newTask("releasing", api.Releasing, "node-2"),
				newTask("replacement", api.Pipelined, "node-3"),
			},
		},
		{
			name: "never scheduled releasing task is not counted",
			min:  1,
			tasks: []*api.TaskInfo{
				newTask("releasing", api.Releasing, ""),
			},
		},
		{
			name: "scheduled tasks below minAvailable",
			min:  3,
			tasks: []*api.TaskInfo{
				newTask("running", api.Running, "node-1"),
				newTask("releasing", api.Releasing, "node-2"),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			job := api.NewJobInfo("job-1", tc.tasks...)
			job.MinAvailable = tc.min
			if got := shouldSkipUnschedulableForReleasing(job); got != tc.expected {
				t.Fatalf("shouldSkipUnschedulableForReleasing() = %v, want %v", got, tc.expected)
			}
		})
	}
}
