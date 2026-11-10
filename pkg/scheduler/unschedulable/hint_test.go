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

package unschedulable

import (
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestComputeSkip(t *testing.T) {
	task := func(id api.TaskID, role string) *api.TaskInfo {
		return &api.TaskInfo{UID: id, TaskRole: role}
	}
	pendingJob := func(minAvailable int32, tasks ...*api.TaskInfo) *api.JobInfo {
		pending := api.TasksMap{}
		for _, task := range tasks {
			pending[task.UID] = task
		}
		return &api.JobInfo{
			MinAvailable:     minAvailable,
			TaskStatusIndex:  map[api.TaskStatus]api.TasksMap{api.Pending: pending},
			TaskMinAvailable: map[string]int32{},
			SubJobs:          map[api.SubJobID]*api.SubJobInfo{},
			MinSubJobs:       map[api.SubJobGID]int32{},
		}
	}

	tests := []struct {
		name       string
		job        *api.JobInfo
		rejections []Rejection
		want       api.SkipDecision
	}{
		{
			name: "enqueue rejection skips enqueue",
			job:  pendingJob(1, task("task-1", "")),
			rejections: []Rejection{{
				Source: RejectionEnqueue,
			}},
			want: api.SkipDecision{Enqueue: true},
		},
		{
			name: "skips rejected task when remaining tasks reach Job minimum",
			job: pendingJob(2,
				task("task-1", ""), task("task-2", ""), task("task-3", "")),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []api.TaskID{"task-1"},
			}},
			want: api.SkipDecision{Tasks: map[api.TaskID]struct{}{"task-1": {}}},
		},
		{
			name: "skips allocation when remaining tasks miss Job minimum",
			job: pendingJob(3,
				task("task-1", ""), task("task-2", ""), task("task-3", "")),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []api.TaskID{"task-1"},
			}},
			want: api.SkipDecision{Allocate: true},
		},
		{
			name: "counts running tasks toward Job minimum",
			job: &api.JobInfo{
				MinAvailable: 2,
				TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
					api.Running: {"running": task("running", "")},
					api.Pending: {
						"rejected": task("rejected", ""),
						"pending":  task("pending", ""),
					},
				},
				TaskMinAvailable: map[string]int32{},
				SubJobs:          map[api.SubJobID]*api.SubJobInfo{},
				MinSubJobs:       map[api.SubJobGID]int32{},
			},
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []api.TaskID{"rejected"},
			}},
			want: api.SkipDecision{Tasks: map[api.TaskID]struct{}{"rejected": {}}},
		},
		{
			name: "skips allocation when a role minimum cannot be reached",
			job: func() *api.JobInfo {
				job := pendingJob(2,
					task("worker-1", "worker"), task("worker-2", "worker"), task("ps", "ps"))
				job.TaskMinAvailable = map[string]int32{"worker": 2}
				job.TaskMinAvailableTotal = 2
				return job
			}(),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []api.TaskID{"worker-1"},
			}},
			want: api.SkipDecision{Allocate: true},
		},
		{
			name: "skips allocation when subgroup minimum cannot be reached",
			job: func() *api.JobInfo {
				tasks := []*api.TaskInfo{
					task("group-a-1", ""), task("group-a-2", ""),
					task("group-b-1", ""), task("group-b-2", ""),
				}
				job := pendingJob(2, tasks...)
				gid := api.SubJobGID("group")
				job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
					"group-a": {
						GID:             gid,
						MinAvailable:    2,
						TaskStatusIndex: map[api.TaskStatus]api.TasksMap{api.Pending: {"group-a-1": tasks[0], "group-a-2": tasks[1]}},
					},
					"group-b": {
						GID:             gid,
						MinAvailable:    2,
						TaskStatusIndex: map[api.TaskStatus]api.TasksMap{api.Pending: {"group-b-1": tasks[2], "group-b-2": tasks[3]}},
					},
				}
				job.MinSubJobs = map[api.SubJobGID]int32{gid: 2}
				return job
			}(),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []api.TaskID{"group-a-1"},
			}},
			want: api.SkipDecision{Allocate: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ComputeSkip(test.job, test.rejections); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ComputeSkip() = %#v, want %#v", got, test.want)
			}
		})
	}
}
