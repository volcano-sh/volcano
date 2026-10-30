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
	"k8s.io/utils/ptr"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestPredicateNodes(t *testing.T) {
	samplingNodes := make([]*api.NodeInfo, 1000)
	for i := range samplingNodes {
		samplingNodes[i] = &api.NodeInfo{Name: fmt.Sprintf("node-%d", i)}
	}
	samplingOpts := &options.ServerOption{MinNodesToFind: 100, MinPercentageOfNodesToFind: 5}
	tests := []struct {
		name               string
		task               *api.TaskInfo
		nodes              []*api.NodeInfo
		nodesInShard       sets.Set[string]
		shardingMode       string
		enableErrorCache   bool
		predicateFn        api.PredicateFn
		feasibleNodeTarget *int32
		serverOpts         *options.ServerOption
		expectedCount      int
		expectedNodes      []string
		expectedErr        string
		expectedErrCache   map[string]map[string]string
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
			name: "predicate success keeps enabled error cache",
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
			predicateFn: func(_ *api.TaskInfo, node *api.NodeInfo) error {
				if node.Name == "node1" {
					return fmt.Errorf("predicate failed")
				}
				return nil
			},
			expectedNodes: []string{"node2"},
			expectedErr:   "0/1 nodes are unavailable: 1 predicate failed.",
			expectedErrCache: map[string]map[string]string{
				"job1/worker": {
					"node1": "predicate failed",
				},
			},
		},
		{
			name: "predicate errors do not populate disabled cache",
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
			enableErrorCache: false,
			predicateFn: func(*api.TaskInfo, *api.NodeInfo) error {
				return fmt.Errorf("predicate failed")
			},
			expectedNodes:    []string{},
			expectedErr:      "0/2 nodes are unavailable: 2 predicate failed.",
			expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "empty task role does not populate predicate error cache",
			task: &api.TaskInfo{
				Job:       "job1",
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
			expectedNodes:    []string{},
			expectedErr:      "0/2 nodes are unavailable: 2 predicate failed.",
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
			expectedErrCache: map[string]map[string]string{},
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
			expectedErr:      "0/2 nodes are unavailable: 2 node isn't in scheduler node shard.",
			expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "fixed target preserves original scope sampling",
			task: &api.TaskInfo{Name: "task"}, nodes: samplingNodes,
			predicateFn:        func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			feasibleNodeTarget: ptr.To(int32(680)), serverOpts: samplingOpts,
			expectedCount: 680, expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "fixed target is capped by candidate count",
			task: &api.TaskInfo{Name: "task"}, nodes: samplingNodes[:1],
			predicateFn:        func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			feasibleNodeTarget: ptr.To(int32(680)), serverOpts: samplingOpts,
			expectedCount: 1, expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "zero target returns no feasible nodes",
			task: &api.TaskInfo{Name: "task"}, nodes: samplingNodes,
			predicateFn:        func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			feasibleNodeTarget: ptr.To(int32(0)), serverOpts: samplingOpts,
			expectedCount: 0, expectedErr: "0/0 nodes are unavailable", expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "negative target returns no feasible nodes",
			task: &api.TaskInfo{Name: "task"}, nodes: samplingNodes,
			predicateFn:        func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			feasibleNodeTarget: ptr.To(int32(-1)), serverOpts: samplingOpts,
			expectedCount: 0, expectedErr: "0/0 nodes are unavailable", expectedErrCache: map[string]map[string]string{},
		},
		{
			name: "default helper uses adaptive sampling",
			task: &api.TaskInfo{Name: "task"}, nodes: samplingNodes,
			predicateFn: func(*api.TaskInfo, *api.NodeInfo) error { return nil },
			serverOpts:  samplingOpts, expectedCount: 420, expectedErrCache: map[string]map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previousOpts := options.ServerOpts
			t.Cleanup(func() { options.ServerOpts = previousOpts })
			options.ServerOpts = &options.ServerOption{
				MinPercentageOfNodesToFind: 5,
				MinNodesToFind:             1,
				PercentageOfNodesToFind:    100,
				ShardingMode:               tt.shardingMode,
			}

			if tt.serverOpts != nil {
				options.ServerOpts = tt.serverOpts
			}
			ph := NewPredicateHelper()
			if tt.feasibleNodeTarget != nil {
				ph = NewPredicateHelperWithFeasibleNodeTarget(*tt.feasibleNodeTarget)
			}
			result, fitErr := ph.PredicateNodes(tt.task, tt.nodes, tt.predicateFn, tt.enableErrorCache, tt.nodesInShard)

			wantCount := tt.expectedCount
			if tt.expectedNodes != nil {
				wantCount = len(tt.expectedNodes)
			}
			if len(result) != wantCount {
				t.Fatalf("expected %d nodes, got %d", wantCount, len(result))
			}
			for i, name := range tt.expectedNodes {
				if result[i].Name != name {
					t.Fatalf("expected node %s, got %s", name, result[i].Name)
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

			if tt.expectedErr != "" && fitErr.Error() != tt.expectedErr {
				t.Fatalf("expected error %s, got %s", tt.expectedErr, fitErr.Error())
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

func TestPredicateNodesFixedTargetAcrossCalls(t *testing.T) {
	previousOpts := options.ServerOpts
	options.ServerOpts = &options.ServerOption{MinNodesToFind: 100, MinPercentageOfNodesToFind: 5}
	t.Cleanup(func() { options.ServerOpts = previousOpts })
	nodes := make([]*api.NodeInfo, 1000)
	for i := range nodes {
		nodes[i] = &api.NodeInfo{Name: fmt.Sprintf("node-%d", i)}
	}
	task := &api.TaskInfo{Name: "task"}
	predicate := func(*api.TaskInfo, *api.NodeInfo) error { return nil }
	helper := NewPredicateHelperWithFeasibleNodeTarget(680)
	// Reuse one helper: capping a call must not reduce the target for later calls.
	for _, tc := range []struct {
		name       string
		candidates int
		want       int
	}{
		{name: "initial target", candidates: 1000, want: 680},
		{name: "cap to one candidate", candidates: 1, want: 1},
		{name: "original target retained", candidates: 1000, want: 680},
		{name: "empty candidates", candidates: 0, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := helper.PredicateNodes(task, nodes[:tc.candidates], predicate, false, nil)
			if len(result) != tc.want {
				t.Fatalf("expected %d feasible nodes, got %d", tc.want, len(result))
			}
		})
	}
}
