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

// TestRecoverOperations_DiscardRollsBackOnPartialFailure is a regression test for
// issue #5362 (and similar in gangpreempt/gangreclaim): when RecoverOperations fails partway through
// replaying operations (e.g. an Evict succeeds but a subsequent Pipeline fails because the target node
// is not present in the session), the caller must call Discard() on the partially-populated
// statement to roll back the already-applied session mutations.
func TestRecoverOperations_DiscardRollsBackOnPartialFailure(t *testing.T) {
	jobID := api.JobID("ns/job-partial")

	// victim: a running task that will be evicted in the saved plan.
	victim := &api.TaskInfo{
		UID:       "victim",
		Job:       jobID,
		Name:      "victim",
		Namespace: "ns",
		Resreq:    (&api.Resource{MilliCPU: 500}).Clone(),
		InitResreq: (&api.Resource{
			MilliCPU: 500,
		}).Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "victim",
				Namespace: "ns",
				UID:       types.UID("victim"),
			},
		},
		NumaInfo: &api.TopologyInfo{ResMap: map[int]v1.ResourceList{}},
		TransactionContext: api.TransactionContext{
			Status:   api.Running,
			NodeName: "n1",
		},
	}

	// preemptor: a pending task that will be pipelined to a node that does NOT
	// exist in the recovery session, forcing RecoverOperations to return an error.
	preemptor := &api.TaskInfo{
		UID:       "preemptor",
		Job:       jobID,
		Name:      "preemptor",
		Namespace: "ns",
		Resreq:    (&api.Resource{MilliCPU: 500}).Clone(),
		InitResreq: (&api.Resource{
			MilliCPU: 500,
		}).Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "preemptor",
				Namespace: "ns",
				UID:       types.UID("preemptor"),
			},
		},
		NumaInfo: &api.TopologyInfo{ResMap: map[int]v1.ResourceList{}},
		TransactionContext: api.TransactionContext{
			Status:   api.Pending,
			NodeName: "ghost-node", // node absent from session — Pipeline will fail
		},
	}

	job := api.NewJobInfo(jobID, victim, preemptor)

	// Only n1 is in the session; ghost-node is intentionally absent.
	n1 := api.NewNodeInfo(nil)
	n1.Name = "n1"
	n1.Idle = (&api.Resource{MilliCPU: 1000}).Clone()
	n1.Releasing = api.EmptyResource()
	n1.Pipelined = api.EmptyResource()
	assert.NoError(t, n1.AddTask(victim))

	ssn := &Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job},
		Nodes: map[string]*api.NodeInfo{"n1": n1},
	}

	// Build a saved plan that first evicts the victim then pipelines the preemptor
	// to a node that does not exist in the session.
	plan := &Statement{
		operations: []operation{
			{name: Evict, task: victim.Clone(), reason: "preempt"},
			{name: Pipeline, task: preemptor.Clone(), reason: ""},
		},
	}

	// Apply the plan. The Evict succeeds (victim -> Releasing) but the Pipeline
	// fails because "ghost-node" is absent, so RecoverOperations returns an error.
	finalStmt := NewStatement(ssn)
	err := finalStmt.RecoverOperations(plan)
	assert.Error(t, err, "RecoverOperations should fail because ghost-node is missing")

	// Verify the session is dirty before Discard
	releasingVictim := job.TaskStatusIndex[api.Releasing][victim.UID]
	assert.NotNil(t, releasingVictim, "victim must be in Releasing state before Discard")

	// Discard() reverses the eviction, restoring victim to Running.
	finalStmt.Discard()

	// The victim must be back in the Running state after Discard().
	restoredVictim := job.TaskStatusIndex[api.Running][victim.UID]
	assert.NotNil(t, restoredVictim,
		"victim must be in Running state after Discard() rolls back the partial eviction")

	// The victim must NOT remain in the Releasing state.
	assert.Nil(t, job.TaskStatusIndex[api.Releasing][victim.UID],
		"victim must not remain in Releasing state after Discard()")
}
