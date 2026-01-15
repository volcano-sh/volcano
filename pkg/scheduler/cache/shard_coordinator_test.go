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
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	nodeshardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	commonutil "volcano.sh/volcano/pkg/util"
)

// countingStatusUpdater is a mock StatusUpdater that counts UpdateNodeShardStatus calls
type countingStatusUpdater struct {
	updateNodeShardStatusCount int
	lastUpdateShardName        string
	mutex                      sync.Mutex
}

func (c *countingStatusUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	return pod, nil
}

func (c *countingStatusUpdater) UpdatePodGroup(pg *api.PodGroup) (*api.PodGroup, error) {
	return pg, nil
}

func (c *countingStatusUpdater) UpdateQueueStatus(queue *api.QueueInfo) error {
	return nil
}

func (c *countingStatusUpdater) UpdateNodeShardStatus(nodeshard *nodeshardv1alpha1.NodeShard) (*nodeshardv1alpha1.NodeShard, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.updateNodeShardStatusCount++
	c.lastUpdateShardName = nodeshard.Name
	return nodeshard, nil // Return success without actual k8s call
}

func TestGetAvailableNodesFromShard(t *testing.T) {
	// Setup test data
	nodeShard := &api.NodeShardInfo{
		Name:        "test-shard",
		NodeDesired: sets.New("node-1", "node-2", "node-3", "node-4"),
		NodeInUse:   sets.New("node-1", "node-2"),
	}

	otherShard1 := &api.NodeShardInfo{
		Name:        "other-shard-1",
		NodeDesired: sets.New("node-2", "node-3"),
		NodeInUse:   sets.New("node-2"),
	}

	otherShard2 := &api.NodeShardInfo{
		Name:        "other-shard-2",
		NodeDesired: sets.New("node-3", "node-4"),
		NodeInUse:   sets.New("node-3", "node-4"),
	}

	nodeShards := map[string]*api.NodeShardInfo{
		"test-shard":    nodeShard,
		"other-shard-1": otherShard1,
		"other-shard-2": otherShard2,
	}

	sc := &SchedulerCache{
		NodeShards: nodeShards,
	}

	tests := []struct {
		name          string
		shardName     string
		expectedNodes sets.Set[string]
		description   string
	}{
		{
			name:          "basic calculation",
			shardName:     "test-shard",
			expectedNodes: sets.New("node-1"), // node-2, node-3, node-4 are used by other shards
			description:   "Available nodes should exclude nodes used by other shards",
		},
		{
			name:          "no overlapping usage",
			shardName:     "other-shard-1",
			expectedNodes: sets.New[string](), // node-2 is used by test-shard, node-3 is used by other-shard-2
			description:   "Should calculate available nodes correctly when there are overlapping usages",
		},
		{
			name:          "all nodes used by others",
			shardName:     "other-shard-2",
			expectedNodes: sets.New("node-3", "node-4"), // node-3 and node-4 are not used by other shards
			description:   "Should handle case where all desired nodes are used by other shards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shardInfo := nodeShards[tt.shardName]
			result := sc.getAvailableNodesFromShard(shardInfo)

			if !result.Equal(tt.expectedNodes) {
				t.Errorf("%s: expected available nodes %v, got %v",
					tt.description, tt.expectedNodes, result)
			}
		})
	}
}

func TestRefreshNodeShards_AvailableNodesCalculation(t *testing.T) {
	// Setup mock server options
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()
	options.ServerOpts = &options.ServerOption{
		ShardingMode: commonutil.HardShardingMode,
		ShardName:    "test-shard",
	}

	// Setup test data
	nodeShard := schedulingapi.NewNodeShardInfo(buildShard("test-shard", []string{"node-1", "node-2", "node-3"}, []string{"node-1"}))
	otherShard := schedulingapi.NewNodeShardInfo(buildShard("other-shard", []string{"node-2", "node-3"}, []string{"node-2"}))

	tests := []struct {
		name                string
		initialSessionState bool
		multipleCalls       int
		expectedUpdateCount int
		description         string
	}{
		{
			name:                "session running - single call",
			initialSessionState: true,
			multipleCalls:       1,
			expectedUpdateCount: 1,
			description:         "Single refresh call when session is running should defer update",
		},
		{
			name:                "session running - multiple calls",
			initialSessionState: true,
			multipleCalls:       3,
			expectedUpdateCount: 1,
			description:         "Multiple refresh calls when session is running should only execute update once",
		},
		{
			name:                "session not running - single call",
			initialSessionState: false,
			multipleCalls:       1,
			expectedUpdateCount: 1,
			description:         "Single refresh call when session is not running should execute immediately",
		},
		{
			name:                "session not running - multiple calls",
			initialSessionState: false,
			multipleCalls:       3,
			expectedUpdateCount: 3,
			description:         "Multiple refresh calls when session is not running should execute immediately each time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create counting status updater to track UpdateNodeShardStatus calls
			countingUpdater := &countingStatusUpdater{}

			sc := &SchedulerCache{
				NodeShards: map[string]*api.NodeShardInfo{
					"test-shard":  nodeShard,
					"other-shard": otherShard,
				},
				InUseNodesInShard:      sets.New("node-1"), // Initially only node-1 is in use
				shardUpdateCoordinator: NewShardUpdateCoordinator(),
				StatusUpdater:          countingUpdater,
			}

			// Set initial session state
			sc.shardUpdateCoordinator.IsSessionRunning.Store(tt.initialSessionState)

			// Make refresh calls - use multipleCalls as the actual call count
			for i := 0; i < tt.multipleCalls; i++ {
				sc.Mutex.Lock()
				sc.RefreshNodeShards()
				sc.Mutex.Unlock()
			}
			// Give some time for goroutines to execute
			time.Sleep(10 * time.Millisecond)
			if tt.initialSessionState {
				// If session was running, simulate session end to trigger deferred updates
				sc.OnSessionClose()
			}
			// Give some time for goroutines to execute
			time.Sleep(10 * time.Millisecond)

			// Verify available nodes calculation is correct regardless of session state or call count
			expectedAvailableNodes := sets.New("node-1", "node-3") // node-2 is used by other-shard
			if !sc.InUseNodesInShard.Equal(expectedAvailableNodes) {
				t.Errorf("%s: Expected available nodes %v, got %v",
					tt.description, expectedAvailableNodes, sc.InUseNodesInShard)
			}

			// Verify that pending flag is reset after processing
			if sc.shardUpdateCoordinator.ShardUpdatePending.Load() {
				t.Errorf("%s: Expected ShardUpdatePending to be false after processing, but it was true", tt.description)
			}

			countingUpdater.mutex.Lock()
			// Verify UpdateNodeShardStatus call count using expectedUpdateCount from test case
			if countingUpdater.updateNodeShardStatusCount != tt.expectedUpdateCount {
				t.Errorf("%s: Expected UpdateNodeShardStatus to be called %d time(s), but was called %d time(s)",
					tt.description, tt.expectedUpdateCount, countingUpdater.updateNodeShardStatusCount)
			}

			// Verify the correct shard name was passed
			if countingUpdater.lastUpdateShardName != "test-shard" {
				t.Errorf("%s: Expected UpdateNodeShardStatus to be called with 'test-shard', but got '%s'",
					tt.description, countingUpdater.lastUpdateShardName)
			}
			countingUpdater.mutex.Unlock()
		})
	}
}

func buildShard(name string, desired []string, inuse []string) *nodeshardv1alpha1.NodeShard {
	return &nodeshardv1alpha1.NodeShard{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: nodeshardv1alpha1.NodeShardSpec{
			NodesDesired: desired,
		},
		Status: nodeshardv1alpha1.NodeShardStatus{
			NodesInUse: inuse,
		},
	}
}

func TestRefreshNodeShards_ShardNotFound(t *testing.T) {
	// Setup mock server options
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()
	options.ServerOpts = &options.ServerOption{
		ShardingMode: commonutil.HardShardingMode,
		ShardName:    "non-existent-shard",
	}

	sc := &SchedulerCache{
		NodeShards: map[string]*api.NodeShardInfo{
			"test-shard": {
				Name:        "test-shard",
				NodeDesired: sets.New("node-1", "node-2"),
			},
		},
		InUseNodesInShard: sets.New("node-1"),
	}

	// Test RefreshNodeShards with non-existent shard
	sc.RefreshNodeShards()

	// Should not change InUseNodesInShard when shard is not found
	if !sc.InUseNodesInShard.Equal(sets.New("node-1")) {
		t.Errorf("Expected InUseNodesInShard to remain unchanged when shard not found, got %v",
			sc.InUseNodesInShard)
	}
}

// TestConcurrentRefreshNodeShards tests concurrent calls to RefreshNodeShards
func TestConcurrentRefreshNodeShards(t *testing.T) {
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()
	options.ServerOpts = &options.ServerOption{
		ShardingMode: commonutil.HardShardingMode,
		ShardName:    "test-shard",
	}

	nodeShard := schedulingapi.NewNodeShardInfo(buildShard("test-shard", []string{"node-1", "node-2", "node-3"}, []string{"node-1"}))
	otherShard := schedulingapi.NewNodeShardInfo(buildShard("other-shard", []string{"node-2", "node-3"}, []string{"node-2"}))

	countingUpdater := &countingStatusUpdater{}
	sc := &SchedulerCache{
		NodeShards: map[string]*api.NodeShardInfo{
			"test-shard":  nodeShard,
			"other-shard": otherShard,
		},
		InUseNodesInShard:      sets.New("node-1"),
		shardUpdateCoordinator: NewShardUpdateCoordinator(),
		StatusUpdater:          countingUpdater,
	}

	// Test concurrent refresh calls when session is running
	sc.shardUpdateCoordinator.IsSessionRunning.Store(true)

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			sc.Mutex.Lock()
			sc.RefreshNodeShards()
			sc.Mutex.Unlock()
		}()
	}

	wg.Wait()
	time.Sleep(20 * time.Millisecond)

	// Close session to trigger pending update
	sc.OnSessionClose()
	time.Sleep(20 * time.Millisecond)

	// Verify only one update was executed despite multiple concurrent calls
	countingUpdater.mutex.Lock()
	if countingUpdater.updateNodeShardStatusCount != 1 {
		t.Errorf("Expected exactly 1 status update from concurrent calls, got %d", countingUpdater.updateNodeShardStatusCount)
	}
	countingUpdater.mutex.Unlock()

	// Verify pending flag is reset
	if sc.shardUpdateCoordinator.ShardUpdatePending.Load() {
		t.Error("Expected ShardUpdatePending to be false after processing")
	}
}

// TestGetAvailableNodesFromShard_EdgeCases tests edge cases for available nodes calculation
func TestGetAvailableNodesFromShard_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		targetShard   *api.NodeShardInfo
		otherShards   map[string]*api.NodeShardInfo
		expectedNodes sets.Set[string]
		description   string
	}{
		{
			name: "empty desired nodes",
			targetShard: &api.NodeShardInfo{
				Name:        "empty-shard",
				NodeDesired: sets.New[string](),
				NodeInUse:   sets.New[string](),
			},
			otherShards:   map[string]*api.NodeShardInfo{},
			expectedNodes: sets.New[string](),
			description:   "Shard with no desired nodes should return empty set",
		},
		{
			name: "all nodes available",
			targetShard: &api.NodeShardInfo{
				Name:        "available-shard",
				NodeDesired: sets.New("node-1", "node-2", "node-3"),
				NodeInUse:   sets.New[string](),
			},
			otherShards:   map[string]*api.NodeShardInfo{},
			expectedNodes: sets.New("node-1", "node-2", "node-3"),
			description:   "All desired nodes should be available when no other shards",
		},
		{
			name: "all nodes used by other shards",
			targetShard: &api.NodeShardInfo{
				Name:        "blocked-shard",
				NodeDesired: sets.New("node-1", "node-2"),
				NodeInUse:   sets.New[string](),
			},
			otherShards: map[string]*api.NodeShardInfo{
				"other-shard": {
					Name:        "other-shard",
					NodeDesired: sets.New("node-1", "node-2", "node-3"),
					NodeInUse:   sets.New("node-1", "node-2"),
				},
			},
			expectedNodes: sets.New[string](),
			description:   "No nodes available when all are used by other shards",
		},
		{
			name: "partial overlap with multiple shards",
			targetShard: &api.NodeShardInfo{
				Name:        "target-shard",
				NodeDesired: sets.New("node-1", "node-2", "node-3", "node-4", "node-5"),
				NodeInUse:   sets.New[string](),
			},
			otherShards: map[string]*api.NodeShardInfo{
				"shard-1": {
					Name:        "shard-1",
					NodeDesired: sets.New("node-1", "node-2"),
					NodeInUse:   sets.New("node-1"),
				},
				"shard-2": {
					Name:        "shard-2",
					NodeDesired: sets.New("node-3", "node-4"),
					NodeInUse:   sets.New("node-3"),
				},
			},
			expectedNodes: sets.New("node-2", "node-4", "node-5"),
			description:   "Should exclude nodes in use by multiple other shards",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allShards := make(map[string]*api.NodeShardInfo)
			allShards[tt.targetShard.Name] = tt.targetShard
			for name, shard := range tt.otherShards {
				allShards[name] = shard
			}

			sc := &SchedulerCache{
				NodeShards: allShards,
			}

			result := sc.getAvailableNodesFromShard(tt.targetShard)
			if !result.Equal(tt.expectedNodes) {
				t.Errorf("%s: expected %v, got %v", tt.description, tt.expectedNodes, result)
			}
		})
	}
}

// TestGenerateNodeShardWithStatus tests the status generation logic
func TestGenerateNodeShardWithStatus(t *testing.T) {
	tests := []struct {
		name               string
		shardName          string
		currentStatus      nodeshardv1alpha1.NodeShardStatus
		inUseNodes         sets.Set[string]
		desiredNodes       []string
		expectNil          bool
		expectedNodesInUse []string
		expectedToAdd      []string
		expectedToRemove   []string
		description        string
	}{
		{
			name:      "no status change",
			shardName: "test-shard",
			currentStatus: nodeshardv1alpha1.NodeShardStatus{
				NodesInUse:    []string{"node-1", "node-2"},
				NodesToAdd:    []string{"node-3"},
				NodesToRemove: []string{},
			},
			inUseNodes:   sets.New("node-1", "node-2"),
			desiredNodes: []string{"node-1", "node-2", "node-3"},
			expectNil:    true,
			description:  "Should return nil when status hasn't changed",
		},
		{
			name:      "nodes added to desired",
			shardName: "test-shard",
			currentStatus: nodeshardv1alpha1.NodeShardStatus{
				NodesInUse:    []string{"node-1"},
				NodesToAdd:    []string{},
				NodesToRemove: []string{},
			},
			inUseNodes:         sets.New("node-1"),
			desiredNodes:       []string{"node-1", "node-2", "node-3"},
			expectNil:          false,
			expectedNodesInUse: []string{"node-1"},
			expectedToAdd:      []string{"node-2", "node-3"},
			expectedToRemove:   []string{},
			description:        "Should detect new nodes to add",
		},
		{
			name:      "nodes removed from desired",
			shardName: "test-shard",
			currentStatus: nodeshardv1alpha1.NodeShardStatus{
				NodesInUse:    []string{"node-1", "node-2", "node-3"},
				NodesToAdd:    []string{},
				NodesToRemove: []string{},
			},
			inUseNodes:         sets.New("node-1", "node-2", "node-3"),
			desiredNodes:       []string{"node-1"},
			expectNil:          false,
			expectedNodesInUse: []string{"node-1", "node-2", "node-3"},
			expectedToAdd:      []string{},
			expectedToRemove:   []string{"node-2", "node-3"},
			description:        "Should detect nodes to remove",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shard := &nodeshardv1alpha1.NodeShard{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.shardName,
				},
				Spec: nodeshardv1alpha1.NodeShardSpec{
					NodesDesired: tt.desiredNodes,
				},
				Status: tt.currentStatus,
			}

			sc := &SchedulerCache{
				NodeShards: map[string]*api.NodeShardInfo{
					tt.shardName: schedulingapi.NewNodeShardInfo(shard),
				},
				InUseNodesInShard: tt.inUseNodes,
			}

			result := sc.generateNodeShardWithStatus(tt.shardName)

			if tt.expectNil {
				if result != nil {
					t.Errorf("%s: expected nil result, got %v", tt.description, result)
				}
				return
			}

			if result == nil {
				t.Errorf("%s: expected non-nil result", tt.description)
				return
			}

			// Verify NodesInUse
			resultNodesInUse := sets.New(result.Status.NodesInUse...)
			expectedNodesInUse := sets.New(tt.expectedNodesInUse...)
			if !resultNodesInUse.Equal(expectedNodesInUse) {
				t.Errorf("%s: NodesInUse mismatch, expected %v, got %v",
					tt.description, tt.expectedNodesInUse, result.Status.NodesInUse)
			}

			// Verify NodesToAdd
			resultToAdd := sets.New(result.Status.NodesToAdd...)
			expectedToAdd := sets.New(tt.expectedToAdd...)
			if !resultToAdd.Equal(expectedToAdd) {
				t.Errorf("%s: NodesToAdd mismatch, expected %v, got %v",
					tt.description, tt.expectedToAdd, result.Status.NodesToAdd)
			}

			// Verify NodesToRemove
			resultToRemove := sets.New(result.Status.NodesToRemove...)
			expectedToRemove := sets.New(tt.expectedToRemove...)
			if !resultToRemove.Equal(expectedToRemove) {
				t.Errorf("%s: NodesToRemove mismatch, expected %v, got %v",
					tt.description, tt.expectedToRemove, result.Status.NodesToRemove)
			}
		})
	}
}

// TestGenerateNodeShardWithStatus_NonExistentShard tests error handling for non-existent shard
func TestGenerateNodeShardWithStatus_NonExistentShard(t *testing.T) {
	sc := &SchedulerCache{
		NodeShards:        map[string]*api.NodeShardInfo{},
		InUseNodesInShard: sets.New("node-1"),
	}

	result := sc.generateNodeShardWithStatus("non-existent")
	if result != nil {
		t.Errorf("Expected nil for non-existent shard, got %v", result)
	}
}

// TestSessionLifecycle tests the complete session lifecycle with shard updates
func TestSessionLifecycle(t *testing.T) {
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()
	options.ServerOpts = &options.ServerOption{
		ShardingMode: commonutil.HardShardingMode,
		ShardName:    "test-shard",
	}

	nodeShard := schedulingapi.NewNodeShardInfo(buildShard("test-shard", []string{"node-1", "node-2"}, []string{"node-1"}))
	countingUpdater := &countingStatusUpdater{}

	sc := &SchedulerCache{
		NodeShards: map[string]*api.NodeShardInfo{
			"test-shard": nodeShard,
		},
		InUseNodesInShard:      sets.New("node-1"),
		shardUpdateCoordinator: NewShardUpdateCoordinator(),
		StatusUpdater:          countingUpdater,
	}

	// Start session
	sc.shardUpdateCoordinator.IsSessionRunning.Store(true)

	// Trigger refresh during session
	sc.Mutex.Lock()
	sc.RefreshNodeShards()
	sc.Mutex.Unlock()

	time.Sleep(10 * time.Millisecond)

	// Verify update is pending
	if !sc.shardUpdateCoordinator.ShardUpdatePending.Load() {
		t.Error("Expected update to be pending during session")
	}

	// Verify no update has been executed yet
	countingUpdater.mutex.Lock()
	if countingUpdater.updateNodeShardStatusCount != 0 {
		t.Errorf("Expected 0 updates during session, got %d", countingUpdater.updateNodeShardStatusCount)
	}
	countingUpdater.mutex.Unlock()

	// End session
	sc.OnSessionClose()
	time.Sleep(20 * time.Millisecond)

	// Verify update was executed after session ended
	countingUpdater.mutex.Lock()
	if countingUpdater.updateNodeShardStatusCount != 1 {
		t.Errorf("Expected 1 update after session end, got %d", countingUpdater.updateNodeShardStatusCount)
	}
	countingUpdater.mutex.Unlock()

	// Verify pending flag is reset
	if sc.shardUpdateCoordinator.ShardUpdatePending.Load() {
		t.Error("Expected pending flag to be reset after session end")
	}
}

// TestRapidSessionCycles tests rapid session open/close cycles
func TestRapidSessionCycles(t *testing.T) {
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()
	options.ServerOpts = &options.ServerOption{
		ShardingMode: commonutil.HardShardingMode,
		ShardName:    "test-shard",
	}

	nodeShard := schedulingapi.NewNodeShardInfo(buildShard("test-shard", []string{"node-1", "node-2"}, []string{"node-1"}))
	countingUpdater := &countingStatusUpdater{}

	sc := &SchedulerCache{
		NodeShards: map[string]*api.NodeShardInfo{
			"test-shard": nodeShard,
		},
		InUseNodesInShard:      sets.New("node-1"),
		shardUpdateCoordinator: NewShardUpdateCoordinator(),
		StatusUpdater:          countingUpdater,
	}

	// Simulate rapid session cycles
	for i := 0; i < 5; i++ {
		// Start session
		sc.shardUpdateCoordinator.IsSessionRunning.Store(true)

		// Trigger refresh
		sc.Mutex.Lock()
		sc.RefreshNodeShards()
		sc.Mutex.Unlock()

		time.Sleep(5 * time.Millisecond)

		// End session
		sc.OnSessionClose()
		time.Sleep(5 * time.Millisecond)
	}

	// Allow all goroutines to complete
	time.Sleep(50 * time.Millisecond)

	// Verify updates were executed (should be at least 1, possibly more depending on timing)
	countingUpdater.mutex.Lock()
	updateCount := countingUpdater.updateNodeShardStatusCount
	countingUpdater.mutex.Unlock()

	if updateCount < 1 {
		t.Errorf("Expected at least 1 update from rapid cycles, got %d", updateCount)
	}

	// Verify no pending updates remain
	if sc.shardUpdateCoordinator.ShardUpdatePending.Load() {
		t.Error("Expected no pending updates after all cycles complete")
	}
}

// TestRefreshNodeShards_DisabledSharding tests behavior when sharding is disabled
func TestRefreshNodeShards_DisabledSharding(t *testing.T) {
	originalOpts := options.ServerOpts
	defer func() {
		options.ServerOpts = originalOpts
	}()

	tests := []struct {
		name         string
		shardingMode string
		description  string
	}{
		{
			name:         "no sharding mode",
			shardingMode: "",
			description:  "Should return early when sharding mode is empty",
		},
		{
			name:         "unknown sharding mode",
			shardingMode: "unknown-mode",
			description:  "Should return early when sharding mode is unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options.ServerOpts = &options.ServerOption{
				ShardingMode: tt.shardingMode,
				ShardName:    "test-shard",
			}

			sc := &SchedulerCache{
				NodeShards: map[string]*api.NodeShardInfo{
					"test-shard": {
						Name:        "test-shard",
						NodeDesired: sets.New("node-1", "node-2"),
					},
				},
				InUseNodesInShard: sets.New("node-1"),
			}

			initialNodes := sc.InUseNodesInShard.Clone()
			sc.RefreshNodeShards()

			// Verify InUseNodesInShard was not modified
			if !sc.InUseNodesInShard.Equal(initialNodes) {
				t.Errorf("%s: Expected InUseNodesInShard to remain unchanged, got %v",
					tt.description, sc.InUseNodesInShard)
			}
		})
	}
}
