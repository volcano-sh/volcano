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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/util"
)

type ShardUpdateCoordinator struct {
	ShardUpdateMu      sync.Mutex
	ShardUpdateCond    *sync.Cond
	IsSessionRunning   bool
	ShardUpdatePending bool
}

func NewShardUpdateCoordinator() *ShardUpdateCoordinator {
	coordinator := &ShardUpdateCoordinator{}
	coordinator.ShardUpdateCond = sync.NewCond(&coordinator.ShardUpdateMu)
	return coordinator
}

// RefreshNodeShards update node shards cached in coordinator
func (sc *SchedulerCache) RefreshNodeShards() {
	if options.ServerOpts.ShardingMode != util.HardShardingMode && options.ServerOpts.ShardingMode != util.SoftShardingMode {
		return
	}
	var nodeShardInfo *api.NodeShardInfo
	for shardName, shard := range sc.NodeShards {
		if shardName == options.ServerOpts.ShardName {
			nodeShardInfo = shard
			// shardForSchedulerFound = true
			break
		}
	}
	if nodeShardInfo == nil {
		klog.Errorf("Sharding is enabled but no shard is defined for this scheduler!")
		// sc.schedulerNodeShardInfo = nil
		return
	}

	if availableNodes := sc.getAvailableNodesFromShard(nodeShardInfo); !sc.InUseNodesInShard.Equal(availableNodes) {
		sc.InUseNodesInShard = availableNodes
		klog.V(3).Infof("Try to update nodeshard status after nodeshard refresh")
		go sc.tryUpdateNodeShardStatus(nodeShardInfo.Name)
	}
}

func (sc *SchedulerCache) tryUpdateNodeShardStatus(nodeShardName string) {
	sc.shardUpdateCoordinator.ShardUpdateMu.Lock()

	if sc.shardUpdateCoordinator.IsSessionRunning {
		sc.shardUpdateCoordinator.ShardUpdatePending = true
		klog.V(3).Infof("Update status of nodeshard is pending because session is running")

		sc.shardUpdateCoordinator.ShardUpdateCond.Wait()

		// when multiple update request are trying to acquire lock, only one request will do upatte
		if sc.shardUpdateCoordinator.ShardUpdatePending && !sc.shardUpdateCoordinator.IsSessionRunning {
			sc.UpdateNodeShardStatus(nodeShardName)
			sc.shardUpdateCoordinator.ShardUpdatePending = false
		}

		sc.shardUpdateCoordinator.ShardUpdateMu.Unlock()
		return
	}
	sc.UpdateNodeShardStatus(nodeShardName)
	sc.shardUpdateCoordinator.ShardUpdateMu.Unlock()
}

// getAvailableNodesFromShard get available nodes based on desired nodes. Nodes are still being used in other shard should not be put into available nodes
func (sc *SchedulerCache) getAvailableNodesFromShard(nodeShardInfo *api.NodeShardInfo) sets.Set[string] {
	nodes := nodeShardInfo.NodeDesired
	for shardName, nodeShard := range sc.NodeShards {
		if shardName != nodeShardInfo.Name {
			nodes = nodes.Difference(nodeShard.NodeInUse)
		}
	}
	return nodes
}
