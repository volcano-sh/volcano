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
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestPredicateNodes(t *testing.T) {
	tests := []struct {
		name             string
		task             *api.TaskInfo
		nodes            []*api.NodeInfo
		nodesInShard     sets.Set[string]
		shardingMode     string
		enableErrorCache bool
		predicateFn      api.PredicateFn
		expectedNodes    []string
		expectedErr      string
		expectedErrCache map[string]map[string]string
	}{
		{
			name: "empty nodes returns empty result",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes:            []*api.NodeInfo{},
			nodesInShard:     sets.New[string](),
			shardingMode:     commonutil.NoneShardingMode,
			predicateFn:      func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			expectedNodes:    []string{},
			expectedErr:      "0/0 nodes are unavailable",
			expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "predicate errors returns empty result",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:     sets.New[string]("node1", "node2"),
			shardingMode:     commonutil.NoneShardingMode,
			enableErrorCache: true,
			predicateFn: func(*api.TaskInfo, *api.NodeInfo) error {
				return fmt.Errorf("predicate failed")
			},
			expectedNodes: []string{},
			expectedErr:   "0/2 nodes are unavailable: 2 predicate failed.",
			expectedErrCache: map[string]map[string]string{
				"job1/worker": {
					"node1": "predicate failed",
					"node2": "predicate failed",
				},
			},
		},
		{
			name: "predicate success returns node",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes: []*api.NodeInfo{
				{Name: "node1"},
			},
			nodesInShard:     sets.New[string]("node1"),
			shardingMode:     commonutil.NoneShardingMode,
			enableErrorCache: false,
			predicateFn:      func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			expectedNodes:    []string{"node1"},
			expectedErr:      "",
			expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "hard sharding rejects nodes outside shard",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes: []*api.NodeInfo{
				{Name: "node2"},
			},
			nodesInShard:     sets.New[string]("node1"),
			shardingMode:     commonutil.HardShardingMode,
			enableErrorCache: false,
			predicateFn:      func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			expectedNodes:    []string{},
			expectedErr:      "0/1 nodes are unavailable: 1 node isn't in scheduler node shard.",
			expectedErrCache: map[string]map[string]string{
				"job1/worker": {
					"node2": "node isn't in scheduler node shard",
				},
			},
		},
		{
			name: "soft sharding allows nodes outside shard",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes: []*api.NodeInfo{
				{Name: "node2"},
			},
			nodesInShard:     sets.New[string]("node1"),
			shardingMode:     commonutil.SoftShardingMode,
			enableErrorCache: false,
			predicateFn:      func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			expectedNodes:    []string{"node2"},
			expectedErr:      "",
			expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "hard sharding allows nodes inside shard",
			task: &api.TaskInfo{
				Job:       "job1",
				TaskRole:  "worker",
				Namespace: "ns",
				Name:      "task",
			},
			nodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
				{Name: "node3"},
			},
			nodesInShard:     sets.New[string]("node1"),
			shardingMode:     commonutil.HardShardingMode,
			enableErrorCache: false,
			predicateFn:      func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			expectedNodes:    []string{"node1"},
			expectedErr:      "",
			expectedErrCache: map[string]map[string]string{
				"job1/worker": {
					"node2": "node isn't in scheduler node shard",
					"node3": "node isn't in scheduler node shard",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options.ServerOpts = &options.ServerOption{
				MinPercentageOfNodesToFind: 5,
				MinNodesToFind:             1,
				PercentageOfNodesToFind:    100,
				ShardingMode:               tt.shardingMode,
			}

			ph := NewPredicateHelper()
			result, fitErr := ph.PredicateNodes(tt.task, tt.nodes, tt.predicateFn, tt.enableErrorCache, tt.nodesInShard)

			if len(result) != len(tt.expectedNodes) {
				t.Fatalf("expected %d nodes, got %d", len(tt.expectedNodes), len(result))
			}
			for i, node := range result {
				if node.Name != tt.expectedNodes[i] {
					t.Fatalf("expected node %s, got %s", tt.expectedNodes[i], node.Name)
				}
			}

			helper := ph.(*predicateHelper)
			cache := map[string]map[string]string{}
			for groupID, nodeErrs := range helper.taskPredicateErrorCache {
				cache[groupID] = map[string]string{}
				for nodeName, err := range nodeErrs {
					cache[groupID][nodeName] = err.Error()
				}
			}
			if !reflect.DeepEqual(cache, tt.expectedErrCache) {
				t.Fatalf("unexpected predicate error cache: %#v", cache)
			}

			if len(result) > 0 {
				// If at least one node is filtered, the subsequent error checks are not performed
				return
			}

			if gotErr := fitErr.Error(); gotErr != tt.expectedErr {
				t.Fatalf("expected error %s, got %s", tt.expectedErr, gotErr)
			}
		})
	}
}

func TestGetPredicatedNodeByShard(t *testing.T) {
	tests := []struct {
		name                 string
		predicateNodes       []*api.NodeInfo
		nodesInShard         sets.Set[string]
		shardingMode         string
		expectedInShard      []string
		expectedInOtherShard []string
	}{
		{
			name: "SoftShardingMode: split nodes",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
				{Name: "node3"},
			},
			nodesInShard:         sets.New[string]("node1", "node3"),
			shardingMode:         commonutil.SoftShardingMode,
			expectedInShard:      []string{"node1", "node3"},
			expectedInOtherShard: []string{"node2"},
		},
		{
			name: "SoftShardingMode: all nodes in shard",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:         sets.New[string]("node1", "node2"),
			shardingMode:         commonutil.SoftShardingMode,
			expectedInShard:      []string{"node1", "node2"},
			expectedInOtherShard: nil,
		},
		{
			name: "SoftShardingMode: no nodes in shard",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:         sets.New[string](),
			shardingMode:         commonutil.SoftShardingMode,
			expectedInShard:      nil,
			expectedInOtherShard: []string{"node1", "node2"},
		},
		{
			name: "SoftShardingMode: nil nodesInShard",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:         nil,
			shardingMode:         commonutil.SoftShardingMode,
			expectedInShard:      []string{"node1", "node2"},
			expectedInOtherShard: nil,
		},
		{
			name: "HardShardingMode: should not split",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:         sets.New[string]("node1"),
			shardingMode:         commonutil.HardShardingMode,
			expectedInShard:      []string{"node1", "node2"},
			expectedInOtherShard: nil,
		},
		{
			name: "NoneShardingMode: should not split",
			predicateNodes: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node2"},
			},
			nodesInShard:         sets.New[string]("node1"),
			shardingMode:         commonutil.NoneShardingMode,
			expectedInShard:      []string{"node1", "node2"},
			expectedInOtherShard: nil,
		},
	}

	checkNodes := func(t *testing.T, name string, result []*api.NodeInfo, expected []string) {
		if len(result) != len(expected) {
			t.Errorf("%s: expected %d nodes, got %d", name, len(expected), len(result))
			return
		}
		for i, node := range result {
			if node.Name != expected[i] {
				t.Errorf("%s: expected node %s at index %d, got %s", name, expected[i], i, node.Name)
			}
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options.ServerOpts = &options.ServerOption{
				ShardingMode: tt.shardingMode,
			}

			result := GetPredicatedNodeByShard(tt.predicateNodes, tt.nodesInShard)

			checkNodes(t, "InShard", result[0], tt.expectedInShard)
			checkNodes(t, "InOtherShard", result[1], tt.expectedInOtherShard)
		})
	}
}
