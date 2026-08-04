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

package api

import (
	"reflect"
	"testing"
)

func TestComputeSkip(t *testing.T) {
	task := func(id TaskID, role string) *TaskInfo {
		return &TaskInfo{UID: id, TaskRole: role}
	}
	pendingJob := func(minAvailable int32, tasks ...*TaskInfo) *JobInfo {
		pending := TasksMap{}
		for _, task := range tasks {
			pending[task.UID] = task
		}
		return &JobInfo{
			MinAvailable:     minAvailable,
			TaskStatusIndex:  map[TaskStatus]TasksMap{Pending: pending},
			TaskMinAvailable: map[string]int32{},
			SubJobs:          map[SubJobID]*SubJobInfo{},
			MinSubJobs:       map[SubJobGID]int32{},
		}
	}

	tests := []struct {
		name       string
		job        *JobInfo
		rejections []Rejection
		want       SkipDecision
	}{
		{
			name: "enqueue rejection skips enqueue",
			job:  pendingJob(1, task("task-1", "")),
			rejections: []Rejection{{
				Source: RejectionEnqueue,
			}},
			want: SkipDecision{Enqueue: true},
		},
		{
			name: "skips rejected task when remaining tasks reach Job minimum",
			job: pendingJob(2,
				task("task-1", ""), task("task-2", ""), task("task-3", "")),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []TaskID{"task-1"},
			}},
			want: SkipDecision{Tasks: map[TaskID]struct{}{"task-1": {}}},
		},
		{
			name: "skips allocation when remaining tasks miss Job minimum",
			job: pendingJob(3,
				task("task-1", ""), task("task-2", ""), task("task-3", "")),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []TaskID{"task-1"},
			}},
			want: SkipDecision{Allocate: true},
		},
		{
			name: "counts running tasks toward Job minimum",
			job: &JobInfo{
				MinAvailable: 2,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Running: {"running": task("running", "")},
					Pending: {
						"rejected": task("rejected", ""),
						"pending":  task("pending", ""),
					},
				},
				TaskMinAvailable: map[string]int32{},
				SubJobs:          map[SubJobID]*SubJobInfo{},
				MinSubJobs:       map[SubJobGID]int32{},
			},
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []TaskID{"rejected"},
			}},
			want: SkipDecision{Tasks: map[TaskID]struct{}{"rejected": {}}},
		},
		{
			name: "skips allocation when a role minimum cannot be reached",
			job: func() *JobInfo {
				job := pendingJob(2,
					task("worker-1", "worker"), task("worker-2", "worker"), task("ps", "ps"))
				job.TaskMinAvailable = map[string]int32{"worker": 2}
				job.TaskMinAvailableTotal = 2
				return job
			}(),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []TaskID{"worker-1"},
			}},
			want: SkipDecision{Allocate: true},
		},
		{
			name: "skips allocation when subgroup minimum cannot be reached",
			job: func() *JobInfo {
				tasks := []*TaskInfo{
					task("group-a-1", ""), task("group-a-2", ""),
					task("group-b-1", ""), task("group-b-2", ""),
				}
				job := pendingJob(2, tasks...)
				gid := SubJobGID("group")
				job.SubJobs = map[SubJobID]*SubJobInfo{
					"group-a": {
						GID:             gid,
						MinAvailable:    2,
						TaskStatusIndex: map[TaskStatus]TasksMap{Pending: {"group-a-1": tasks[0], "group-a-2": tasks[1]}},
					},
					"group-b": {
						GID:             gid,
						MinAvailable:    2,
						TaskStatusIndex: map[TaskStatus]TasksMap{Pending: {"group-b-1": tasks[2], "group-b-2": tasks[3]}},
					},
				}
				job.MinSubJobs = map[SubJobGID]int32{gid: 2}
				return job
			}(),
			rejections: []Rejection{{
				Source: RejectionPredicate,
				Tasks:  []TaskID{"group-a-1"},
			}},
			want: SkipDecision{Allocate: true},
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
