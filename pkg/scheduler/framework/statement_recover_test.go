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

// TestRecoverOperations_PartialFailureLeavesOperationsApplied documents the
// invariant that callers of RecoverOperations depend on: the operations are
// replayed one at a time, so a failure part way through leaves the earlier ones
// applied to the session and recorded on the recovering statement. The caller
// must Discard() them, otherwise they stay orphaned and corrupt node accounting
// for the rest of the scheduling cycle.
func TestRecoverOperations_PartialFailureLeavesOperationsApplied(t *testing.T) {
	ssn, _, task, node := newTestSession(t)

	// Build a plan that allocates the task, then record the node's idle capacity
	// so the rollback can be checked against it.
	idleBefore := node.Idle.Clone()

	sourceStmt := NewStatement(ssn)
	if err := sourceStmt.Allocate(task, node); err != nil {
		t.Fatalf("expected Allocate to succeed, got: %v", err)
	}
	plan := SaveOperations(sourceStmt)
	sourceStmt.Discard()

	// Append a second operation targeting a node that is not in the session, so
	// replay fails only after the first operation has already been applied. A
	// Pipeline op is used because it resolves the node by name and reports a clean
	// error, and its rollback touches only its own task.
	orphanPod := task.Pod.DeepCopy()
	orphanPod.Name = "orphan-pod"
	orphanPod.UID = types.UID("orphan-pod")
	plan.operations = append(plan.operations, operation{
		name: Pipeline,
		task: &api.TaskInfo{
			UID:       "orphan-task",
			Job:       task.Job,
			Name:      "orphan-task",
			Namespace: task.Namespace,
			Resreq:    (&api.Resource{MilliCPU: 1000}).Clone(),
			Pod:       orphanPod,
			TransactionContext: api.TransactionContext{
				NodeName: "nonexistent-node",
				Status:   api.Pending,
			},
		},
	})

	recoverStmt := NewStatement(ssn)
	err := recoverStmt.RecoverOperations(plan)
	if err == nil {
		t.Fatal("expected RecoverOperations to fail on the unresolvable node")
	}

	// The first operation is applied and recorded even though the call failed.
	if len(recoverStmt.operations) == 0 {
		t.Fatal("expected the successfully replayed operation to remain recorded on the statement")
	}
	if node.Idle.Equal(idleBefore, api.Zero) {
		t.Fatal("expected node idle resources to be reduced by the applied operation")
	}

	// Only Discard puts the session back; without it these mutations are orphaned.
	recoverStmt.Discard()

	if !node.Idle.Equal(idleBefore, api.Zero) {
		t.Errorf("expected node idle resources restored after Discard, got %v want %v", node.Idle, idleBefore)
	}
	if task.Status != api.Pending {
		t.Errorf("expected task status Pending after Discard, got %v", task.Status)
	}
}
