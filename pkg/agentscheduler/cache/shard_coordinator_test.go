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
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/util"
)

func TestNewShardCoordinator(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)

	if coordinator == nil {
		t.Fatal("NewShardCoordinator returned nil")
	}

	if coordinator.schedulerShardName != "test-scheduler" {
		t.Errorf("Expected schedulerShardName to be 'test-scheduler', got %s", coordinator.schedulerShardName)
	}

	if len(coordinator.workerStates) != 3 {
		t.Errorf("Expected workerStates length to be 3, got %d", len(coordinator.workerStates))
	}

	if !coordinator.shardingEnabled {
		t.Error("Expected shardingEnabled to be true for HardShardingMode")
	}

	if coordinator.cache != mockCache {
		t.Error("Expected cache to be set correctly")
	}
}

func TestGetNodesForScheduling(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)

	// Set some nodes
	coordinator.nodeToUse = sets.New("node-1", "node-2", "node-3")

	// Test valid worker index
	nodes := coordinator.getNodesForScheduling(0)
	expectedNodes := sets.New("node-1", "node-2", "node-3")

	if !nodes.Equal(expectedNodes) {
		t.Errorf("Expected nodes %v, got %v", expectedNodes, nodes)
	}

	// Test invalid worker index
	nodes = coordinator.getNodesForScheduling(10)
	if len(nodes) != 0 {
		t.Errorf("Expected empty set for invalid worker index, got %v", nodes)
	}
}

func TestGetNodesForScheduling_StateInitialization(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)
	coordinator.latestRevision = 5

	// First call should initialize state and set revision
	coordinator.getNodesForScheduling(1)
	if coordinator.workerStates[1] == nil {
		t.Error("Expected worker state to be initialized")
	}
	if coordinator.workerStates[1].revisionInScheduling != 5 {
		t.Errorf("Expected worker revision to be 5, got %d", coordinator.workerStates[1].revisionInScheduling)
	}

	// Second call should update revision
	coordinator.latestRevision = 10
	coordinator.getNodesForScheduling(1)
	if coordinator.workerStates[1].revisionInScheduling != 10 {
		t.Errorf("Expected worker revision to be updated to 10, got %d", coordinator.workerStates[1].revisionInScheduling)
	}
}

func TestRefreshNodeShards_Disabled(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.NoneShardingMode)

	nodeShards := map[string]*api.NodeShardInfo{
		"test-scheduler": {
			NodeDesired: sets.New("node-1", "node-2"),
		},
	}

	coordinator.RefreshNodeShards(nodeShards)

	// Should not update anything when sharding is disabled
	if coordinator.nodeShardInfos != nil {
		t.Error("Expected nodeShardInfos to remain nil when sharding disabled")
	}
}

func TestRefreshNodeShards_ShardNotFound(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)

	nodeShards := map[string]*api.NodeShardInfo{
		"other-scheduler": {
			NodeDesired: sets.New("node-1", "node-2"),
		},
	}

	coordinator.RefreshNodeShards(nodeShards)

	if coordinator.schedulerNodeShardInfo != nil {
		t.Error("Expected schedulerNodeShardInfo to be nil when shard not found")
	}
}

func TestRefreshNodeShards_Success(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)

	// Initialize worker states and prevent status update by setting lastSyncedRevision high
	for i := range coordinator.workerStates {
		coordinator.workerStates[i] = &workerNodeShardState{
			revisionInScheduling: 1, // Set to 1 so they appear up to date with latest revision
		}
	}
	coordinator.lastSyncedRevision = 100 // Set high to prevent status update

	nodeShards := map[string]*api.NodeShardInfo{
		"test-scheduler": {
			NodeDesired: sets.New("node-1", "node-2", "node-3"),
		},
		"other-scheduler": {
			NodeDesired: sets.New("node-2", "node-3"),
			NodeInUse:   sets.New("node-2"),
		},
	}

	coordinator.RefreshNodeShards(nodeShards)

	// Should have scheduler's shard info
	if coordinator.schedulerNodeShardInfo == nil {
		t.Fatal("Expected schedulerNodeShardInfo to be set")
	}

	// Available nodes should be node-1, node-3 (node-2 is in use by other scheduler)
	expectedNodes := sets.New("node-1", "node-3")
	if !coordinator.nodeToUse.Equal(expectedNodes) {
		t.Errorf("Expected available nodes %v, got %v", expectedNodes, coordinator.nodeToUse)
	}

	// Revision should be incremented
	if coordinator.latestRevision != 1 {
		t.Errorf("Expected latestRevision to be 1, got %d", coordinator.latestRevision)
	}
}

func TestTryUpdateNodeShardStatus(t *testing.T) {
	tests := []struct {
		name                string
		lastSyncedRevision  int64
		latestRevision      int64
		workerStates        []*workerNodeShardState
		expectUpdateAttempt bool
	}{
		{
			name:               "should not attempt update when already synced",
			lastSyncedRevision: 5,
			latestRevision:     5,
			workerStates: []*workerNodeShardState{
				{revisionInScheduling: 5},
				{revisionInScheduling: 5},
				{revisionInScheduling: 5},
			},
			expectUpdateAttempt: false,
		},
		{
			name:               "should not attempt update when lastSynced >= latest",
			lastSyncedRevision: 6,
			latestRevision:     5,
			workerStates: []*workerNodeShardState{
				{revisionInScheduling: 5},
				{revisionInScheduling: 5},
				{revisionInScheduling: 5},
			},
			expectUpdateAttempt: false,
		},
		{
			name:               "should attempt update when all workers are up to date",
			lastSyncedRevision: 4,
			latestRevision:     5,
			workerStates: []*workerNodeShardState{
				{revisionInScheduling: 5}, // Up to date
				{revisionInScheduling: 5}, // Up to date
				{revisionInScheduling: 5}, // Up to date
			},
			expectUpdateAttempt: true,
		},
		{
			name:               "should not attempt update when some workers are actively scheduling old nodes",
			lastSyncedRevision: 0,
			latestRevision:     5,
			workerStates: []*workerNodeShardState{
				{revisionInScheduling: 5}, // Up to date
				{revisionInScheduling: 4}, // Actively scheduling old nodes (3 < 5 and > 0)
				{revisionInScheduling: 5}, // Up to date
			},
			expectUpdateAttempt: false,
		},
		{
			name:               "should attempt update when workers have finished scheduling",
			lastSyncedRevision: 0,
			latestRevision:     5,
			workerStates: []*workerNodeShardState{
				{revisionInScheduling: 0}, // Finished scheduling
				{revisionInScheduling: 0}, // Finished scheduling
				{revisionInScheduling: 0}, // Finished scheduling
			},
			expectUpdateAttempt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCache := NewDefaultMockSchedulerCache("test-scheduler")
			coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)
			coordinator.schedulerShardName = "test-scheduler"
			coordinator.nodeToUse = sets.New("node-1", "node-2")
			coordinator.latestRevision = tt.latestRevision
			coordinator.lastSyncedRevision = tt.lastSyncedRevision
			coordinator.workerStates = tt.workerStates

			updateAttempted := coordinator.tryUpdateNodeShardStatus()

			if updateAttempted != tt.expectUpdateAttempt {
				t.Errorf("Expected tryUpdateNodeShardStatus to return %v, got %v", tt.expectUpdateAttempt, updateAttempted)
			}
		})
	}
}

func TestConcurrentWorkerSchedulingCycleUpdateRequests(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)
	coordinator.lastSyncedRevision = 0
	coordinator.latestRevision = 10
	coordinator.nodeToUse = sets.New("node-1", "node-2")

	// Initialize worker states - all workers start with revision 0 (not scheduling)
	for i := range coordinator.workerStates {
		coordinator.workerStates[i] = &workerNodeShardState{
			revisionInScheduling: 0,
		}
	}

	var wg sync.WaitGroup

	// Simulate concurrent worker scheduling cycles with different timing
	// Some workers will finish while others are still scheduling, testing update triggering
	workerScenarios := []struct {
		workerIndex    int
		startDelay     time.Duration
		schedulingTime time.Duration
	}{
		{0, 0, 3 * time.Millisecond},
		{1, 5 * time.Millisecond, 5 * time.Millisecond},
		{2, 2 * time.Millisecond, 10 * time.Millisecond},
	}

	// All workers are using the latest revision (11) except one executed before update, so only one worker completion should trigger an update
	// since all other workers are using the latest revision
	expectedUpdates := 1
	for _, scenario := range workerScenarios {
		wg.Add(1)
		go func(w int, startDelay, schedulingTime time.Duration) {
			defer wg.Done()

			// Wait before starting
			time.Sleep(startDelay)

			// Start scheduling cycle
			coordinator.OnWorkerStartSchedulingCycle(w, &agentapi.SchedulingContext{})
			atomic.StoreInt64(&coordinator.latestRevision, 11) //mock revision changed before worker end

			// Simulate scheduling work
			time.Sleep(schedulingTime)

			// End scheduling cycle
			coordinator.OnWorkerEndSchedulingCycle(w)
		}(scenario.workerIndex, scenario.startDelay, scenario.schedulingTime)
	}

	// Wait for all workers to complete
	wg.Wait()

	// Check how many update notifications were sent to the channel
	updateNotificationsReceived := 0
	// Drain the channel to count notifications
	for len(coordinator.updateChan) > 0 {
		<-coordinator.updateChan
		updateNotificationsReceived++
	}

	if updateNotificationsReceived != expectedUpdates {
		t.Errorf("Expected %d update notifications, got %d", expectedUpdates, updateNotificationsReceived)
	}

	// Verify that all workers have finished scheduling (revisionInScheduling should be 0)
	for i, state := range coordinator.workerStates {
		if state.revisionInScheduling != 0 {
			t.Errorf("Worker %d should have finished scheduling (revisionInScheduling=0), got %d", i, state.revisionInScheduling)
		}
	}
}

func TestConcurrentWorkerEndSchedulingCycleUpdateTriggering(t *testing.T) {
	mockCache := NewDefaultMockSchedulerCache("test-scheduler")
	coordinator := NewShardCoordinator(mockCache, 3, "test-scheduler", util.HardShardingMode)
	coordinator.lastSyncedRevision = 9
	coordinator.latestRevision = 10

	// Test different scenarios where update should be triggered
	tests := []struct {
		name                  string
		setupWorkerStates     func()
		workerToTest          int
		expectUpdateTriggered bool
		description           string
	}{
		{
			name: "worker finishes with old revision while others are up to date",
			setupWorkerStates: func() {
				coordinator.workerStates[0] = &workerNodeShardState{revisionInScheduling: 9}  // Old revision
				coordinator.workerStates[1] = &workerNodeShardState{revisionInScheduling: 10} // Latest
				coordinator.workerStates[2] = &workerNodeShardState{revisionInScheduling: 10} // Latest
			},
			workerToTest:          0,
			expectUpdateTriggered: true,
			description:           "Worker with old revision should trigger update when others are current",
		},
		{
			name: "worker finishes with latest revision",
			setupWorkerStates: func() {
				coordinator.workerStates[0] = &workerNodeShardState{revisionInScheduling: 10} // Latest
				coordinator.workerStates[1] = &workerNodeShardState{revisionInScheduling: 10} // Latest
				coordinator.workerStates[2] = &workerNodeShardState{revisionInScheduling: 10} // Latest
			},
			workerToTest:          0,
			expectUpdateTriggered: false,
			description:           "Worker with latest revision should not trigger update",
		},
		{
			name: "worker finishes while others are still scheduling old nodes",
			setupWorkerStates: func() {
				coordinator.workerStates[0] = &workerNodeShardState{revisionInScheduling: 9}  // Old revision
				coordinator.workerStates[1] = &workerNodeShardState{revisionInScheduling: 9}  // Older than latest
				coordinator.workerStates[2] = &workerNodeShardState{revisionInScheduling: 10} // Latest
			},
			workerToTest:          0,
			expectUpdateTriggered: false,
			description:           "Update should be blocked when other workers are scheduling old nodes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup initial state
			tt.setupWorkerStates()

			// Clear any existing notifications in the channel
			for len(coordinator.updateChan) > 0 {
				<-coordinator.updateChan
			}

			// Execute the worker end scheduling cycle
			coordinator.OnWorkerEndSchedulingCycle(tt.workerToTest)

			// Check if update notification was sent to channel
			select {
			case <-coordinator.updateChan:
				if !tt.expectUpdateTriggered {
					t.Errorf("%s: Expected no update notification, but received one", tt.description)
				}
			default:
				if tt.expectUpdateTriggered {
					t.Errorf("%s: Expected update notification, but none received", tt.description)
				}
			}

			// Verify worker state is reset
			if coordinator.workerStates[tt.workerToTest].revisionInScheduling != 0 {
				t.Errorf("Expected worker %d revisionInScheduling to be reset to 0, got %d",
					tt.workerToTest, coordinator.workerStates[tt.workerToTest].revisionInScheduling)
			}
		})
	}
}
