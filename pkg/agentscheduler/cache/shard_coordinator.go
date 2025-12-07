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

	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/util/sets"
	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type ShardCoordinator struct {
	schedulerShardName     string
	workerStates           []*workerNodeShardState
	nodeShardInfos         map[string]*api.NodeShardInfo // node shards for all schedulers
	schedulerNodeShardInfo *api.NodeShardInfo            // node shard for this scheduler
	nodeToUse              sets.Set[string]              // nodes should be used by workers
	mutex                  sync.RWMutex
	shardingEnabled        bool
	lastSynced             int64 //last synced revision to nodeshard cr
	latestRevision         int64 //last revision of nodeshard cr change
	cache                  Cache
}

type workerNodeShardState struct {
	revision                   int64
	schedulingWithLastRevision bool
}

func NewShardCoordinator(cache Cache, workerCount int, schedulerName string, shardingMode string) *ShardCoordinator {
	klog.V(3).Infof("Shard Coordinator is initialized")

	return &ShardCoordinator{
		schedulerShardName: schedulerName,
		workerStates:       make([]*workerNodeShardState, workerCount),
		shardingEnabled:    shardingMode == options.HardShardingMode || shardingMode == options.SoftShardingMode,
		cache:              cache,
	}
}

// GetNodesForWorker get nodes can be involved in worker
func (sc *ShardCoordinator) GetNodesForWorker(index int) sets.Set[string] {
	klog.V(5).Infof("Worker %d will schedule with nodes%v", index, sc.nodeToUse.UnsortedList())
	if index > len(sc.workerStates) {
		klog.Errorf("Worker %d does not exist, no nodes are returned", index)
		return sets.Set[string]{}
	}
	state := sc.workerStates[index]
	if state == nil {
		sc.workerStates[index] = &workerNodeShardState{revision: sc.latestRevision}
	} else {
		state.revision = sc.latestRevision
	}
	return sc.nodeToUse
}

// RefreshNodeShards update node shards cached in coordinator
func (sc *ShardCoordinator) RefreshNodeShards(nodeShards map[string]*api.NodeShardInfo) {
	if !sc.shardingEnabled {
		return
	}

	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.nodeShardInfos = nodeShards
	shardForSchedulerFound := false
	for shardName, shard := range nodeShards {
		if shardName == sc.schedulerShardName {
			sc.schedulerNodeShardInfo = shard
			shardForSchedulerFound = true
			break
		}
	}
	if !shardForSchedulerFound {
		klog.Errorf("Sharding is enabled but no shard is defined for this scheduler!")
		sc.schedulerNodeShardInfo = nil
		return
	}

	if availableNodes := sc.getAvailableNodesFromShard(); !sc.nodeToUse.Equal(availableNodes) {
		atomic.AddInt64(&sc.latestRevision, 1)
		sc.nodeToUse = availableNodes
		klog.V(3).Infof("Try to update nodeshard status after nodeshard refresh")
		sc.tryUpdateNodeShardStatus()
	}
}

func (sc *ShardCoordinator) tryUpdateNodeShardStatus() {
	latest := atomic.LoadInt64(&sc.latestRevision)
	//skip upate if status has been updated
	if atomic.LoadInt64(&sc.lastSynced) >= latest {
		return
	}

	for index, state := range sc.workerStates {
		if state == nil {
			state = &workerNodeShardState{}
			sc.workerStates[index] = state
		}
		//skip update if any worker is scheduling with nodes before this revision
		if state.schedulingWithLastRevision && state.revision < latest {
			klog.V(3).Infof("Worker %d is scheduling with old nodes, skip nodeshard update", index)
			return
		}
	}

	atomic.StoreInt64(&sc.lastSynced, latest)
	sc.cache.UpdateNodeShardStatus(sc.schedulerShardName, sc.nodeToUse)
}

func (sc *ShardCoordinator) OnWorkerStartSchedulingCycle(index int) {
	if index > len(sc.workerStates) {
		klog.Errorf("Worker %d does not exist", index)
		return
	}

	latest := atomic.LoadInt64(&sc.latestRevision)
	state := sc.workerStates[index]
	if state == nil {
		sc.mutex.Lock()
		sc.workerStates[index] = &workerNodeShardState{
			revision: latest,
		}
		sc.mutex.Unlock()
		return
	}
	//worker has pickup nodes in latest revision, avoid acquiring lock when no revision changed
	if state.revision == latest {
		return
	}

	sc.mutex.Lock()
	state.schedulingWithLastRevision = true
	sc.mutex.Unlock()
}

func (sc *ShardCoordinator) OnWorkerEndSchedulingCycle(index int) {
	if index > len(sc.workerStates) {
		klog.Errorf("Worker %d does not exist", index)
		return
	}
	latest := atomic.LoadInt64(&sc.latestRevision)
	state := sc.workerStates[index]
	if state == nil {
		klog.Errorf("Worker %d state was not initialized before", index)
		return
	}
	//worker has pickup nodes in latest revision, avoid acquiring lock when no revision changed
	if state.revision == latest {
		return
	}

	// worker used nodes in old revision, try to update nodeshard status after worker end
	// because worker in next schedule must pick new nodes.
	sc.mutex.Lock()
	state.schedulingWithLastRevision = false
	klog.V(3).Infof("Try to update nodeshard status after worker %d end scheduling cycle", index)
	sc.tryUpdateNodeShardStatus()
	sc.mutex.Unlock()
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
