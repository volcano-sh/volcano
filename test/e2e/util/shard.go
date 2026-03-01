/*
Copyright 2026 The Volcano Authors.

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

package util

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// NodeShardSpec defines the specification for creating a NodeShard in tests.
type NodeShardSpec struct {
	Name         string
	NodesDesired []string
}

// CreateNodeShard creates a NodeShard CR for testing.
func CreateNodeShard(ctx *TestContext, spec *NodeShardSpec) *shardv1alpha1.NodeShard {
	nodeShard := &shardv1alpha1.NodeShard{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name,
		},
		Spec: shardv1alpha1.NodeShardSpec{
			NodesDesired: spec.NodesDesired,
		},
	}

	nodeShard, err := ctx.Vcclient.ShardV1alpha1().NodeShards().Create(
		context.TODO(),
		nodeShard,
		metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred(), "failed to create NodeShard %s", spec.Name)

	return nodeShard
}

// GetNodeShard retrieves a NodeShard by name.
func GetNodeShard(ctx *TestContext, name string) (*shardv1alpha1.NodeShard, error) {
	return ctx.Vcclient.ShardV1alpha1().NodeShards().Get(
		context.TODO(),
		name,
		metav1.GetOptions{},
	)
}

// ListNodeShards lists all NodeShards in the cluster.
func ListNodeShards(ctx *TestContext) (*shardv1alpha1.NodeShardList, error) {
	return ctx.Vcclient.ShardV1alpha1().NodeShards().List(
		context.TODO(),
		metav1.ListOptions{},
	)
}

// DeleteNodeShard deletes a NodeShard by name.
func DeleteNodeShard(ctx *TestContext, name string) error {
	return ctx.Vcclient.ShardV1alpha1().NodeShards().Delete(
		context.TODO(),
		name,
		metav1.DeleteOptions{},
	)
}

// UpdateNodeShardSpec updates the spec of a NodeShard.
func UpdateNodeShardSpec(ctx *TestContext, name string, nodesDesired []string) (*shardv1alpha1.NodeShard, error) {
	nodeShard, err := GetNodeShard(ctx, name)
	if err != nil {
		return nil, err
	}

	nodeShard.Spec.NodesDesired = nodesDesired
	return ctx.Vcclient.ShardV1alpha1().NodeShards().Update(
		context.TODO(),
		nodeShard,
		metav1.UpdateOptions{},
	)
}

// WaitNodeShardExists waits for a NodeShard to exist.
func WaitNodeShardExists(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, OneMinute, true,
		func(c context.Context) (bool, error) {
			_, err := GetNodeShard(ctx, name)
			if err != nil {
				return false, nil
			}
			return true, nil
		})
}

// WaitNodeShardDeleted waits for a NodeShard to be deleted.
func WaitNodeShardDeleted(ctx *TestContext, name string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, OneMinute, true,
		func(c context.Context) (bool, error) {
			_, err := GetNodeShard(ctx, name)
			if err != nil {
				return true, nil
			}
			return false, nil
		})
}

// WaitNodeInShard waits for a specific node to appear in a NodeShard's NodesDesired list.
func WaitNodeInShard(ctx *TestContext, shardName, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true,
		func(c context.Context) (bool, error) {
			nodeShard, err := GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			for _, n := range nodeShard.Spec.NodesDesired {
				if n == nodeName {
					return true, nil
				}
			}
			return false, nil
		})
}

// WaitNodeInShardStatus waits for a specific node to appear in a NodeShard's NodesInUse status.
func WaitNodeInShardStatus(ctx *TestContext, shardName, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true,
		func(c context.Context) (bool, error) {
			nodeShard, err := GetNodeShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			for _, n := range nodeShard.Status.NodesInUse {
				if n == nodeName {
					return true, nil
				}
			}
			return false, nil
		})
}

// WaitNodeNotInShard waits for a specific node to be removed from a NodeShard's NodesDesired list.
func WaitNodeNotInShard(ctx *TestContext, shardName, nodeName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true,
		func(c context.Context) (bool, error) {
			nodeShard, err := GetNodeShard(ctx, shardName)
			if err != nil {
				// Return false, nil to continue polling on transient errors
				return false, nil
			}
			for _, n := range nodeShard.Spec.NodesDesired {
				if n == nodeName {
					return false, nil
				}
			}
			return true, nil
		})
}

// GetNodesInShard returns the list of nodes in a NodeShard's NodesDesired list.
func GetNodesInShard(ctx *TestContext, shardName string) ([]string, error) {
	nodeShard, err := GetNodeShard(ctx, shardName)
	if err != nil {
		return nil, err
	}
	return nodeShard.Spec.NodesDesired, nil
}

// GetNodesInUse returns the list of nodes currently in use by a NodeShard.
func GetNodesInUse(ctx *TestContext, shardName string) ([]string, error) {
	nodeShard, err := GetNodeShard(ctx, shardName)
	if err != nil {
		return nil, err
	}
	return nodeShard.Status.NodesInUse, nil
}

// WaitForNodeShardCount waits until the specified number of NodeShards exist.
func WaitForNodeShardCount(ctx *TestContext, expectedCount int) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true,
		func(c context.Context) (bool, error) {
			shards, err := ListNodeShards(ctx)
			if err != nil {
				return false, nil
			}
			return len(shards.Items) == expectedCount, nil
		})
}

// WaitForNodeCountInShard waits until the specified number of nodes exist in a shard.
func WaitForNodeCountInShard(ctx *TestContext, shardName string, expectedCount int) error {
	return wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, TwoMinute, true,
		func(c context.Context) (bool, error) {
			nodes, err := GetNodesInShard(ctx, shardName)
			if err != nil {
				return false, nil
			}
			return len(nodes) == expectedCount, nil
		})
}

// CleanupNodeShards deletes all NodeShards created during tests.
func CleanupNodeShards(ctx *TestContext) error {
	shards, err := ListNodeShards(ctx)
	if err != nil {
		return fmt.Errorf("failed to list NodeShards: %v", err)
	}

	for _, shard := range shards.Items {
		err := DeleteNodeShard(ctx, shard.Name)
		if err != nil {
			return fmt.Errorf("failed to delete NodeShard %s: %v", shard.Name, err)
		}
	}

	return nil
}
