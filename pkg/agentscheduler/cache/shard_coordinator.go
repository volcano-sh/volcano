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
}

type workerNodeShardState struct {
	//revisionInScheduing revision of the NodeShard being used in worker scheduling. revisionInScheduing < 0 means no nodes in scheduling cycle
	revisionInScheduing int64
}

func NewShardCoordinator(cache Cache, workerCount int, schedulerName string, shardingMode string) *ShardCoordinator {
	klog.V(3).Infof("Shard Coordinator is initialized")
	workerStates := make([]*workerNodeShardState, workerCount)
	for i := range workerCount {
		workerStates[i] = &workerNodeShardState{}
	}

	return &ShardCoordinator{
		schedulerShardName: schedulerName,
		workerStates:       workerStates,
		shardingEnabled:    shardingMode == util.HardShardingMode || shardingMode == util.SoftShardingMode,
		cache:              cache,
	}
}

// GetNodesForScheduling get nodes available to use for worker
func (sc *ShardCoordinator) GetNodesForScheduling(workerIdx int) sets.Set[string] {
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
	state.revisionInScheduing = latest
	if klog.V(5).Enabled() {
		klog.V(5).Infof("Worker %d will schedule with nodes%v", workerIdx, sc.nodeToUse.UnsortedList())
	}
	return sc.nodeToUse
}

// RefreshNodeShards update node shards cached in coordinator
func (sc *ShardCoordinator) RefreshNodeShards(nodeShards map[string]*api.NodeShardInfo) {
	if !sc.shardingEnabled {
		return
	}
	sc.mutex.Lock()
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
	sc.mutex.Unlock()
	if needUpdate {
		klog.V(3).Infof("Try to update nodeshard status after nodeshard refresh")
		sc.tryUpdateNodeShardStatus()
	}
}

func (sc *ShardCoordinator) tryUpdateNodeShardStatus() {
	latest := atomic.LoadInt64(&sc.latestRevision)
	//skip upate if status has been updated
	if atomic.LoadInt64(&sc.lastSyncedRevision) >= latest {
		return
	}

	update := true
	for index, state := range sc.workerStates {
		//skip update if any worker is scheduling with nodes before this revision
		if state != nil && state.revisionInScheduing > 0 && state.revisionInScheduing < latest {
			klog.V(3).Infof("Worker %d is scheduling with old nodes, skip nodeshard update", index)
			update = false
			break
		}
	}
	if update {
		if err := sc.cache.UpdateNodeShardStatus(sc.schedulerShardName, sc.nodeToUse); err == nil {
			atomic.StoreInt64(&sc.lastSyncedRevision, latest)
		}
	}
}

func (sc *ShardCoordinator) OnWorkerStartSchedulingCycle(index int, schedCtx *agentapi.SchedulingContext) {
	if schedCtx != nil {
		schedCtx.NodesInShard = sc.GetNodesForScheduling(index)
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
	//worker has pickup nodes in latest revisionInSchedulingCycle, avoid trying update when no revisionInSchedulingCycle changed
	revisionInSchedulingCycle := state.revisionInScheduing
	state.revisionInScheduing = 0 //end scheduling, clear revision
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
