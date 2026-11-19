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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
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

// TestGangOnSessionCloseConditions checks which PodGroup conditions
// OnSessionClose writes. An Inqueue job that was never attempted in this
// session (no fit errors) must not get a spurious Unschedulable condition
// before the real Scheduled one (#4503).
func TestGangOnSessionCloseConditions(t *testing.T) {
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
	newJob := func(minMember int32, phase scheduling.PodGroupPhase, tasks ...*api.TaskInfo) *api.JobInfo {
		job := api.NewJobInfo("test-job", tasks...)
		job.MinAvailable = minMember
		job.PodGroup = &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					MinMember: minMember,
				},
				Status: scheduling.PodGroupStatus{
					Phase: phase,
				},
			},
		}
		return job
	}

	tests := []struct {
		name                string
		job                 *api.JobInfo
		expectUnschedulable bool
		expectScheduled     bool
	}{
		{
			name: "inqueue without fit errors is not marked unschedulable",
			job: newJob(2, scheduling.PodGroupInqueue,
				newTask("task-1", api.Pending, ""),
				newTask("task-2", api.Pending, "")),
		},
		{
			name: "inqueue with node fit errors is marked unschedulable",
			job: func() *api.JobInfo {
				job := newJob(2, scheduling.PodGroupInqueue,
					newTask("task-1", api.Pending, ""),
					newTask("task-2", api.Pending, ""))
				fe := api.NewFitErrors()
				fe.SetNodeError("node-1", errors.New("Insufficient cpu"))
				job.NodesFitErrors[api.TaskID("task-1")] = fe
				return job
			}(),
			expectUnschedulable: true,
		},
		{
			name: "inqueue with job fit errors is marked unschedulable",
			job: func() *api.JobInfo {
				job := newJob(2, scheduling.PodGroupInqueue,
					newTask("task-1", api.Pending, ""),
					newTask("task-2", api.Pending, ""))
				job.JobFitErrors = "insufficient hypernode"
				return job
			}(),
			expectUnschedulable: true,
		},
		{
			name: "pending job rejected by enqueue is still marked unschedulable",
			job: newJob(2, scheduling.PodGroupPending,
				newTask("task-1", api.Pending, ""),
				newTask("task-2", api.Pending, "")),
			expectUnschedulable: true,
		},
		{
			name: "ready inqueue job is marked scheduled",
			job: newJob(2, scheduling.PodGroupInqueue,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Running, "node-2")),
			expectScheduled: true,
		},
		{
			name: "releasing gang keeps the skip over the new guard",
			job: newJob(2, scheduling.PodGroupInqueue,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Releasing, "node-2")),
		},
		{
			name: "running plus pending without fit errors is not marked unschedulable",
			job: newJob(2, scheduling.PodGroupInqueue,
				newTask("task-1", api.Running, "node-1"),
				newTask("task-2", api.Pending, "")),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")
			defer cache.ShutdownMockSchedulerCache(schedulerCache)
			ssn := framework.OpenSession(schedulerCache, nil, nil)
			ssn.Jobs[tc.job.UID] = tc.job

			gp := New(framework.Arguments{})
			gp.(*gangPlugin).OnSessionClose(ssn)

			var unschedulable, scheduled bool
			for _, cond := range tc.job.PodGroup.Status.Conditions {
				switch cond.Type {
				case scheduling.PodGroupUnschedulableType:
					unschedulable = true
				case scheduling.PodGroupScheduled:
					scheduled = true
				}
			}
			assert.Equal(t, tc.expectUnschedulable, unschedulable, "Unschedulable condition mismatch")
			assert.Equal(t, tc.expectScheduled, scheduled, "Scheduled condition mismatch")

			// No framework.CloseSession here: no action ran on this session and
			// the close path (job status/queue writeback) needs a populated
			// mock cache; ShutdownMockSchedulerCache above covers cleanup.
		})
	}
}
