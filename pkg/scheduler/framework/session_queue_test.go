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
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
)

const queueUpdateTestTimeout = 2 * time.Second

var benchmarkQueueAllocatedResources map[api.QueueID]*api.Resource

func newQueueInfo(name, parent string) *api.QueueInfo {
	return api.NewQueueInfo(&scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: scheduling.QueueSpec{
			Weight: 1,
			Parent: parent,
		},
	})
}

func newTaskInfo(uid string, status api.TaskStatus, resources v1.ResourceList) *api.TaskInfo {
	return &api.TaskInfo{
		UID:                api.TaskID(uid),
		Resreq:             api.NewResource(resources),
		TransactionContext: api.TransactionContext{Status: status},
	}
}

func newJobInfo(uid, queue string, tasks ...*api.TaskInfo) *api.JobInfo {
	job := api.NewJobInfo(api.JobID(uid), tasks...)
	job.Queue = api.QueueID(queue)
	return job
}

func queueTestResourceList(cpu, memory, scalar string) v1.ResourceList {
	resources := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
	if scalar != "" {
		resources[v1.ResourceName("example.com/accelerator")] = resource.MustParse(scalar)
	}
	return resources
}

func assertQueueResource(t *testing.T, got *api.Resource, cpu, memory, scalar float64) {
	t.Helper()
	if got == nil {
		t.Fatal("expected queue resource, got nil")
	}
	if got.MilliCPU != cpu || got.Memory != memory || got.ScalarResources[v1.ResourceName("example.com/accelerator")] != scalar {
		t.Fatalf("unexpected queue resource: got %v, want cpu %.0f, memory %.0f, scalar %.0f", got, cpu, memory, scalar)
	}
}

func TestCalculateQueueAllocatedResourcesBottomUp(t *testing.T) {
	leafTask := newTaskInfo("leaf-running", api.Running, queueTestResourceList("1", "1Gi", "2"))
	leafPendingTask := newTaskInfo("leaf-pending", api.Pending, queueTestResourceList("4", "4Gi", "4"))
	teamTask := newTaskInfo("team-allocated", api.Allocated, queueTestResourceList("500m", "512Mi", "1"))
	topLevelTask := newTaskInfo("top-level-bound", api.Bound, queueTestResourceList("250m", "256Mi", "3"))

	ssn := &Session{
		Jobs: map[api.JobID]*api.JobInfo{
			"leaf-job":      newJobInfo("leaf-job", "leaf", leafTask, leafPendingTask),
			"team-job":      newJobInfo("team-job", "team", teamTask),
			"top-level-job": newJobInfo("top-level-job", "top-level", topLevelTask),
		},
		Nodes: map[string]*api.NodeInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{
			"root":      newQueueInfo("root", ""),
			"team":      newQueueInfo("team", "root"),
			"leaf":      newQueueInfo("leaf", "team"),
			"top-level": newQueueInfo("top-level", ""),
		},
	}

	allocated, _, err := calculateQueueAllocatedResources(ssn)
	if err != nil {
		t.Fatalf("calculateQueueAllocatedResources returned error: %v", err)
	}

	assertQueueResource(t, allocated["leaf"], 1000, float64(1<<30), 2000)
	assertQueueResource(t, allocated["team"], 1500, float64(3*(1<<29)), 3000)
	assertQueueResource(t, allocated["top-level"], 250, float64(1<<28), 3000)
	assertQueueResource(t, allocated["root"], 1750, float64(7*(1<<28)), 6000)
}

func TestCalculateQueueAllocatedResourcesDoesNotCreateEmptyDRAResults(t *testing.T) {
	ssn := &Session{
		Jobs: map[api.JobID]*api.JobInfo{
			"job": newJobInfo("job", "leaf", newTaskInfo("task", api.Running, queueTestResourceList("1", "1Gi", ""))),
		},
		Nodes: map[string]*api.NodeInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{
			"root": newQueueInfo("root", ""),
			"leaf": newQueueInfo("leaf", "root"),
		},
	}

	_, allocatedDRA, err := calculateQueueAllocatedResources(ssn)
	if err != nil {
		t.Fatalf("calculateQueueAllocatedResources returned error: %v", err)
	}
	if allocatedDRA != nil {
		t.Fatalf("expected no DRA result for queues without DRA resources, got %v", allocatedDRA)
	}
}

func TestCalculateQueueAllocatedResourcesRejectsInvalidHierarchy(t *testing.T) {
	tests := []struct {
		name    string
		queues  map[api.QueueID]*api.QueueInfo
		wantErr string
	}{
		{
			name: "missing root",
			queues: map[api.QueueID]*api.QueueInfo{
				"leaf": newQueueInfo("leaf", ""),
			},
			wantErr: "root queue",
		},
		{
			name: "root has parent",
			queues: map[api.QueueID]*api.QueueInfo{
				"root": newQueueInfo("root", "parent"),
			},
			wantErr: "must not have parent",
		},
		{
			name: "missing parent",
			queues: map[api.QueueID]*api.QueueInfo{
				"root": newQueueInfo("root", ""),
				"leaf": newQueueInfo("leaf", "missing"),
			},
			wantErr: "missing",
		},
		{
			name: "cycle",
			queues: map[api.QueueID]*api.QueueInfo{
				"root":    newQueueInfo("root", ""),
				"queue-a": newQueueInfo("queue-a", "queue-b"),
				"queue-b": newQueueInfo("queue-b", "queue-a"),
			},
			wantErr: "cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := calculateQueueAllocatedResources(&Session{
				Jobs:   map[api.JobID]*api.JobInfo{},
				Nodes:  map[string]*api.NodeInfo{},
				Queues: tt.queues,
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

type countingQueueUpdateCache struct {
	schedcache.Cache

	mu      sync.Mutex
	updates int
}

func (c *countingQueueUpdateCache) UpdateQueueStatus(_ *api.QueueInfo) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates++
	return nil
}

func (c *countingQueueUpdateCache) updateCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.updates
}

func TestUpdateQueueStatusDoesNotWriteInvalidHierarchy(t *testing.T) {
	cache := &countingQueueUpdateCache{}
	ssn := &Session{
		cache: cache,
		Jobs:  map[api.JobID]*api.JobInfo{},
		Nodes: map[string]*api.NodeInfo{},
		Queues: map[api.QueueID]*api.QueueInfo{
			"root": newQueueInfo("root", ""),
			"leaf": newQueueInfo("leaf", "missing"),
		},
	}

	updateQueueStatus(ssn)

	if got := cache.updateCount(); got != 0 {
		t.Fatalf("expected no queue writes, got %d", got)
	}
}

type blockingQueueUpdateTracker struct {
	entered chan string
	release <-chan struct{}

	mu      sync.Mutex
	updates []string
}

func (t *blockingQueueUpdateTracker) track(name string) {
	t.mu.Lock()
	t.updates = append(t.updates, name)
	t.mu.Unlock()
	t.entered <- name
	<-t.release
}

func (t *blockingQueueUpdateTracker) updateNames() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.updates...)
}

type blockingQueueUpdateCache struct {
	schedcache.Cache
	tracker *blockingQueueUpdateTracker
}

func (c *blockingQueueUpdateCache) UpdateQueueStatus(queue *api.QueueInfo) error {
	c.tracker.track(queue.Name)
	return nil
}

func TestUpdateQueueStatusHonorsWorkerLimit(t *testing.T) {
	const (
		workerNum    = 4
		normalQueues = 8
	)

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseUpdates := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseUpdates)

	tracker := &blockingQueueUpdateTracker{
		entered: make(chan string, normalQueues+2),
		release: release,
	}
	cache := &blockingQueueUpdateCache{tracker: tracker}

	queues := map[api.QueueID]*api.QueueInfo{
		"root": newQueueInfo("root", ""),
	}
	for i := 0; i < normalQueues; i++ {
		name := fmt.Sprintf("queue-%d", i)
		queues[api.QueueID(name)] = newQueueInfo(name, "root")
	}

	originalOptions := options.ServerOpts
	options.ServerOpts = &options.ServerOption{QueueUpdaterWorkerNum: workerNum}
	t.Cleanup(func() { options.ServerOpts = originalOptions })

	ssn := &Session{
		cache:  cache,
		Jobs:   map[api.JobID]*api.JobInfo{},
		Nodes:  map[string]*api.NodeInfo{},
		Queues: queues,
	}
	done := make(chan struct{})
	go func() {
		updateQueueStatus(ssn)
		close(done)
	}()

	for i := 0; i < workerNum; i++ {
		select {
		case <-tracker.entered:
		case <-time.After(queueUpdateTestTimeout):
			t.Fatalf("timed out waiting for %d concurrent queue updates", workerNum)
		}
	}
	select {
	case name := <-tracker.entered:
		t.Fatalf("worker limit exceeded: a fifth update for %q started before release", name)
	case <-time.After(100 * time.Millisecond):
	}

	releaseUpdates()
	select {
	case <-done:
	case <-time.After(queueUpdateTestTimeout):
		t.Fatal("timed out waiting for queue status updates to finish")
	}

	updateCounts := make(map[string]int)
	for _, name := range tracker.updateNames() {
		updateCounts[name]++
	}
	if len(updateCounts) != len(queues) {
		t.Fatalf("expected %d queues to be updated, got %d: %v", len(queues), len(updateCounts), updateCounts)
	}
	for name, count := range updateCounts {
		if count != 1 {
			t.Fatalf("expected queue %q to be updated once, got %d", name, count)
		}
	}
}

func legacyQueueAllocatedResources(ssn *Session) map[api.QueueID]*api.Resource {
	rootQueue := api.QueueID("root")
	allocatedResources := make(map[api.QueueID]*api.Resource, len(ssn.Queues))
	for queueID := range ssn.Queues {
		allocatedResources[queueID] = api.EmptyResource()
	}
	for _, job := range ssn.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}
			for _, task := range tasks {
				allocatedResources[job.Queue].Add(task.Resreq)
				queue := ssn.Queues[job.Queue].Queue
				for ssn.Queues[rootQueue] != nil {
					parent := string(rootQueue)
					if queue.Spec.Parent != "" {
						parent = queue.Spec.Parent
					}
					allocatedResources[api.QueueID(parent)].Add(task.Resreq)
					if parent == string(rootQueue) {
						break
					}
					queue = ssn.Queues[api.QueueID(queue.Spec.Parent)].Queue
				}
			}
		}
	}
	return allocatedResources
}

func BenchmarkCalculateQueueAllocatedResources(b *testing.B) {
	const (
		queueDepth = 8
		taskCount  = 2000
	)

	queues := map[api.QueueID]*api.QueueInfo{
		"root": newQueueInfo("root", ""),
	}
	parent := "root"
	for i := 0; i < queueDepth; i++ {
		name := fmt.Sprintf("queue-%d", i)
		queues[api.QueueID(name)] = newQueueInfo(name, parent)
		parent = name
	}
	tasks := make([]*api.TaskInfo, 0, taskCount)
	for i := 0; i < taskCount; i++ {
		tasks = append(tasks, newTaskInfo(fmt.Sprintf("task-%d", i), api.Running, queueTestResourceList("1m", "1Mi", "")))
	}
	job := newJobInfo("benchmark-job", parent, tasks...)
	ssn := &Session{
		Jobs:   map[api.JobID]*api.JobInfo{job.UID: job},
		Nodes:  map[string]*api.NodeInfo{},
		Queues: queues,
	}

	b.Run("legacy-per-task-ancestors", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			benchmarkQueueAllocatedResources = legacyQueueAllocatedResources(ssn)
		}
	})
	b.Run("bottom-up", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			var err error
			benchmarkQueueAllocatedResources, _, err = calculateQueueAllocatedResources(ssn)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
