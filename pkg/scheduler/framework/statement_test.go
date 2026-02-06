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

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestStatementOperationCount(t *testing.T) {
	// Build test resources
	pg := util.BuildPodGroup("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue)
	queue := util.BuildQueue("q1", 1, nil)
	node := util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

	pod1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)
	pod2 := util.BuildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)

	// Create scheduler cache
	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	schedulerCache.AddOrUpdateNode(node)
	schedulerCache.AddPod(pod1)
	schedulerCache.AddPod(pod2)
	schedulerCache.AddPodGroupV1beta1(pg)
	schedulerCache.AddQueueV1beta1(queue)

	// Open session
	ssn := OpenSession(schedulerCache, nil, nil)
	defer CloseSession(ssn)

	// Create statement
	stmt := NewStatement(ssn)

	// Verify initial operation count is 0
	assert.Equal(t, 0, stmt.OperationCount(), "Initial operation count should be 0")

	// Get task info
	var task1, task2 *api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			if task.Name == "p1" {
				task1 = task
			} else if task.Name == "p2" {
				task2 = task
			}
		}
	}

	if task1 == nil || task2 == nil {
		t.Fatal("Failed to find tasks in session")
	}

	// Evict first task
	err := stmt.Evict(task1, "test")
	assert.NoError(t, err, "Evict task1 should succeed")
	assert.Equal(t, 1, stmt.OperationCount(), "Operation count should be 1 after first eviction")

	// Evict second task
	err = stmt.Evict(task2, "test")
	assert.NoError(t, err, "Evict task2 should succeed")
	assert.Equal(t, 2, stmt.OperationCount(), "Operation count should be 2 after second eviction")
}

func TestStatementRollbackOperationsFrom(t *testing.T) {
	// Build test resources
	pg := util.BuildPodGroup("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue)
	queue := util.BuildQueue("q1", 1, nil)
	node := util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

	pod1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)
	pod2 := util.BuildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)
	pod3 := util.BuildPod("c1", "p3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)

	// Create scheduler cache
	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	schedulerCache.AddOrUpdateNode(node)
	schedulerCache.AddPod(pod1)
	schedulerCache.AddPod(pod2)
	schedulerCache.AddPod(pod3)
	schedulerCache.AddPodGroupV1beta1(pg)
	schedulerCache.AddQueueV1beta1(queue)

	// Open session
	ssn := OpenSession(schedulerCache, nil, nil)
	defer CloseSession(ssn)

	// Create statement
	stmt := NewStatement(ssn)

	// Get task infos
	var task1, task2, task3 *api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			switch task.Name {
			case "p1":
				task1 = task
			case "p2":
				task2 = task
			case "p3":
				task3 = task
			}
		}
	}

	if task1 == nil || task2 == nil || task3 == nil {
		t.Fatal("Failed to find tasks in session")
	}

	// Evict task1 (this is a "successful" preemption)
	err := stmt.Evict(task1, "test")
	assert.NoError(t, err)
	assert.Equal(t, api.Releasing, task1.Status, "task1 should be Releasing after eviction")

	// Record operation count before second preemption attempt
	opCountBeforeSecondAttempt := stmt.OperationCount()
	assert.Equal(t, 1, opCountBeforeSecondAttempt)

	// Evict task2 and task3 (this is a "failed" preemption attempt)
	err = stmt.Evict(task2, "test")
	assert.NoError(t, err)
	assert.Equal(t, api.Releasing, task2.Status, "task2 should be Releasing after eviction")

	err = stmt.Evict(task3, "test")
	assert.NoError(t, err)
	assert.Equal(t, api.Releasing, task3.Status, "task3 should be Releasing after eviction")

	assert.Equal(t, 3, stmt.OperationCount(), "Should have 3 operations")

	// Simulate pipeline failure - rollback only the second preemption attempt
	stmt.RollbackOperationsFrom(opCountBeforeSecondAttempt)

	// Verify task2 and task3 are restored to Running
	assert.Equal(t, api.Running, task2.Status, "task2 should be Running after rollback")
	assert.Equal(t, api.Running, task3.Status, "task3 should be Running after rollback")

	// Verify task1 is still Releasing (not rolled back)
	assert.Equal(t, api.Releasing, task1.Status, "task1 should still be Releasing")

	// Verify operation count
	assert.Equal(t, 1, stmt.OperationCount(), "Should have 1 operation after rollback")
}

func TestStatementRollbackOperationsFromBoundaryConditions(t *testing.T) {
	// Build minimal test resources
	pg := util.BuildPodGroup("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue)
	queue := util.BuildQueue("q1", 1, nil)
	node := util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)
	pod1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)

	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	schedulerCache.AddOrUpdateNode(node)
	schedulerCache.AddPod(pod1)
	schedulerCache.AddPodGroupV1beta1(pg)
	schedulerCache.AddQueueV1beta1(queue)

	ssn := OpenSession(schedulerCache, nil, nil)
	defer CloseSession(ssn)

	stmt := NewStatement(ssn)

	// Test rollback with negative index (should do nothing)
	stmt.RollbackOperationsFrom(-1)
	assert.Equal(t, 0, stmt.OperationCount(), "Negative index should not affect operations")

	// Test rollback with index >= operation count (should do nothing)
	stmt.RollbackOperationsFrom(0)
	assert.Equal(t, 0, stmt.OperationCount(), "Index >= count should not affect operations")

	stmt.RollbackOperationsFrom(100)
	assert.Equal(t, 0, stmt.OperationCount(), "Large index should not affect operations")
}

func TestStatementUnevict(t *testing.T) {
	// Build test resources
	pg := util.BuildPodGroup("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue)
	queue := util.BuildQueue("q1", 1, nil)
	node := util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)
	pod1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil)

	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	schedulerCache.AddOrUpdateNode(node)
	schedulerCache.AddPod(pod1)
	schedulerCache.AddPodGroupV1beta1(pg)
	schedulerCache.AddQueueV1beta1(queue)

	ssn := OpenSession(schedulerCache, nil, nil)
	defer CloseSession(ssn)

	stmt := NewStatement(ssn)

	// Get task info
	var task1 *api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			if task.Name == "p1" {
				task1 = task
				break
			}
		}
	}

	if task1 == nil {
		t.Fatal("Failed to find task in session")
	}

	// Evict task
	err := stmt.Evict(task1, "test")
	assert.NoError(t, err)
	assert.Equal(t, api.Releasing, task1.Status, "task should be Releasing after eviction")

	// Unevict task using the public method
	err = stmt.Unevict(task1)
	assert.NoError(t, err)
	assert.Equal(t, api.Running, task1.Status, "task should be Running after unevict")
}
