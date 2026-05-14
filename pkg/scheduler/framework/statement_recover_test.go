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

package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestRecoverOperations_PipelinePreservesEvictionFlag(t *testing.T) {
	jobID := api.JobID("ns/job-recover")
	task := &api.TaskInfo{
		UID:       "t1",
		Job:       jobID,
		Name:      "t1",
		Namespace: "ns",
		Resreq:    (&api.Resource{MilliCPU: 1000}).Clone(),
		InitResreq: (&api.Resource{
			MilliCPU: 1000,
		}).Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "t1",
				Namespace: "ns",
				UID:       types.UID("t1"),
			},
		},
		NumaInfo: &api.TopologyInfo{
			ResMap: map[int]v1.ResourceList{},
		},
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}
	job := api.NewJobInfo(jobID, task)
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	ssn := &Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}

	sourceStmt := NewStatement(ssn)
	assert.NoError(t, sourceStmt.Pipeline(task, node.Name, true))
	plan := SaveOperations(sourceStmt)
	sourceStmt.Discard()

	recoverStmt := NewStatement(ssn)
	assert.NoError(t, recoverStmt.RecoverOperations(plan))

	recoveredTask := ssn.Jobs[jobID].TaskStatusIndex[api.Pipelined][task.UID]
	if assert.NotNil(t, recoveredTask) {
		assert.True(t, recoveredTask.EvictionOccurred)
	}
}
