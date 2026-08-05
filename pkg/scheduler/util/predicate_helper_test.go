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
	"bytes"
	"flag"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

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

			if tt.expectedErr != "" && fitErr.Error() != tt.expectedErr {
				t.Fatalf("expected error %s, got %s", tt.expectedErr, fitErr.Error())
			}
		})
	}
}

func TestPredicateNodesLogVerbosity(t *testing.T) {
	tests := []struct {
		name               string
		verbosity          int
		predicateFails     bool
		wantSummaries      int
		wantEvaluations    int
		wantFailureDetails int
		wantFailureReason  bool
		wantDebugFields    bool
		wantCandidateNodes int
		wantFailedNodes    int
	}{
		{
			name:              "V3 logs one failure summary without node details",
			verbosity:         3,
			predicateFails:    true,
			wantSummaries:     1,
			wantFailureReason: true,
		},
		{
			name:      "V3 omits successful predicate summaries",
			verbosity: 3,
		},
		{
			name:               "V4 logs one detailed failure summary",
			verbosity:          4,
			predicateFails:     true,
			wantSummaries:      1,
			wantFailureReason:  true,
			wantDebugFields:    true,
			wantCandidateNodes: 0,
			wantFailedNodes:    2,
		},
		{
			name:               "V4 logs one successful predicate summary",
			verbosity:          4,
			wantSummaries:      1,
			wantDebugFields:    true,
			wantCandidateNodes: 2,
		},
		{
			name:               "V5 logs the predicate summary and node details",
			verbosity:          5,
			predicateFails:     true,
			wantSummaries:      1,
			wantEvaluations:    2,
			wantFailureDetails: 2,
			wantFailureReason:  true,
			wantDebugFields:    true,
			wantCandidateNodes: 0,
			wantFailedNodes:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureKlogOutput(t, tt.verbosity, func() {
				options.ServerOpts = &options.ServerOption{
					MinPercentageOfNodesToFind: 5,
					MinNodesToFind:             1,
					PercentageOfNodesToFind:    100,
					ShardingMode:               commonutil.NoneShardingMode,
				}

				task := &api.TaskInfo{
					Job:       "job1",
					TaskRole:  "worker",
					Namespace: "ns",
					Name:      "task",
				}
				nodes := []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}}
				predicateFn := func(*api.TaskInfo, *api.NodeInfo) error {
					if tt.predicateFails {
						return fmt.Errorf("predicate failed")
					}
					return nil
				}

				NewPredicateHelper().PredicateNodes(task, nodes, predicateFn, true, sets.New[string]("node1", "node2"))
			})

			assertLogCount(t, logs, "Predicate evaluation summary", tt.wantSummaries)
			assertLogCount(t, logs, "Evaluating predicate for task on node", tt.wantEvaluations)
			assertLogCount(t, logs, "Predicate failed for task on node", tt.wantFailureDetails)
			if got := strings.Contains(logs, "2 predicate failed"); got != tt.wantFailureReason {
				t.Errorf("expected failure reason presence %t, got %t in logs:\n%s", tt.wantFailureReason, got, logs)
			}
			if tt.wantDebugFields {
				fields := []string{
					"totalNodes=2",
					"evaluatedNodes=2",
					fmt.Sprintf("candidateNodes=%d", tt.wantCandidateNodes),
					fmt.Sprintf("failedNodes=%d", tt.wantFailedNodes),
					"cachedFailureNodes=0",
				}
				for _, field := range fields {
					if !strings.Contains(logs, field) {
						t.Errorf("expected predicate summary to contain %q, got logs:\n%s", field, logs)
					}
				}
			} else {
				for _, field := range []string{"totalNodes=", "evaluatedNodes=", "candidateNodes=", "failedNodes=", "cachedFailureNodes="} {
					if strings.Contains(logs, field) {
						t.Errorf("expected predicate summary to omit debug field %q, got logs:\n%s", field, logs)
					}
				}
			}
		})
	}
}

func TestPredicateNodesLogsCachedFailuresAtV5(t *testing.T) {
	logs := captureKlogOutput(t, 5, func() {
		options.ServerOpts = &options.ServerOption{
			MinPercentageOfNodesToFind: 5,
			MinNodesToFind:             1,
			PercentageOfNodesToFind:    100,
			ShardingMode:               commonutil.NoneShardingMode,
		}

		task := &api.TaskInfo{
			Job:       "job1",
			TaskRole:  "worker",
			Namespace: "ns",
			Name:      "task",
		}
		nodes := []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}}
		predicateFn := func(*api.TaskInfo, *api.NodeInfo) error {
			return fmt.Errorf("predicate failed")
		}

		ph := NewPredicateHelper()
		ph.PredicateNodes(task, nodes, predicateFn, true, sets.New[string]("node1", "node2"))
		ph.PredicateNodes(task, nodes, predicateFn, true, sets.New[string]("node1", "node2"))
	})

	assertLogCount(t, logs, "cached=false", 2)
	assertLogCount(t, logs, "cached=true", 2)
	assertLogCount(t, logs, "cachedFailureNodes=2", 1)
}

func captureKlogOutput(t *testing.T, verbosity int, run func()) string {
	t.Helper()

	state := klog.CaptureState()
	t.Cleanup(state.Restore)

	fs := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(fs)
	if err := fs.Set("v", strconv.Itoa(verbosity)); err != nil {
		t.Fatalf("failed to set klog verbosity: %v", err)
	}

	var output bytes.Buffer
	klog.SetOutput(&output)
	klog.LogToStderr(false)
	run()
	klog.Flush()
	return output.String()
}

func assertLogCount(t *testing.T, logs, message string, expected int) {
	t.Helper()
	if got := strings.Count(logs, message); got != expected {
		t.Errorf("expected %d occurrences of %q, got %d in logs:\n%s", expected, message, got, logs)
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
