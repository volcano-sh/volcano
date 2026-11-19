/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

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

package reclaim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestIsReclaimVictimTask(t *testing.T) {
	trueValue := true
	falseValue := false

	reclaimorJob := &api.JobInfo{UID: "reclaimor-job", Queue: "q2"}
	victimJob := &api.JobInfo{UID: "victim-job", Name: "victim-job", Queue: "q1"}
	sameQueueJob := &api.JobInfo{UID: "same-queue-job", Name: "same-queue-job", Queue: "q2"}
	nonPreemptableJob := &api.JobInfo{UID: "non-preempt-job", Name: "non-preempt-job", Queue: "q1"}

	reclaimableQueue := &api.QueueInfo{
		UID:   "q1",
		Queue: &scheduling.Queue{Spec: scheduling.QueueSpec{Reclaimable: &trueValue}},
	}
	nonReclaimableQueue := &api.QueueInfo{
		UID:   "q1",
		Queue: &scheduling.Queue{Spec: scheduling.QueueSpec{Reclaimable: &falseValue}},
	}

	baseSSN := func() *framework.Session {
		return &framework.Session{
			Jobs: map[api.JobID]*api.JobInfo{
				victimJob.UID:         victimJob,
				sameQueueJob.UID:      sameQueueJob,
				nonPreemptableJob.UID: nonPreemptableJob,
			},
			Queues: map[api.QueueID]*api.QueueInfo{
				"q1": reclaimableQueue,
				"q2": {UID: "q2"},
			},
		}
	}

	task := func(jobID api.JobID, status api.TaskStatus, preemptable bool) *api.TaskInfo {
		return &api.TaskInfo{
			UID:         api.TaskID(string(jobID) + "-task"),
			Name:        string(jobID) + "-task",
			Job:         jobID,
			Preemptable: preemptable,
			TransactionContext: api.TransactionContext{
				Status: status,
			},
			Pod: &v1.Pod{},
		}
	}

	tests := []struct {
		name string
		ssn  *framework.Session
		task *api.TaskInfo
		want bool
	}{
		{
			name: "running cross-queue preemptable task",
			ssn:  baseSSN(),
			task: task(victimJob.UID, api.Running, true),
			want: true,
		},
		{
			name: "non-running task",
			ssn:  baseSSN(),
			task: task(victimJob.UID, api.Pending, true),
			want: false,
		},
		{
			name: "non-preemptable task",
			ssn:  baseSSN(),
			task: task(victimJob.UID, api.Running, false),
			want: false,
		},
		{
			name: "missing job",
			ssn:  baseSSN(),
			task: task("missing-job", api.Running, true),
			want: false,
		},
		{
			name: "same queue task",
			ssn:  baseSSN(),
			task: task(sameQueueJob.UID, api.Running, true),
			want: false,
		},
		{
			name: "non-reclaimable queue",
			ssn: func() *framework.Session {
				ssn := baseSSN()
				ssn.Queues["q1"] = nonReclaimableQueue
				return ssn
			}(),
			task: task(victimJob.UID, api.Running, true),
			want: false,
		},
		{
			name: "missing queue",
			ssn: func() *framework.Session {
				ssn := baseSSN()
				delete(ssn.Queues, "q1")
				return ssn
			}(),
			task: task(victimJob.UID, api.Running, true),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isReclaimVictimTask(tt.ssn, tt.task, reclaimorJob)
			assert.Equal(t, tt.want, got)
		})
	}
}
