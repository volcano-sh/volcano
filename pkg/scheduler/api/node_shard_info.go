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

package api

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	nodeshardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// ShardID is UID type, serves as unique ID for each queue
type ShardID types.UID

// QueueInfo will have all details about queue
type NodeShardInfo struct {
	Name         string
	NodeDesired  sets.Set[string]
	NodeInUse    sets.Set[string]
	NodeToRemove sets.Set[string]
	NodeToAdd    sets.Set[string]
	NodeShard    *nodeshardv1alpha1.NodeShard
}

// NewQueueInfo creates new queueInfo object
func NewNodeShardInfo(shard *nodeshardv1alpha1.NodeShard) *NodeShardInfo {
	shardInfo := &NodeShardInfo{
		Name:         shard.Name,
		NodeDesired:  sets.New(shard.Spec.NodesDesired...),
		NodeInUse:    sets.New(shard.Status.NodesInUse...),
		NodeToRemove: sets.New(shard.Status.NodesToRemove...),
		NodeToAdd:    sets.New(shard.Status.NodesToAdd...),
		NodeShard:    shard,
	}

	//NodesToRemove and NodesToAdd in status may have delay, e.g. scheduler update NodesToRemove/NodesToAdd based on old NodesDesired
	//so calculate based on NodesDesired and NodesInUse
	shardInfo.NodeToRemove = shardInfo.NodeInUse.Difference(shardInfo.NodeDesired)
	shardInfo.NodeToAdd = shardInfo.NodeDesired.Difference(shardInfo.NodeInUse)
	return shardInfo
}

// Clone is used to clone queueInfo object
func (ns *NodeShardInfo) Clone() *NodeShardInfo {
	return &NodeShardInfo{
		Name:         ns.Name,
		NodeDesired:  ns.NodeDesired,
		NodeInUse:    ns.NodeInUse,
		NodeToRemove: ns.NodeToRemove,
		NodeToAdd:    ns.NodeToAdd,
	}
}
