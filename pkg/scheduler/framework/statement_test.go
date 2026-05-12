/*
Copyright 2025 The Volcano Authors.

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

	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestStatementMerge(t *testing.T) {
	makeTask := func(name string) *api.TaskInfo {
		return &api.TaskInfo{
			Name: name,
		}
	}

	t.Run("merge transfers operations from source to target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
				{name: Pipeline, task: makeTask("t2")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 2 {
			t.Fatalf("expected 2 operations in target, got %d", len(target.operations))
		}
		if target.operations[0].task.Name != "t1" || target.operations[0].name != Evict {
			t.Errorf("first operation mismatch: got %v", target.operations[0])
		}
		if target.operations[1].task.Name != "t2" || target.operations[1].name != Pipeline {
			t.Errorf("second operation mismatch: got %v", target.operations[1])
		}
	})

	t.Run("source operations cleared after merge", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge(source)

		if source.operations != nil {
			t.Errorf("expected source operations to be nil after merge, got %v", source.operations)
		}
	})

	t.Run("merge multiple sources", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Allocate, task: makeTask("t0")},
			},
		}
		src1 := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		src2 := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t2")},
				{name: Evict, task: makeTask("t3"), reason: "preempt"},
			},
		}

		target.Merge(src1, src2)

		if len(target.operations) != 4 {
			t.Fatalf("expected 4 operations in target, got %d", len(target.operations))
		}
		// Verify order: existing target ops first, then src1, then src2
		expectedNames := []string{"t0", "t1", "t2", "t3"}
		for i, name := range expectedNames {
			if target.operations[i].task.Name != name {
				t.Errorf("operation %d: expected task %s, got %s", i, name, target.operations[i].task.Name)
			}
		}
		if src1.operations != nil {
			t.Errorf("expected src1 operations to be nil after merge")
		}
		if src2.operations != nil {
			t.Errorf("expected src2 operations to be nil after merge")
		}
	})

	t.Run("merge empty source is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		empty := &Statement{}

		target.Merge(empty)

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation in target after merging empty, got %d", len(target.operations))
		}
	})

	t.Run("merge into empty target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t1")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 1 {
			t.Fatalf("expected 1 operation in target, got %d", len(target.operations))
		}
		if target.operations[0].task.Name != "t1" {
			t.Errorf("expected task t1, got %s", target.operations[0].task.Name)
		}
	})

	t.Run("merge with no arguments is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge()

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation after no-arg merge, got %d", len(target.operations))
		}
	})
}

// newTestSession creates a minimal session with one queue, one job (with a pending task), and one node.
// The returned task is in Pending status and belongs to the job.
func newTestSession(t *testing.T) (ssn *Session, job *api.JobInfo, task *api.TaskInfo, node *api.NodeInfo) {
	t.Helper()
	scherCache := cache.NewDefaultMockSchedulerCache("test-scheduler")

	scherCache.AddOrUpdateNode(
		util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil),
	)
	scherCache.AddPod(
		util.BuildPod("ns1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
	)
	scherCache.AddPodGroupV1beta1(
		util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue),
	)
	scherCache.AddQueueV1beta1(
		util.BuildQueue("q1", 1, nil),
	)

	ssn = OpenSession(scherCache, nil, nil)
	t.Cleanup(func() { CloseSession(ssn) })

	for _, j := range ssn.Jobs {
		job = j
		break
	}
	if job == nil {
		t.Fatal("no job found in session")
	}
	for _, tk := range job.Tasks {
		task = tk
		break
	}
	if task == nil {
		t.Fatal("no task found in job")
	}
	for _, nd := range ssn.Nodes {
		node = nd
		break
	}
	return
}

func TestPipelineAutoRollbackOnError(t *testing.T) {
	t.Run("successful pipeline records operation", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		err := stmt.Pipeline(task, node.Name, false)
		if err != nil {
			t.Fatalf("expected Pipeline to succeed, got: %v", err)
		}
		if len(stmt.operations) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(stmt.operations))
		}
		if stmt.operations[0].name != Pipeline {
			t.Errorf("expected Pipeline operation, got %v", stmt.operations[0].name)
		}
		if task.NodeName != node.Name {
			t.Errorf("expected task on node %s, got %s", node.Name, task.NodeName)
		}
	})

	t.Run("pipeline to missing node returns error and rolls back", func(t *testing.T) {
		ssn, job, task, _ := newTestSession(t)
		stmt := NewStatement(ssn)

		err := stmt.Pipeline(task, "nonexistent-node", false)
		if err == nil {
			t.Fatal("expected Pipeline to fail for nonexistent node")
		}

		// Auto-rollback should ensure no operations are recorded.
		if len(stmt.operations) != 0 {
			t.Errorf("expected 0 operations after rollback, got %d", len(stmt.operations))
		}
		// Task should have been rolled back to Pending status.
		if task.Status != api.Pending {
			t.Errorf("expected task status Pending after rollback, got %v", task.Status)
		}
		// Task should not be assigned to any node after rollback.
		if task.NodeName != "" {
			t.Errorf("expected empty NodeName after rollback, got %s", task.NodeName)
		}
		// Job should still know about the task.
		if _, found := job.Tasks[task.UID]; !found {
			t.Error("task should still exist in the job after rollback")
		}
	})

	t.Run("pipeline to missing job returns error and rolls back", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		// Remove the job from the session to trigger the "job not found" error path.
		delete(ssn.Jobs, task.Job)

		err := stmt.Pipeline(task, node.Name, false)
		if err == nil {
			t.Fatal("expected Pipeline to fail when job is missing")
		}
		if len(stmt.operations) != 0 {
			t.Errorf("expected 0 operations after rollback, got %d", len(stmt.operations))
		}
	})
}

func TestAllocateAutoRollbackOnError(t *testing.T) {
	t.Run("successful allocate records operation", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		err := stmt.Allocate(task, node)
		if err != nil {
			t.Fatalf("expected Allocate to succeed, got: %v", err)
		}
		if len(stmt.operations) != 1 {
			t.Fatalf("expected 1 operation, got %d", len(stmt.operations))
		}
		if stmt.operations[0].name != Allocate {
			t.Errorf("expected Allocate operation, got %v", stmt.operations[0].name)
		}
		if task.NodeName != node.Name {
			t.Errorf("expected task on node %s, got %s", node.Name, task.NodeName)
		}
	})

	t.Run("allocate to missing node returns error and rolls back", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		// Remove the node from the session to trigger the "node not found" error path.
		delete(ssn.Nodes, node.Name)
		fakeNode := &api.NodeInfo{Name: "nonexistent-node"}

		err := stmt.Allocate(task, fakeNode)
		if err == nil {
			t.Fatal("expected Allocate to fail for nonexistent node")
		}
		if len(stmt.operations) != 0 {
			t.Errorf("expected 0 operations after rollback, got %d", len(stmt.operations))
		}
		// After auto-rollback, task should be back to Pending.
		if task.Status != api.Pending {
			t.Errorf("expected task status Pending after rollback, got %v", task.Status)
		}
		if task.NodeName != "" {
			t.Errorf("expected empty NodeName after rollback, got %s", task.NodeName)
		}
	})

	t.Run("allocate with missing job returns error and rolls back", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		delete(ssn.Jobs, task.Job)

		err := stmt.Allocate(task, node)
		if err == nil {
			t.Fatal("expected Allocate to fail when job is missing")
		}
		if len(stmt.operations) != 0 {
			t.Errorf("expected 0 operations after rollback, got %d", len(stmt.operations))
		}
	})
}

func TestDiscardReversesOperations(t *testing.T) {
	t.Run("discard reverses pipeline operation", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		err := stmt.Pipeline(task, node.Name, false)
		if err != nil {
			t.Fatalf("Pipeline failed: %v", err)
		}
		if task.Status != api.Pipelined {
			t.Fatalf("expected Pipelined status, got %v", task.Status)
		}

		stmt.Discard()

		// After discard, task should be reverted to Pending.
		if task.Status != api.Pending {
			t.Errorf("expected task status Pending after Discard, got %v", task.Status)
		}
		if task.NodeName != "" {
			t.Errorf("expected empty NodeName after Discard, got %s", task.NodeName)
		}
	})

	t.Run("discard reverses allocate operation", func(t *testing.T) {
		ssn, _, task, node := newTestSession(t)
		stmt := NewStatement(ssn)

		err := stmt.Allocate(task, node)
		if err != nil {
			t.Fatalf("Allocate failed: %v", err)
		}
		if task.Status != api.Allocated {
			t.Fatalf("expected Allocated status, got %v", task.Status)
		}

		stmt.Discard()

		if task.Status != api.Pending {
			t.Errorf("expected task status Pending after Discard, got %v", task.Status)
		}
		if task.NodeName != "" {
			t.Errorf("expected empty NodeName after Discard, got %s", task.NodeName)
		}
	})

	t.Run("discard reverses evict operation", func(t *testing.T) {
		ssn, job, task, node := newTestSession(t)

		// First allocate the task so it's on the node, then mark Running, then evict.
		if err := NewStatement(ssn).Allocate(task, node); err != nil {
			t.Fatalf("setup allocate failed: %v", err)
		}
		job.UpdateTaskStatus(task, api.Running)

		stmt := NewStatement(ssn)
		stmt.Evict(task, "test-reclaim")

		if task.Status != api.Releasing {
			t.Fatalf("expected Releasing status after Evict, got %v", task.Status)
		}

		stmt.Discard()

		// After discard, unevict should restore the task to Running.
		if task.Status != api.Running {
			t.Errorf("expected task status Running after Discard of Evict, got %v", task.Status)
		}
	})
}
