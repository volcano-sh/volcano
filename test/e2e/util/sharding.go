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

package util

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

// SetupNodeShard creates a nodeshard with the given configuration
func SetupNodeShard(ctx *TestContext, spec *shardv1alpha1.NodeShard) error {
	nodeShard := &shardv1alpha1.NodeShard{
		ObjectMeta: metav1.ObjectMeta{
			Name: spec.Name,
		},
		Spec: spec.Spec,
	}

	_, err := ctx.Vcclient.ShardV1alpha1().NodeShards().Create(context.TODO(), nodeShard, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create nodeshard %s: %v", spec.Name, err)
	}

	return nil
}

// CleanupNodeShards deletes all nodeshard resources in the cluster
func CleanupNodeShards(ctx *TestContext) error {
	err := ctx.Vcclient.ShardV1alpha1().NodeShards().DeleteCollection(
		context.TODO(),
		metav1.DeleteOptions{},
		metav1.ListOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to delete nodeshards: %v", err)
	}
	return nil
}

// UpdateNodeShardStatus updates the status of a nodeshard
func UpdateNodeShardStatus(ctx *TestContext, nodeShard *shardv1alpha1.NodeShard) error {
	_, err := ctx.Vcclient.ShardV1alpha1().NodeShards().UpdateStatus(context.TODO(), nodeShard, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update nodeshard status %s: %v", nodeShard.Name, err)
	}
	return nil
}
