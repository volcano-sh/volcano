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

package cache

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestSchedulerCachePrepareBindBatchRollsBackPreparedParticipants(t *testing.T) {
	schedulerCache := newMockSchedulerCache("batch-bind-test")
	t.Cleanup(func() {
		ShutdownMockSchedulerCache(schedulerCache)
	})

	events := make([]string, 0)
	schedulerCache.binderRegistry.Register("first", &batchBindTestPreBinder{
		name:   "first",
		events: &events,
	})
	schedulerCache.binderRegistry.Register("second", &batchBindTestPreBinder{
		name:       "second",
		events:     &events,
		prepareErr: errors.New("second preparation failed"),
	})

	if _, err := schedulerCache.PrepareBindBatch(context.Background(), nil); err == nil {
		t.Fatal("PrepareBindBatch() error = nil, want preparation failure")
	}
	wantEvents := []string{"prepare:first", "prepare:second", "rollback:first"}
	if !reflect.DeepEqual(events, wantEvents) {
		t.Fatalf("batch preparation events = %v, want %v", events, wantEvents)
	}
}

func TestSchedulerCacheAddBindBatchIsAtomic(t *testing.T) {
	t.Run("accepts and queues all contexts", func(t *testing.T) {
		schedulerCache, job, node, tasks := newBatchBindTestSchedulerCache(t, 2, 2)
		bindContexts := batchBindContextsForNode(tasks, node.Name)

		if err := schedulerCache.AddBindBatch(bindContexts); err != nil {
			t.Fatalf("AddBindBatch() error = %v", err)
		}
		if len(schedulerCache.BindFlowChannel) != 2 {
			t.Fatalf("queued binds = %d, want 2", len(schedulerCache.BindFlowChannel))
		}
		if len(node.Tasks) != 2 {
			t.Fatalf("Node task count = %d, want 2", len(node.Tasks))
		}
		for _, task := range tasks {
			if task.Status != schedulingapi.Binding || task.NodeName != node.Name {
				t.Fatalf("Task %q = (%q, %q), want (Binding, %q)", task.UID, task.Status, task.NodeName, node.Name)
			}
		}
		if job.TaskPlacementGeneration != 2 {
			t.Fatalf("TaskPlacementGeneration = %d, want 2", job.TaskPlacementGeneration)
		}
	})

	t.Run("restores earlier contexts after later admission fails", func(t *testing.T) {
		schedulerCache, job, node, tasks := newBatchBindTestSchedulerCache(t, 1, 2)
		bindContexts := batchBindContextsForNode(tasks, node.Name)

		if err := schedulerCache.AddBindBatch(bindContexts); err == nil {
			t.Fatal("AddBindBatch() error = nil, want insufficient-resource failure")
		}
		if len(schedulerCache.BindFlowChannel) != 0 {
			t.Fatalf("queued binds = %d, want 0", len(schedulerCache.BindFlowChannel))
		}
		if len(node.Tasks) != 0 {
			t.Fatalf("Node task count = %d, want rollback to zero", len(node.Tasks))
		}
		for _, task := range tasks {
			if task.Status != schedulingapi.Pending || task.NodeName != "" {
				t.Fatalf("Task %q after rollback = (%q, %q), want (Pending, empty)", task.UID, task.Status, task.NodeName)
			}
		}
		if pending := job.TaskStatusIndex[schedulingapi.Pending]; len(pending) != len(tasks) {
			t.Fatalf("pending Task count = %d, want %d", len(pending), len(tasks))
		}
		if job.TaskPlacementGeneration != 0 {
			t.Fatalf("TaskPlacementGeneration = %d, want no partial placement", job.TaskPlacementGeneration)
		}
	})
}

type batchBindTestPreBinder struct {
	name       string
	events     *[]string
	prepareErr error
}

func (binder *batchBindTestPreBinder) PrepareBindBatch(_ context.Context, _ []*BindContext) (BatchBindPreparation, error) {
	*binder.events = append(*binder.events, "prepare:"+binder.name)
	if binder.prepareErr != nil {
		return nil, binder.prepareErr
	}
	return &batchBindTestPreparation{name: binder.name, events: binder.events}, nil
}

type batchBindTestPreparation struct {
	name   string
	events *[]string
}

func (preparation *batchBindTestPreparation) Commit(context.Context) error {
	*preparation.events = append(*preparation.events, "commit:"+preparation.name)
	return nil
}

func (preparation *batchBindTestPreparation) Rollback(context.Context) error {
	*preparation.events = append(*preparation.events, "rollback:"+preparation.name)
	return nil
}

func newBatchBindTestSchedulerCache(t *testing.T, nodeCPU int64, taskCount int) (*SchedulerCache, *schedulingapi.JobInfo, *schedulingapi.NodeInfo, []*schedulingapi.TaskInfo) {
	t.Helper()
	schedulerCache := newMockSchedulerCache("batch-bind-test")
	t.Cleanup(func() {
		ShutdownMockSchedulerCache(schedulerCache)
	})
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: types.UID("node-a-uid")},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    *resource.NewQuantity(nodeCPU, resource.DecimalSI),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
			corev1.ResourcePods:   *resource.NewQuantity(100, resource.DecimalSI),
		}},
	}
	nodeInfo := schedulingapi.NewNodeInfo(node)
	schedulerCache.Nodes[node.Name] = nodeInfo

	tasks := make([]*schedulingapi.TaskInfo, 0, taskCount)
	for index := 0; index < taskCount; index++ {
		name := fmt.Sprintf("worker-%d", index)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				UID:       types.UID(name),
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{
				Name: "worker",
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				}},
			}}},
		}
		task := schedulingapi.NewTaskInfo(pod)
		task.Job = "job-a"
		task.Status = schedulingapi.Pending
		tasks = append(tasks, task)
	}
	job := schedulingapi.NewJobInfo("job-a", tasks...)
	schedulerCache.Jobs[job.UID] = job
	return schedulerCache, job, nodeInfo, tasks
}

func batchBindContextsForNode(tasks []*schedulingapi.TaskInfo, nodeName string) []*BindContext {
	contexts := make([]*BindContext, 0, len(tasks))
	for _, task := range tasks {
		contextTask := task.Clone()
		contextTask.NodeName = nodeName
		contexts = append(contexts, &BindContext{TaskInfo: contextTask, Extensions: make(map[string]BindContextExtension)})
	}
	return contexts
}
