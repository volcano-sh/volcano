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
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	nodeshardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/util"
)

type ShardUpdateCoordinator struct {
	IsSessionRunning   *atomic.Bool
	ShardUpdatePending *atomic.Bool
	SessionEndCh       chan struct{}
}

func NewShardUpdateCoordinator() *ShardUpdateCoordinator {
	return &ShardUpdateCoordinator{
		IsSessionRunning:   new(atomic.Bool),
		ShardUpdatePending: new(atomic.Bool),
		SessionEndCh:       make(chan struct{}), // unbuffered channel for session end notification
	}
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
			break
		}
	}
	if nodeShardInfo == nil {
		klog.Errorf("Sharding is enabled but no shard is defined for this scheduler!")
		return
	}
	sc.InUseNodesInShard = sc.getAvailableNodesFromShard(nodeShardInfo)
	klog.V(3).Infof("Try to update NodeShard status after NodeShard refresh")
	go sc.tryUpdateNodeShardStatus(nodeShardInfo.Name)
}

func (sc *SchedulerCache) tryUpdateNodeShardStatus(nodeShardName string) {
	coordinator := sc.shardUpdateCoordinator
	if coordinator != nil {
		if coordinator.IsSessionRunning.Load() {
			// Try to set pending flag atomically - if already pending, skip this request
			if !coordinator.ShardUpdatePending.CompareAndSwap(false, true) {
				klog.V(3).Infof("Update status of NodeShard is already pending, skip this request")
				return
			}
			klog.V(3).Infof("Update status of NodeShard is pending because session is running")

			// Wait for session to end
			<-coordinator.SessionEndCh
			klog.V(3).Infof("Update status of NodeShard is resumed")

			// Double-check session has ended and we should still update
			if !coordinator.IsSessionRunning.Load() {
				sc.UpdateNodeShardStatus(nodeShardName)
			}
			// Reset pending flag
			coordinator.ShardUpdatePending.Store(false)
			return
		}
		sc.UpdateNodeShardStatus(nodeShardName)
	}
}

// UpdateNodeShardStatus update the status of nodeshard
func (sc *SchedulerCache) UpdateNodeShardStatus(nodeShardName string) error {
	if nodeShard := sc.generateNodeShardWithStatus(nodeShardName); nodeShard != nil {
		klog.V(3).Infof("Update NodeShard %s status...", nodeShardName)
		_, err := sc.StatusUpdater.UpdateNodeShardStatus(nodeShard)
		if err != nil {
			klog.Errorf("Failed to update NodeShard %s status %v", nodeShard.Name, err)
			return err
		}
		klog.V(3).Infof("Updated NodeShard %s status", nodeShard.Name)
	}
	return nil
}

// generateNodeShardWithStatus generate nodeshard with updated status. return nil if no status change
func (sc *SchedulerCache) generateNodeShardWithStatus(nodeShardName string) *nodeshardv1alpha1.NodeShard {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	nodeShard, exist := sc.NodeShards[nodeShardName]
	if !exist {
		klog.Warningf("NodeShard %s does not exist in cache, skip status generation", nodeShardName)
		return nil
	}

	oldNodesInUse := sets.New(nodeShard.NodeShard.Status.NodesInUse...)
	oldNodesToRemove := sets.New(nodeShard.NodeShard.Status.NodesToRemove...)
	oldNodesToAdd := sets.New(nodeShard.NodeShard.Status.NodesToAdd...)
	nodesInUse := sc.InUseNodesInShard
	// Create a deep copy to avoid modifying cache objects
	nodeShardCopy := nodeShard.NodeShard.DeepCopy()
	desiredNodes := sets.New(nodeShardCopy.Spec.NodesDesired...)
	nodesToRemove := nodesInUse.Difference(desiredNodes)
	nodesToAdd := desiredNodes.Difference(nodesInUse)
	if nodesInUse.Equal(oldNodesInUse) && nodesToRemove.Equal(oldNodesToRemove) && nodesToAdd.Equal(oldNodesToAdd) {
		klog.V(3).Infof("No change for status of nodeshard %s status", nodeShard.Name)
		return nil
	}
	nodeShardCopy.Status.NodesInUse = nodesInUse.UnsortedList()
	nodeShardCopy.Status.NodesToRemove = nodesToRemove.UnsortedList()
	nodeShardCopy.Status.NodesToAdd = nodesToAdd.UnsortedList()
	return nodeShardCopy
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

func (sc *SchedulerCache) notifySessionEnd() {
	// Notify shardUpdateCoordinator that session has ended
	select {
	case sc.shardUpdateCoordinator.SessionEndCh <- struct{}{}:
	default:
		// No shardUpdateCoordinator goroutine is waiting, which is fine
	}
}

func (su *defaultStatusUpdater) UpdateNodeShardStatus(nodeshard *nodeshardv1alpha1.NodeShard) (*nodeshardv1alpha1.NodeShard, error) {
	return su.vcclient.ShardV1alpha1().NodeShards().UpdateStatus(context.Background(), nodeshard, metav1.UpdateOptions{})
}
