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

package cache

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	k8sschedulingqueue "volcano.sh/volcano/third_party/kubernetes/pkg/scheduler/backend/queue"
)

// TestRemoveBindRecord tests removing bind records
func TestRemoveBindRecord(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords: make(map[string]int64),
	}

	// Add some records
	binder.nodeBindRecords["node-1"] = 5
	binder.nodeBindRecords["node-2"] = 3

	// Remove one record
	binder.RemoveBindRecord("node-1")

	// Verify removal
	if _, exists := binder.nodeBindRecords["node-1"]; exists {
		t.Error("Expected node-1 record to be removed")
	}
	if binder.nodeBindRecords["node-2"] != 3 {
		t.Errorf("Expected node-2 record to remain 3, got %d", binder.nodeBindRecords["node-2"])
	}
}

// TestEnqueueScheduleResult tests the EnqueueScheduleResult method
func TestEnqueueScheduleResult(t *testing.T) {
	// Create ConflictAwareBinder
	binder := &ConflictAwareBinder{
		nodeBindRecords:  make(map[string]int64),
		BindCheckChannel: make(chan *agentapi.PodScheduleResult, 10),
	}

	// Create a schedule result
	scheduleResult := &agentapi.PodScheduleResult{}

	// Enqueue the schedule result
	binder.EnqueueScheduleResult(scheduleResult)

	// Verify the result was enqueued
	select {
	case result := <-binder.BindCheckChannel:
		if result != scheduleResult {
			t.Errorf("Expected enqueued result to be %v, got %v", scheduleResult, result)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected result to be enqueued in BindCheckChannel")
	}
}

// TestFindNonConflictingNode_NoConflict tests finding non-conflicting node when no previous records exist
func TestFindNonConflictingNode_NoConflict(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords: make(map[string]int64),
	}

	// Create nodes with no previous records (no conflicts)
	nodes := []*api.NodeInfo{
		{Name: "node-1", BindGeneration: 1},
		{Name: "node-2", BindGeneration: 2},
	}

	scheduleResult := &agentapi.PodScheduleResult{
		SuggestedNodes: nodes,
	}

	// Should return first node (no conflict)
	node := binder.FindNonConflictingNode(scheduleResult)
	if node == nil {
		t.Error("Expected to find non-conflicting node")
	} else if node.Name != "node-1" {
		t.Errorf("Expected node-1, got %s", node.Name)
	}
}

// TestFindNonConflictingNode_WithConflict tests finding non-conflicting node when some nodes conflict
func TestFindNonConflictingNode_WithConflict(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords: map[string]int64{
			"node-1": 5, // Higher than suggested generation
			"node-2": 2, // Lower than suggested generation
		},
	}

	nodes := []*api.NodeInfo{
		{Name: "node-1", BindGeneration: 3}, // Conflicts (3 <= 5)
		{Name: "node-2", BindGeneration: 4}, // No conflict (4 > 2)
		{Name: "node-3", BindGeneration: 1}, // No record, no conflict
	}

	scheduleResult := &agentapi.PodScheduleResult{
		SuggestedNodes: nodes,
	}

	// Should return node-2 (first non-conflicting)
	node := binder.FindNonConflictingNode(scheduleResult)
	if node == nil {
		t.Error("Expected to find non-conflicting node")
	} else if node.Name != "node-2" {
		t.Errorf("Expected node-2, got %s", node.Name)
	}
}

// TestFindNonConflictingNode_AllConflict tests when all suggested nodes conflict
func TestFindNonConflictingNode_AllConflict(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords: map[string]int64{
			"node-1": 5,
			"node-2": 5,
			"node-3": 5,
		},
	}

	nodes := []*api.NodeInfo{
		{Name: "node-1", BindGeneration: 4}, // Conflicts (4 <= 5)
		{Name: "node-2", BindGeneration: 5}, // Conflicts (5 <= 5)
		{Name: "node-3", BindGeneration: 3}, // Conflicts (3 <= 5)
	}

	scheduleResult := &agentapi.PodScheduleResult{
		SuggestedNodes: nodes,
	}

	// Should return nil (all conflict)
	node := binder.FindNonConflictingNode(scheduleResult)
	if node != nil {
		t.Errorf("Expected nil (all nodes conflict), got %s", node.Name)
	}
}

// TestFindNonConflictingNode_EqualGeneration tests edge case where generation equals recorded
func TestFindNonConflictingNode_EqualGeneration(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords: map[string]int64{
			"node-1": 3,
		},
	}

	nodes := []*api.NodeInfo{
		{Name: "node-1", BindGeneration: 3}, // Conflicts (equal generation)
		{Name: "node-2", BindGeneration: 4}, // No conflict
	}

	scheduleResult := &agentapi.PodScheduleResult{
		SuggestedNodes: nodes,
	}

	// Should return node-2 (node-1 conflicts due to equal generation)
	node := binder.FindNonConflictingNode(scheduleResult)
	if node == nil {
		t.Error("Expected to find non-conflicting node")
	} else if node.Name != "node-2" {
		t.Errorf("Expected node-2, got %s", node.Name)
	}
}

// Helper function to process sequential binding results
func processSequentialResults(t *testing.T, binder *ConflictAwareBinder, expectedSuccess, expectedConflicts int) {
	processed := 0
	conflicts := 0
	successfulBinds := 0
	total := expectedSuccess + expectedConflicts

	for processed < total {
		select {
		case result := <-binder.BindCheckChannel:
			node := binder.FindNonConflictingNode(result)

			if node == nil {
				conflicts++
				t.Logf("Result %d: CONFLICT", processed)
			} else {
				successfulBinds++
				// Simulate binding by updating record
				binder.nodeBindRecords[node.Name] = node.BindGeneration
				t.Logf("Result %d: SUCCESS - bound to %s", processed, node.Name)
			}
			processed++

		case <-time.After(100 * time.Millisecond):
			t.Errorf("Timeout waiting for result %d", processed)
			return
		}
	}

	// Verify results
	if successfulBinds != expectedSuccess {
		t.Errorf("Expected %d successful binds, got %d", expectedSuccess, successfulBinds)
	}
	if conflicts != expectedConflicts {
		t.Errorf("Expected %d conflicts, got %d", expectedConflicts, conflicts)
	}
}

// TestSequentialScheduleResultProcessing tests sequential processing of multiple results
func TestSequentialScheduleResultProcessing(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords:  make(map[string]int64),
		BindCheckChannel: make(chan *agentapi.PodScheduleResult, 10),
	}

	// Create 3 results for the same node with same generation
	for i := 0; i < 3; i++ {
		pod := createTestPod(fmt.Sprintf("pod-%d", i), fmt.Sprintf("pod-%d", i))
		task := &api.TaskInfo{
			UID:       api.TaskID(fmt.Sprintf("pod-%d", i)),
			Name:      fmt.Sprintf("pod-%d", i),
			Namespace: "default",
			Pod:       pod,
		}

		result := &agentapi.PodScheduleResult{
			SchedCtx: &agentapi.SchedulingContext{Task: task},
			SuggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 1},
			},
			BindContext: &agentapi.BindContext{
				SchedCtx: &agentapi.SchedulingContext{Task: task},
			},
		}

		binder.EnqueueScheduleResult(result)
	}

	processSequentialResults(t, binder, 1, 2)
}

// TestMultipleNodesSequentialProcessing tests sequential processing with multiple nodes
func TestMultipleNodesSequentialProcessing(t *testing.T) {
	binder := &ConflictAwareBinder{
		nodeBindRecords:  make(map[string]int64),
		BindCheckChannel: make(chan *agentapi.PodScheduleResult, 10),
	}

	// Test scenarios that would occur in multi-worker scheduling
	testCases := []struct {
		podName    string
		nodeName   string
		generation int64
	}{
		{"pod-1", "node-1", 1}, // Should succeed
		{"pod-2", "node-2", 1}, // Should succeed
		{"pod-3", "node-1", 2}, // Should succeed (higher generation)
		{"pod-4", "node-2", 1}, // Should conflict (equal generation)
		{"pod-5", "node-1", 1}, // Should conflict (lower generation)
	}

	// Enqueue all results
	for _, tc := range testCases {
		pod := createTestPod(tc.podName, tc.podName)
		task := &api.TaskInfo{
			UID:       api.TaskID(tc.podName),
			Name:      tc.podName,
			Namespace: "default",
			Pod:       pod,
		}

		result := &agentapi.PodScheduleResult{
			SchedCtx: &agentapi.SchedulingContext{Task: task},
			SuggestedNodes: []*api.NodeInfo{
				{Name: tc.nodeName, BindGeneration: tc.generation},
			},
			BindContext: &agentapi.BindContext{
				SchedCtx: &agentapi.SchedulingContext{Task: task},
			},
		}

		binder.EnqueueScheduleResult(result)
	}

	processSequentialResults(t, binder, 3, 2)

	// Verify final bind records
	if binder.nodeBindRecords["node-1"] != 2 {
		t.Errorf("Expected node-1 final record to be 2, got %d", binder.nodeBindRecords["node-1"])
	}
	if binder.nodeBindRecords["node-2"] != 1 {
		t.Errorf("Expected node-2 final record to be 1, got %d", binder.nodeBindRecords["node-2"])
	}
}

// Helper function to create test pod
func createTestPod(name, uid string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID(uid),
			Name:      name,
			Namespace: "default",
		},
	}
}

// Helper function to create test node
func createTestNode(name string) *v1.Node {
	resourceList := api.BuildResourceList("2000m", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    resourceList,
			Allocatable: resourceList,
		},
	}
}

// Helper function to setup cache with node
func setupCacheWithNode(cache *SchedulerCache, nodeName string) {
	node := createTestNode(nodeName)
	cache.AddOrUpdateNode(node)
}

// TestCheckAndBindPod tests the CheckAndBindPod method using table-driven tests
func TestCheckAndBindPod(t *testing.T) {
	tests := []struct {
		name                       string
		preExistingBindGenerations map[string]int64
		expectedBindGeneration     int64
		expectedSelectedNode       string
		suggestedNodes             []*api.NodeInfo
		bindError                  bool
		sharedCandidates           bool
		wantRequeueReason          string
	}{
		{
			name:                       "successful binding without conflict",
			preExistingBindGenerations: nil,
			expectedBindGeneration:     1,
			expectedSelectedNode:       "node-1",
			suggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 1},
			},
		},
		{
			name:                       "binding conflict with higher generation",
			wantRequeueReason:          RequeueReasonBindingConflict,
			preExistingBindGenerations: map[string]int64{"node-1": 2},
			expectedBindGeneration:     2,
			expectedSelectedNode:       "node-1",
			suggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 1},
			},
		},
		{
			name:                       "select first non-conflicting node",
			preExistingBindGenerations: map[string]int64{"node-1": 2},
			expectedBindGeneration:     1,
			expectedSelectedNode:       "node-2",
			suggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 1}, // Conflicts with generation 2
				{Name: "node-2", BindGeneration: 1}, // No record, succeeds
				{Name: "node-3", BindGeneration: 3}, // No record, but node-2 selected first
			},
		},
		{
			name:                       "select second node when first conflicts",
			preExistingBindGenerations: map[string]int64{"node-1": 1, "node-2": 3},
			expectedBindGeneration:     4,
			expectedSelectedNode:       "node-3",
			suggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 1}, // Conflicts (equal generation)
				{Name: "node-2", BindGeneration: 2}, // Conflicts (2 <= 3)
				{Name: "node-3", BindGeneration: 4}, // No record, succeeds
			},
		},
		{
			name:                       "all nodes conflict - no selection",
			wantRequeueReason:          RequeueReasonBindingConflict,
			preExistingBindGenerations: map[string]int64{"node-1": 3, "node-2": 3},
			expectedBindGeneration:     0, // No update expected
			expectedSelectedNode:       "",
			suggestedNodes: []*api.NodeInfo{
				{Name: "node-1", BindGeneration: 2}, // Conflicts (2 <= 3)
				{Name: "node-2", BindGeneration: 3}, // Conflicts (3 <= 3)
			},
		},
		{
			name:                   "successful binding preserves another result's candidates",
			expectedBindGeneration: 1,
			expectedSelectedNode:   "node-1",
			suggestedNodes:         []*api.NodeInfo{{Name: "node-1", BindGeneration: 1}, {Name: "node-2", BindGeneration: 1}},
			sharedCandidates:       true,
		},
		{
			name:                       "conflict preserves another result's candidates",
			preExistingBindGenerations: map[string]int64{"node-1": 1},
			suggestedNodes:             []*api.NodeInfo{{Name: "node-1", BindGeneration: 1}},
			sharedCandidates:           true,
			wantRequeueReason:          RequeueReasonBindingConflict,
		},
		{
			name:              "assume error releases selected and unselected candidates",
			suggestedNodes:    []*api.NodeInfo{{Name: "node-1", BindGeneration: 1}, {Name: "node-2", BindGeneration: 1}},
			bindError:         true,
			wantRequeueReason: RequeueReasonBindTaskFailure,
		},
		{
			name:              "assume error preserves another result's candidates",
			suggestedNodes:    []*api.NodeInfo{{Name: "node-1", BindGeneration: 1}, {Name: "node-2", BindGeneration: 1}},
			bindError:         true,
			sharedCandidates:  true,
			wantRequeueReason: RequeueReasonBindTaskFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, schedulingQueue := mockForTest()
			defer cache.cancel()
			defer cache.nodeQueue.ShutDown()

			// Setup cache with all nodes that might be selected
			for _, node := range tt.suggestedNodes {
				setupCacheWithNode(cache, node.Name)
			}
			cache.RecordCandidateNodesInBinder(tt.suggestedNodes)
			if tt.sharedCandidates {
				cache.RecordCandidateNodesInBinder(tt.suggestedNodes)
			}

			var binderCache Cache = cache
			if tt.bindError {
				binderCache = &failingAssumeCache{Cache: cache}
			}
			binder, err := NewConflictAwareBinder(binderCache, schedulingQueue, 1)
			if err != nil {
				t.Fatal(err)
			}

			// Set up pre-existing bind generations if any
			for name, generation := range tt.preExistingBindGenerations {
				binder.nodeBindRecords[name] = generation
			}

			// Create test pod and task
			pod := createTestPod("test-pod", "test-pod-uid")
			task := api.NewTaskInfo(pod)

			// Create schedule result with suggested nodes from test case
			scheduleResult := &agentapi.PodScheduleResult{
				SchedCtx:       &agentapi.SchedulingContext{Task: task},
				SuggestedNodes: tt.suggestedNodes,
				BindContext: &agentapi.BindContext{
					SchedCtx: &agentapi.SchedulingContext{Task: task},
				},
			}

			// Execute CheckAndBindPod
			binder.CheckAndBindPod(scheduleResult)
			wantReferences := 0
			if tt.sharedCandidates {
				wantReferences = 1
			}
			for _, node := range tt.suggestedNodes {
				if got := cache.NodesInBinder[node.Name]; got != wantReferences {
					t.Errorf("candidate %s references = %d, want %d", node.Name, got, wantReferences)
				}
			}
			if !tt.sharedCandidates && len(cache.NodesInBinder) != 0 {
				t.Errorf("Expected all candidate nodes to be released, got %v", cache.NodesInBinder)
			}
			pending, _ := schedulingQueue.PendingPods()
			if tt.wantRequeueReason == "" {
				if len(pending) != 0 {
					t.Errorf("successful assume requeued %d pods", len(pending))
				}
			} else {
				if len(pending) != 1 {
					t.Fatalf("requeued pods = %d, want 1", len(pending))
				}
				if got := pending[0].Annotations[AnnotationSchedulingPriorityReason]; got != tt.wantRequeueReason {
					t.Errorf("requeue reason = %q, want %q", got, tt.wantRequeueReason)
				}
				if got := GetPodSchedulingPriority(pending[0]); got != SchedulingPriorityUrgent {
					t.Errorf("requeue priority = %d, want %d", got, SchedulingPriorityUrgent)
				}
			}
			if tt.bindError && len(binder.nodeBindRecords) != 0 {
				t.Errorf("failed assume recorded a bind generation: %v", binder.nodeBindRecords)
			}

			// Verify selected node and bind generation
			if tt.expectedSelectedNode == "" {
				// No node should be selected - verify no new records were added
				for nodeName, generation := range tt.preExistingBindGenerations {
					if actual := binder.nodeBindRecords[nodeName]; actual != generation {
						t.Errorf("Expected bind generation for %s to remain %d, got %d", nodeName, generation, actual)
					}
				}
			} else {
				// Verify the expected node was selected with correct generation
				if actual := binder.nodeBindRecords[tt.expectedSelectedNode]; actual != tt.expectedBindGeneration {
					t.Errorf("Expected bind generation for %s to be %d, got %d", tt.expectedSelectedNode, tt.expectedBindGeneration, actual)
				}
			}
		})
	}
}

// failingAssumeCache models a cache failure after Binder candidate selection,
// such as deletion of the selected node before the cache can assume the Pod.
type failingAssumeCache struct{ Cache }

func (c *failingAssumeCache) AddBindTask(*agentapi.BindContext) error {
	return fmt.Errorf("selected node disappeared before assume")
}

// pausedAssumeCache lets a worker inspect its snapshot before the Binder assumes a pod.
type pausedAssumeCache struct {
	Cache
	entered chan struct{}
	resume  chan struct{}
}

func (c *pausedAssumeCache) AddBindTask(bindContext *agentapi.BindContext) error {
	close(c.entered)
	<-c.resume
	return c.Cache.AddBindTask(bindContext)
}

func TestCheckAndBindPodKeepsCandidatesUntilAssumed(t *testing.T) {
	cache, queue := mockForTest()
	t.Cleanup(cache.cancel)
	t.Cleanup(cache.nodeQueue.ShutDown)
	setupCacheWithNode(cache, "selected")
	setupCacheWithNode(cache, "unselected")

	snapshot := k8sutil.NewEmptySnapshot()
	if err := cache.UpdateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	selected, err := snapshot.GetVolcanoNodeInfo("selected")
	if err != nil {
		t.Fatal(err)
	}
	unselected, err := snapshot.GetVolcanoNodeInfo("unselected")
	if err != nil {
		t.Fatal(err)
	}
	candidates := []*api.NodeInfo{selected, unselected}
	cache.RecordCandidateNodesInBinder(candidates)
	initialBindGeneration := selected.BindGeneration

	pausedCache := &pausedAssumeCache{
		Cache: cache, entered: make(chan struct{}), resume: make(chan struct{}),
	}
	binder, err := NewConflictAwareBinder(pausedCache, queue, 1)
	if err != nil {
		t.Fatal(err)
	}
	task := api.NewTaskInfo(createTestPod("test-pod", "test-pod-uid"))
	schedCtx := &agentapi.SchedulingContext{Task: task}
	result := &agentapi.PodScheduleResult{
		SchedCtx:       schedCtx,
		SuggestedNodes: candidates,
		BindContext:    &agentapi.BindContext{SchedCtx: schedCtx},
	}
	done := make(chan struct{})
	resume := sync.OnceFunc(func() { close(pausedCache.resume) })
	t.Cleanup(func() {
		resume()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Binder did not finish after resuming cache assume")
		}
	})
	go func() {
		binder.CheckAndBindPod(result)
		close(done)
	}()
	select {
	case <-pausedCache.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Binder did not reach cache assume")
	}

	// A concurrent worker must still see both candidates as pending while
	// the selected node's cache state and generation have not advanced.
	if err := cache.UpdateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	for _, node := range candidates {
		if got := snapshot.NodesInBinder[node.Name]; got != 1 {
			t.Errorf("candidate %s released before cache assume: pending references = %d, want 1", node.Name, got)
		}
	}
	before := snapshot.GetVolcanoNodeInfoMap()[selected.Name]
	if len(before.Tasks) != 0 || before.BindGeneration != initialBindGeneration {
		t.Fatal("selected node was assumed before the barrier was released")
	}

	resume()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Binder did not finish cache assume")
	}
	if err := cache.UpdateSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.NodesInBinder) != 0 {
		t.Errorf("candidates still pending after assume: %v", snapshot.NodesInBinder)
	}
	after := snapshot.GetVolcanoNodeInfoMap()[selected.Name]
	if _, found := after.Tasks[api.PodKey(task.Pod)]; !found {
		t.Error("released candidate's snapshot does not contain the assumed task")
	}
	if after.BindGeneration <= initialBindGeneration {
		t.Errorf("released candidate's generation = %d, want > %d", after.BindGeneration, initialBindGeneration)
	}
	if got, found := binder.nodeBindRecords[selected.Name]; !found || got != initialBindGeneration {
		t.Errorf("Binder generation record = %d (present=%t), want %d", got, found, initialBindGeneration)
	}
}

func mockForTest() (*SchedulerCache, k8sschedulingqueue.SchedulingQueue) {
	metrics.InitKubeSchedulerRelatedMetrics()
	cache := NewDefaultMockSchedulerCache("mock-scheduler")
	ctx, cancel := context.WithCancel(context.Background())
	cache.cancel = cancel
	metricsRecorder := k8smetrics.NewMetricsAsyncRecorder(1000, time.Second, ctx.Done())

	queueingHintMapPerProfile := make(k8sschedulingqueue.QueueingHintMapPerProfile)
	// for _, schedulerName := range sc.schedulerNames {
	// 	queueingHintMapPerProfile[schedulerName] = buildQueueingHintMap()
	// }
	schedulingQueue := k8sschedulingqueue.NewSchedulingQueue(
		Less,
		cache.informerFactory,
		k8sschedulingqueue.WithClock(defaultSchedulerOptions.clock),
		k8sschedulingqueue.WithPodInitialBackoffDuration(time.Duration(defaultSchedulerOptions.podInitialBackoffSeconds)*time.Second),
		k8sschedulingqueue.WithPodMaxBackoffDuration(time.Duration(defaultSchedulerOptions.podMaxBackoffSeconds)*time.Second),
		k8sschedulingqueue.WithPodMaxInUnschedulablePodsDuration(defaultSchedulerOptions.podMaxInUnschedulablePodsDuration),
		k8sschedulingqueue.WithMetricsRecorder(metricsRecorder),
		k8sschedulingqueue.WithQueueingHintMapPerProfile(queueingHintMapPerProfile),
	)
	cache.schedulingQueue = schedulingQueue
	return cache, schedulingQueue
}

// These cases exercise real cache failures after a valid scheduling snapshot,
// rather than relying only on the failingAssumeCache dependency stub.
func TestCheckAndBindPodRestoresTaskAfterAssumeFailure(t *testing.T) {
	tests := []struct {
		name       string
		removeNode bool
	}{
		{name: "node removed after snapshot", removeNode: true},
		{name: "resources consumed after snapshot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, queue := mockForTest()
			t.Cleanup(cache.cancel)
			t.Cleanup(cache.nodeQueue.ShutDown)
			setupCacheWithNode(cache, "selected")
			binder, err := NewConflictAwareBinder(cache, queue, 1)
			if err != nil {
				t.Fatal(err)
			}
			cache.ConflictAwareBinder = binder

			snapshot := k8sutil.NewEmptySnapshot()
			if err := cache.UpdateSnapshot(snapshot); err != nil {
				t.Fatal(err)
			}
			candidate, err := snapshot.GetVolcanoNodeInfo("selected")
			if err != nil {
				t.Fatal(err)
			}
			candidates := []*api.NodeInfo{candidate}
			pod := createTestPod("pending", "pending-uid")
			pod.Status.Phase = v1.PodPending
			pod.Spec.SchedulerName = cache.schedulerName
			pod.Spec.Containers = []v1.Container{{Name: "task", Image: "busybox", Resources: v1.ResourceRequirements{Requests: api.BuildResourceList("1", "1Gi")}}}
			task := api.NewTaskInfo(pod)
			if !task.Resreq.LessEqual(candidate.Idle, api.Zero) {
				t.Fatal("task must fit the original worker snapshot")
			}
			cache.AddTaskInfo(task)
			originalStatus := task.Status
			cache.RecordCandidateNodesInBinder(candidates)

			var before *api.NodeInfo
			var beforeGeneration int64
			if tt.removeNode {
				if err := cache.RemoveNode("selected"); err != nil {
					t.Fatal(err)
				}
			} else {
				// An informer-observed Pod fills the two-CPU node after the
				// worker's snapshot, without adding an Agent Binder record.
				other := createTestPod("other", "other-uid")
				other.Spec.NodeName = "selected"
				other.Status.Phase = v1.PodRunning
				other.Spec.SchedulerName = "default-scheduler"
				other.Spec.Containers = []v1.Container{{Name: "task", Image: "busybox", Resources: v1.ResourceRequirements{Requests: api.BuildResourceList("2", "1Gi")}}}
				cache.AddPodToCache(other)
				before = cache.Nodes["selected"].info.Clone()
				beforeGeneration = cache.Nodes["selected"].info.Generation
				if len(before.Tasks) != 1 || task.Resreq.LessEqual(before.Idle, api.Zero) {
					t.Fatal("fixture did not exhaust the selected node's CPU")
				}
			}
			schedCtx := &agentapi.SchedulingContext{Task: task}
			binder.CheckAndBindPod(&agentapi.PodScheduleResult{SchedCtx: schedCtx, SuggestedNodes: candidates, BindContext: &agentapi.BindContext{SchedCtx: schedCtx}})

			if task.NodeName != "" || task.Pod.Spec.NodeName != "" {
				t.Errorf("failed assume retained selected node: task=%q pod=%q", task.NodeName, task.Pod.Spec.NodeName)
			}
			cached, found := cache.GetTaskInfo(task.UID)
			if !found {
				t.Fatal("pending task disappeared from cache")
			}
			if cached.NodeName != "" || cached.Pod.Spec.NodeName != "" || cached.Status != originalStatus {
				t.Errorf("cached task was not restored: task=%q pod=%q status=%v", cached.NodeName, cached.Pod.Spec.NodeName, cached.Status)
			}
			pending, _ := queue.PendingPods()
			if len(pending) != 1 {
				t.Fatalf("queued %d pods, want 1", len(pending))
			}
			if pending[0].Spec.NodeName != "" {
				t.Errorf("requeued Pod still assigned to %q", pending[0].Spec.NodeName)
			}
			if pending[0].Annotations[AnnotationSchedulingPriorityReason] != RequeueReasonBindTaskFailure {
				t.Error("unexpected retry reason")
			}
			if len(cache.NodesInBinder) != 0 || len(binder.nodeBindRecords) != 0 || len(cache.BindFlowChannel) != 0 {
				t.Error("failed assume retained references, a bind generation or downstream work")
			}
			if before != nil {
				after := cache.Nodes["selected"].info
				if !after.Idle.Equal(before.Idle, api.Zero) || !after.Used.Equal(before.Used, api.Zero) || len(after.Tasks) != len(before.Tasks) {
					t.Error("failed assume changed node resources or tasks")
				}
				if after.BindGeneration != before.BindGeneration || after.Generation != beforeGeneration {
					t.Error("failed assume advanced a node generation")
				}
			}
		})
	}
}

func TestRequeuePodWithPriorityWithoutQueue(t *testing.T) {
	binder := &ConflictAwareBinder{}
	task := api.NewTaskInfo(createTestPod("pending", "pending-uid"))
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Errorf("nil queue guard panicked: %v", recovered)
		}
	}()
	binder.requeuePodWithPriority(task, SchedulingPriorityUrgent, RequeueReasonBindTaskFailure)
}
