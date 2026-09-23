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

// TestRecoverOperations_DiscardRollsBackPartialRecovery verifies that when
// RecoverOperations fails partway through, the operations it already applied
// remain in the statement and mutate session state, and that Discard rolls
// them back. This is the contract the allocate action relies on: it must call
// Discard on the finalStmt when RecoverOperations returns an error, otherwise
// the partially-applied operations leak into the session.
func TestRecoverOperations_DiscardRollsBackPartialRecovery(t *testing.T) {
	newTask := func(uid, job string) *api.TaskInfo {
		return &api.TaskInfo{
			UID:        api.TaskID(uid),
			Job:        api.JobID(job),
			Name:       uid,
			Namespace:  "ns",
			Resreq:     (&api.Resource{MilliCPU: 1000}).Clone(),
			InitResreq: (&api.Resource{MilliCPU: 1000}).Clone(),
			Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: uid, Namespace: "ns", UID: types.UID(uid)},
			},
			NumaInfo:           &api.TopologyInfo{ResMap: map[int]v1.ResourceList{}},
			TransactionContext: api.TransactionContext{Status: api.Pending},
		}
	}

	const nodeIdle = float64(4000)

	jobAID, jobBID := api.JobID("ns/jobA"), api.JobID("ns/jobB")
	taskA, taskB := newTask("a", string(jobAID)), newTask("b", string(jobBID))
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: nodeIdle}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	ssn := &Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobAID: api.NewJobInfo(jobAID, taskA), jobBID: api.NewJobInfo(jobBID, taskB)},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}

	// Build a plan that allocates both tasks, then discard the source statement.
	sourceStmt := NewStatement(ssn)
	assert.NoError(t, sourceStmt.Allocate(taskA, node))
	assert.NoError(t, sourceStmt.Allocate(taskB, node))
	plan := SaveOperations(sourceStmt)
	sourceStmt.Discard()

	// Remove jobB so recovering its allocation fails after taskA is recovered.
	delete(ssn.Jobs, jobBID)

	finalStmt := NewStatement(ssn)
	err := finalStmt.RecoverOperations(plan)
	assert.Error(t, err, "RecoverOperations should fail when a task's job is missing")

	// taskA was recovered before the failure, so it is left allocated in the
	// session: the partially-applied operation is still live.
	assert.NotNil(t, ssn.Jobs[jobAID].TaskStatusIndex[api.Allocated][taskA.UID],
		"partial recovery should leave taskA allocated in the session")

	// Discarding rolls the partial recovery back, restoring taskA to Pending.
	finalStmt.Discard()
	assert.Nil(t, ssn.Jobs[jobAID].TaskStatusIndex[api.Allocated][taskA.UID],
		"Discard should roll back the partially-recovered allocation")
}
