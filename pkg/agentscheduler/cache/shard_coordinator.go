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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/util"
)

type ShardCoordinator struct {
	schedulerShardName     string
	workerStates           []*workerNodeShardState
	nodeShardInfos         map[string]*api.NodeShardInfo // node shards for all schedulers
	schedulerNodeShardInfo *api.NodeShardInfo            // node shard for this scheduler
	nodeToUse              sets.Set[string]              // nodes should be used by workers
	mutex                  sync.RWMutex
	shardingEnabled        bool
	lastSyncedRevision     int64 //revision in last synchronization to nodeshard cr
	latestRevision         int64 //last revision of nodeshard cr change
	cache                  Cache
	updateChan             chan struct{} // channel for update notifications
}

type workerNodeShardState struct {
	//revisionInScheduling revision of the NodeShard being used in worker scheduling. revisionInScheduling < 0 means no nodes in scheduling cycle
	revisionInScheduling int64
}

func NewShardCoordinator(cache Cache, workerCount int, shardName string, shardingMode string) *ShardCoordinator {
	klog.V(3).Infof("Shard Coordinator is initialized")
	workerStates := make([]*workerNodeShardState, workerCount)
	for i := range workerCount {
		workerStates[i] = &workerNodeShardState{}
	}

	sc := &ShardCoordinator{
		schedulerShardName: shardName,
		workerStates:       workerStates,
		shardingEnabled:    shardingMode == util.HardShardingMode || shardingMode == util.SoftShardingMode,
		cache:              cache,
		updateChan:         make(chan struct{}, 100), // buffered channel to handle multiple update requests
	}
	return sc
}

func (sc *ShardCoordinator) Run(stopCh <-chan struct{}) {
	// Start the update processing goroutine
	go sc.processUpdates(stopCh)
}

// getNodesForScheduling get nodes available to use for worker
func (sc *ShardCoordinator) getNodesForScheduling(workerIdx int) sets.Set[string] {
	if workerIdx >= len(sc.workerStates) {
		klog.Errorf("Worker %d does not exist, no nodes are returned", workerIdx)
		return sets.Set[string]{}
	}
	state := sc.workerStates[workerIdx]
	if state == nil {
		klog.Errorf("Worker %d state is not inited, no nodes are returned", workerIdx)
		return sets.Set[string]{}
	}
	sc.mutex.RLock()
	defer sc.mutex.RUnlock()
	latest := atomic.LoadInt64(&sc.latestRevision)
	atomic.StoreInt64(&state.revisionInScheduling, latest)
	klog.V(5).Infof("Worker %d will schedule with nodes%v", workerIdx, sc.nodeToUse.UnsortedList())
	return sc.nodeToUse
}

// RefreshNodeShards update node shards cached in coordinator
func (sc *ShardCoordinator) RefreshNodeShards(nodeShards map[string]*api.NodeShardInfo) {
	if !sc.shardingEnabled {
		return
	}
	if sc.checkAndUpdateShards(nodeShards) {
		klog.V(3).Infof("Try to update nodeshard status after nodeshard refresh")
		sc.tryUpdateNodeShardStatus()
	}
}

// checkAndUpdateShards update shard and node stored in ShardCoordinator and return is NodeShard need be updated because of the update.
func (sc *ShardCoordinator) checkAndUpdateShards(nodeShards map[string]*api.NodeShardInfo) bool {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.nodeShardInfos = nodeShards
	shardForSchedulerFound := false
	desiredNodesChanged := false
	for shardName, shard := range nodeShards {
		if shardName == sc.schedulerShardName {
			if sc.schedulerNodeShardInfo == nil || !shard.NodeDesired.Equal(sc.schedulerNodeShardInfo.NodeDesired) {
				desiredNodesChanged = true
			}
			sc.schedulerNodeShardInfo = shard
			shardForSchedulerFound = true
			break
		}
	}
	needUpdate := false
	if !shardForSchedulerFound {
		klog.Errorf("Sharding is enabled but no shard is defined for this scheduler!")
		sc.schedulerNodeShardInfo = nil
	} else if availableNodes := sc.getAvailableNodesFromShard(); desiredNodesChanged || !sc.nodeToUse.Equal(availableNodes) {
		atomic.AddInt64(&sc.latestRevision, 1)
		sc.nodeToUse = availableNodes
		needUpdate = true
	}
	return needUpdate
}

// tryUpdateNodeShardStatus try to update status of nodeshard if no worker is using nodes in last revision of nodeshard
func (sc *ShardCoordinator) tryUpdateNodeShardStatus() bool {
	latest := atomic.LoadInt64(&sc.latestRevision)
	//skip update if status has been updated
	if atomic.LoadInt64(&sc.lastSyncedRevision) >= latest {
		klog.V(3).Info("last updated revision is new than revision in coordinator, skip nodeshard update")
		return false
	}

	update := true
	for index, state := range sc.workerStates {
		//skip update if any worker is scheduling with nodes before this revision
		p := &state.revisionInScheduling
		if p == nil {
			continue
		}
		revision := atomic.LoadInt64(p)
		if state != nil && revision > 0 && revision < latest {
			klog.V(3).Infof("Worker %d is scheduling with old nodes, skip nodeshard update", index)
			update = false
			break
		}
	}
	if update {
		// Send update notification to channel instead of calling directly
		select {
		case sc.updateChan <- struct{}{}:
			klog.V(3).Info("Sent update notification to channel")
		default:
			klog.Error("Update channel is full, skipping notification")
			update = false
		}
	}
	return update
}

// processUpdates handles update notifications from the channel
func (sc *ShardCoordinator) processUpdates(stopCh <-chan struct{}) {
	maxBatch := 10
	for {
		select {
		case <-sc.updateChan:
			// Drain all pending updates from the channel
			updateCount := 1
			for len(sc.updateChan) > 0 && updateCount < maxBatch {
				<-sc.updateChan
				updateCount++
			}
			klog.V(3).Infof("Processing %d update nodeshard notifications", updateCount)

			// Perform the actual update
			sc.performUpdate()

		case <-stopCh:
			klog.V(3).Infof("Stopping update nodeshard processing goroutine")
			return
		}
	}
}

// performUpdate executes the actual UpdateNodeShardStatus call
func (sc *ShardCoordinator) performUpdate() {
	latest := atomic.LoadInt64(&sc.latestRevision)
	// Double-check if update is still needed
	if atomic.LoadInt64(&sc.lastSyncedRevision) >= latest {
		return
	}

	if err := sc.cache.UpdateNodeShardStatus(sc.schedulerShardName, sc.nodeToUse); err == nil {
		atomic.StoreInt64(&sc.lastSyncedRevision, latest)
		klog.V(3).Infof("Successfully updated NodeShard status")
	} else {
		klog.Errorf("Failed to update NodeShard status: %v", err)
	}
}

func (sc *ShardCoordinator) OnWorkerStartSchedulingCycle(index int, schedCtx *agentapi.SchedulingContext) {
	if schedCtx != nil {
		schedCtx.NodesInShard = sc.getNodesForScheduling(index)
	}
}

func (sc *ShardCoordinator) OnWorkerEndSchedulingCycle(index int) {
	if index >= len(sc.workerStates) {
		klog.Errorf("Worker %d does not exist", index)
		return
	}
	latest := atomic.LoadInt64(&sc.latestRevision)
	state := sc.workerStates[index]
	if state == nil {
		klog.Errorf("Worker %d state should not be nil when end scheduling cycle", index)
		return
	}
	revisionInSchedulingCycle := state.revisionInScheduling
	atomic.StoreInt64(&state.revisionInScheduling, 0) //end scheduling, clear revision

	//worker has pickup nodes in latest revisionInSchedulingCycle, avoid trying update when no revisionInSchedulingCycle changed
	if revisionInSchedulingCycle == latest {
		return
	}

	// worker used nodes in old revision, try to update nodeshard status after worker end scheduling
	// because worker in next schedule must pick new nodes, the nodes being used by this scheduler may change.
	klog.V(3).Infof("Try to update nodeshard status after worker %d end scheduling cycle", index)
	sc.tryUpdateNodeShardStatus()
}

// getAvailableNodesFromShard get available nodes based on desired nodes. Nodes are still being used in other shard should not be put into available nodes
func (sc *ShardCoordinator) getAvailableNodesFromShard() sets.Set[string] {
	nodes := sc.schedulerNodeShardInfo.NodeDesired
	for shardName, nodeShard := range sc.nodeShardInfos {
		if shardName != sc.schedulerShardName {
			nodes = nodes.Difference(nodeShard.NodeInUse)
		}
	}
	return nodes
}
