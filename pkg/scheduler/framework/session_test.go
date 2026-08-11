/*
Copyright 2019 The Volcano Authors.

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
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestGetPodGroupPhase(t *testing.T) {
	newJob := func(minMember int32, currentPhase scheduling.PodGroupPhase, tasks ...*api.TaskInfo) *api.JobInfo {
		job := api.NewJobInfo("test-job", tasks...)
		job.PodGroup = &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					MinMember: minMember,
				},
				Status: scheduling.PodGroupStatus{
					Phase: currentPhase,
				},
			},
		}
		return job
	}
	newTask := func(name string, status api.TaskStatus, nodeName string) *api.TaskInfo {
		return &api.TaskInfo{
			UID: api.TaskID(name),
			TransactionContext: api.TransactionContext{
				Status:   status,
				NodeName: nodeName,
			},
			Resreq: api.EmptyResource(),
		}
	}

	tests := []struct {
		name          string
		job           *api.JobInfo
		unschedulable bool
		expected      scheduling.PodGroupPhase
	}{
		{
			name: "single pod terminating keeps Running",
			job: newJob(1, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "multi pod partial terminating keeps Running",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Releasing, "node-2")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "all pods terminating keeps Running",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1"),
				newTask("task-2", api.Releasing, "node-2")),
			expected: scheduling.PodGroupRunning,
		},
		{
			name: "never-scheduled pending pod deleted stays Pending",
			job: newJob(2, scheduling.PodGroupPending,
				newTask("task-1", api.Releasing, ""),
				newTask("task-2", api.Pending, "")),
			expected: scheduling.PodGroupPending,
		},
		{
			name: "all scheduled tasks completed",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Succeeded, "node-1"),
				newTask("task-2", api.Succeeded, "node-2")),
			expected: scheduling.PodGroupCompleted,
		},
		{
			name: "scheduled releasing tasks below minMember fall to Pending",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Releasing, "node-1")),
			expected: scheduling.PodGroupPending,
		},
		{
			name: "running and unschedulable is Unknown",
			job: newJob(2, scheduling.PodGroupRunning,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Pending, "")),
			unschedulable: true,
			expected:      scheduling.PodGroupUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getPodGroupPhase(tc.job, tc.unschedulable)
			assert.Equal(t, tc.expected, got)
		})
	}
}
