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
