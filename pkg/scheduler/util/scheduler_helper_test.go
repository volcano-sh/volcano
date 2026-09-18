/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced test coverage for scheduler helper functionality

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

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestSelectBestNode(t *testing.T) {
	cases := []struct {
		NodeScores map[float64][]*api.NodeInfo
		// Expected node is one of ExpectedNodes
		ExpectedNodes []*api.NodeInfo
		ExpectedScore float64
	}{
		{
			NodeScores: map[float64][]*api.NodeInfo{
				1.0: {&api.NodeInfo{Name: "node1"}, &api.NodeInfo{Name: "node2"}},
				2.0: {&api.NodeInfo{Name: "node3"}, &api.NodeInfo{Name: "node4"}},
			},
			ExpectedNodes: []*api.NodeInfo{{Name: "node3"}, {Name: "node4"}},
			ExpectedScore: 2.0,
		},
		{
			NodeScores: map[float64][]*api.NodeInfo{
				1.0: {&api.NodeInfo{Name: "node1"}, &api.NodeInfo{Name: "node2"}},
				3.0: {&api.NodeInfo{Name: "node3"}},
				2.0: {&api.NodeInfo{Name: "node4"}, &api.NodeInfo{Name: "node5"}},
			},
			ExpectedNodes: []*api.NodeInfo{{Name: "node3"}},
			ExpectedScore: 3.0,
		},
		{
			NodeScores:    map[float64][]*api.NodeInfo{},
			ExpectedNodes: []*api.NodeInfo{nil},
		},
		{
			NodeScores: map[float64][]*api.NodeInfo{
				-5.0:  {&api.NodeInfo{Name: "node1"}, &api.NodeInfo{Name: "node2"}},
				-10.0: {&api.NodeInfo{Name: "node3"}},
				-8.0:  {&api.NodeInfo{Name: "node4"}, &api.NodeInfo{Name: "node5"}},
			},
			ExpectedNodes: []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}},
			ExpectedScore: -5.0,
		},
	}

	oneOf := func(node *api.NodeInfo, nodes []*api.NodeInfo) bool {
		for _, v := range nodes {
			if equality.Semantic.DeepEqual(node, v) {
				return true
			}
		}
		return false
	}
	for i, test := range cases {
		result, score := SelectBestNodeAndScore(test.NodeScores)
		if !oneOf(result, test.ExpectedNodes) {
			t.Errorf("Failed test case #%d, expected: %#v, got %#v", i, test.ExpectedNodes, result)
		}
		if score != test.ExpectedScore {
			t.Errorf("Failed test case #%d, expected: %#v, got %#v", i, test.ExpectedScore, score)
		}
	}
}

func TestGetMinInt(t *testing.T) {
	cases := []struct {
		vals   []int
		result int
	}{
		{
			vals:   []int{1, 2, 3},
			result: 1,
		},
		{
			vals:   []int{10, 9, 8},
			result: 8,
		},
		{
			vals:   []int{10, 0, 8},
			result: 0,
		},
		{
			vals:   []int{},
			result: 0,
		},
		{
			vals:   []int{0, -1, 1},
			result: -1,
		},
	}
	for i, test := range cases {
		result := GetMinInt(test.vals...)
		if result != test.result {
			t.Errorf("Failed test case #%d, expected: %#v, got %#v", i, test.result, result)
		}
	}
}

func TestNumFeasibleNodesToFind(t *testing.T) {
	tests := []struct {
		name                     string
		percentageOfNodesToScore int32
		numAllNodes              int32
		wantNumNodes             int32
	}{
		{
			name:         "not set percentageOfNodesToScore and nodes number not more than 50",
			numAllNodes:  10,
			wantNumNodes: 10,
		},
		{
			name:                     "set percentageOfNodesToScore and nodes number not more than 50",
			percentageOfNodesToScore: 40,
			numAllNodes:              10,
			wantNumNodes:             10,
		},
		{
			name:         "not set percentageOfNodesToScore and nodes number more than 50",
			numAllNodes:  1000,
			wantNumNodes: 420,
		},
		{
			name:                     "set percentageOfNodesToScore and nodes number more than 50",
			percentageOfNodesToScore: 40,
			numAllNodes:              1000,
			wantNumNodes:             400,
		},
		{
			name:         "not set percentageOfNodesToScore and nodes number more than 50*125",
			numAllNodes:  6000,
			wantNumNodes: 300,
		},
		{
			name:                     "set percentageOfNodesToScore and nodes number more than 50*125",
			percentageOfNodesToScore: 40,
			numAllNodes:              6000,
			wantNumNodes:             2400,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options.ServerOpts = &options.ServerOption{
				MinPercentageOfNodesToFind: 5,
				MinNodesToFind:             100,
				PercentageOfNodesToFind:    tt.percentageOfNodesToScore,
			}
			if gotNumNodes := CalculateNumOfFeasibleNodesToFind(tt.numAllNodes); gotNumNodes != tt.wantNumNodes {
				t.Errorf("Scheduler.numFeasibleNodesToFind() = %v, want %v", gotNumNodes, tt.wantNumNodes)
			}
		})
	}
}

func TestSelectBestNodes(t *testing.T) {
	tests := []struct {
		name          string
		nodeScores    map[float64][]*api.NodeInfo
		count         int
		nodesInBinder map[string]int
		expected      []*api.NodeInfo
	}{
		{
			name:       "empty nodeScores returns empty",
			nodeScores: map[float64][]*api.NodeInfo{},
			count:      3,
			expected:   []*api.NodeInfo{},
		},
		{
			name: "count is zero returns empty",
			nodeScores: map[float64][]*api.NodeInfo{
				1.0: {{Name: "node1"}},
			},
			count:    0,
			expected: []*api.NodeInfo{},
		},
		{
			name: "no downgrade: nodeCount <= count, all nodes included in score order",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3"}},
				2.0: {{Name: "node2"}},
				1.0: {{Name: "node1"}},
			},
			count: 5,
			nodesInBinder: map[string]int{
				"node1": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node3"},
				{Name: "node2"},
				{Name: "node1"},
			},
		},
		{
			name: "no downgrade: nodeCount > count, binder empty, top N by score",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3"}},
				2.0: {{Name: "node2"}},
				1.0: {{Name: "node1"}},
			},
			count:         2,
			nodesInBinder: map[string]int{},
			expected: []*api.NodeInfo{
				{Name: "node3"},
				{Name: "node2"},
			},
		},
		{
			name: "downgrade: binder node skipped, lower-score non-binder selected instead",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3"}},
				2.0: {{Name: "node2_binder"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node2_binder": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node3"},
				{Name: "node1"},
			},
		},
		{
			name: "downgrade: binder node at highest score skipped in favor of lower non-binder nodes",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3_binder"}},
				2.0: {{Name: "node2"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node3_binder": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node2"},
				{Name: "node1"},
			},
		},
		{
			name: "downgrade: insufficient non-binder nodes, binder nodes as fallback at end",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3_binder"}},
				2.0: {{Name: "node2_binder"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node3_binder": 1,
				"node2_binder": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node1"},
				{Name: "node3_binder"}, // node1 (score 1.0) + highest-score binder fallback (3.0)
			},
		},
		{
			name: "downgrade: all nodes in binder, no free nodes, all selected from binder in score order",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3"}},
				2.0: {{Name: "node2"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node3": 1,
				"node2": 1,
				"node1": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node3"},
				{Name: "node2"},
			},
		},
		{
			name: "no downgrade: binder node with count=0 is treated as free",
			nodeScores: map[float64][]*api.NodeInfo{
				3.0: {{Name: "node3"}},
				2.0: {{Name: "node2_binder"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node2_binder": 0,
			},
			expected: []*api.NodeInfo{
				{Name: "node3"},
				{Name: "node2_binder"},
			},
		},
		{
			name: "downgrade within same score tier: binder node skipped, free node at same score selected",
			nodeScores: map[float64][]*api.NodeInfo{
				2.0: {{Name: "node2_binder"}, {Name: "node2_free"}},
				1.0: {{Name: "node1"}},
			},
			count: 2,
			nodesInBinder: map[string]int{
				"node2_binder": 1,
			},
			expected: []*api.NodeInfo{
				{Name: "node2_free"},
				{Name: "node1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectBestNodes(tt.nodeScores, tt.count, tt.nodesInBinder)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d nodes, got %d", len(tt.expected), len(result))
				return
			}
			for i, node := range tt.expected {
				if result[i].Name != node.Name {
					t.Errorf("position %d: expected %s, got %s", i, node.Name, result[i].Name)
				}
			}
		})
	}
}

func TestGetHyperNodeList(t *testing.T) {
	testCases := []struct {
		name       string
		hyperNodes map[string]sets.Set[string]
		allNodes   map[string]*api.NodeInfo
		expected   map[string]sets.Set[string]
	}{
		{
			name: "Normal case",
			hyperNodes: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1", "node2"),
				"hyperNode2": sets.New[string]("node3"),
			},
			allNodes: map[string]*api.NodeInfo{
				"node1": {Name: "node1"},
				"node2": {Name: "node2"},
				"node3": {Name: "node3"},
			},
			expected: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1", "node2"),
				"hyperNode2": sets.New[string]("node3"),
			},
		},
		{
			name: "Missing nodes",
			hyperNodes: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1", "node4"),
				"hyperNode2": sets.New[string]("node3"),
			},
			allNodes: map[string]*api.NodeInfo{
				"node1": {Name: "node1"},
				"node3": {Name: "node3"},
			},
			expected: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1"),
				"hyperNode2": sets.New[string]("node3"),
			},
		},
		{
			name:       "Empty hyperNodes",
			hyperNodes: map[string]sets.Set[string]{},
			allNodes: map[string]*api.NodeInfo{
				"node1": {Name: "node1"},
				"node2": {Name: "node2"},
			},
			expected: map[string]sets.Set[string]{},
		},
		{
			name: "Empty allNodes",
			hyperNodes: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1", "node2"),
			},
			allNodes: map[string]*api.NodeInfo{},
			expected: map[string]sets.Set[string]{
				"hyperNode1": {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, nodesSet := GetRealNodesByHyperNode(tc.hyperNodes, tc.allNodes)
			assert.Equal(t, tc.expected, nodesSet)
		})
	}
}

func TestFindJobTaskNumOfHyperNode(t *testing.T) {
	testCases := []struct {
		name          string
		hyperNodeName string
		tasks         map[string]string
		hyperNodes    map[string][]*api.NodeInfo
		expectedRes   int
	}{
		{
			name:          "Normal case with matching tasks",
			hyperNodeName: "hyperNode1",
			tasks: map[string]string{
				"task1": "node1",
				"task2": "node2",
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node3"},
				},
			},
			expectedRes: 1,
		},
		{
			name:          "No matching tasks case",
			hyperNodeName: "hyperNode1",
			tasks: map[string]string{
				"task1": "node4",
				"task2": "node5",
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node3"},
				},
			},
			expectedRes: 0,
		},
		{
			name:          "Empty job tasks map case",
			hyperNodeName: "hyperNode1",
			tasks:         map[string]string{},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node3"},
				},
			},
			expectedRes: 0,
		},
		{
			name:          "Empty nodes list for hyperNode case",
			hyperNodeName: "hyperNode2",
			tasks: map[string]string{
				"task1": "node1",
				"task2": "node2",
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode2": {},
			},
			expectedRes: 0,
		},
		{
			name:          "Tasks with duplicate match in multiple hyperNodes",
			hyperNodeName: "hyperNode1",
			tasks: map[string]string{
				"task1": "node1",
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
				},
				"hyperNode2": {
					{Name: "node1"},
				},
			},
			expectedRes: 1,
		},
	}

	job := &api.JobInfo{
		Name:     "test-job",
		PodGroup: &api.PodGroup{},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			job.Tasks = make(map[api.TaskID]*api.TaskInfo)
			for name, node := range tc.tasks {
				taskInfo := &api.TaskInfo{
					UID:  api.TaskID(name),
					Name: name,
					Job:  job.UID,
				}
				taskInfo.NodeName = node
				job.Tasks[taskInfo.UID] = taskInfo
			}
			result := FindJobTaskNumOfHyperNode(tc.hyperNodeName, job.Tasks, tc.hyperNodes)
			if result != tc.expectedRes {
				t.Errorf("Test case '%s' failed. Expected result: %d, but got: %d",
					tc.name, tc.expectedRes, result)
			}
		})
	}
}

func TestFindHyperNodeForNode(t *testing.T) {
	testCases := []struct {
		name                string
		nodeName            string
		hyperNodes          map[string][]*api.NodeInfo
		hyperNodesTiers     []int
		hyperNodesSetByTier map[int]sets.Set[string]
		expectedRes         string
	}{
		{
			name:     "Normal case with matching node",
			nodeName: "node1",
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node2"},
				},
				"hyperNode2": {
					{Name: "node3"},
					{Name: "node4"},
				},
			},
			hyperNodesTiers: []int{1, 2},
			hyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("hyperNode1", "hyperNode2"),
				2: sets.New[string]("hyperNode3", "hyperNode4"),
			},
			expectedRes: "hyperNode1",
		},
		{
			name:     "No matching node case",
			nodeName: "node5",
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node2"},
				},
				"hyperNode2": {
					{Name: "node3"},
					{Name: "node4"},
				},
			},
			hyperNodesTiers: []int{1, 2},
			hyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("hyperNode1", "hyperNode2"),
				2: sets.New[string]("hyperNode3", "hyperNode4"),
			},
			expectedRes: "",
		},
		{
			name:            "Empty hyperNodes map case",
			nodeName:        "node1",
			hyperNodes:      map[string][]*api.NodeInfo{},
			hyperNodesTiers: []int{1, 2},
			hyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("hyperNode1", "hyperNode2"),
				2: sets.New[string]("hyperNode3", "hyperNode4"),
			},
			expectedRes: "",
		},
		{
			name:     "Empty hyperNodesTiers case",
			nodeName: "node1",
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node2"},
				},
				"hyperNode2": {
					{Name: "node3"},
					{Name: "node4"},
				},
			},
			hyperNodesTiers: []int{},
			hyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("hyperNode1", "hyperNode2"),
				2: sets.New[string]("hyperNode3", "hyperNode4"),
			},
			expectedRes: "",
		},
		{
			name:     "hyperNodesSetByTier does not contain the tier",
			nodeName: "node1",
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node2"},
				},
				"hyperNode2": {
					{Name: "node3"},
					{Name: "node4"},
				},
			},
			hyperNodesTiers: []int{3, 4},
			hyperNodesSetByTier: map[int]sets.Set[string]{
				1: sets.New[string]("hyperNode1", "hyperNode2"),
				2: sets.New[string]("hyperNode3", "hyperNode4"),
			},
			expectedRes: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FindHyperNodeForNode(tc.nodeName, tc.hyperNodes, tc.hyperNodesTiers, tc.hyperNodesSetByTier)
			if result != tc.expectedRes {
				t.Errorf("Test case '%s' failed. Expected result: %s, but got: %s",
					tc.name, tc.expectedRes, result)
			}
		})
	}
}

func TestSelectBestHyperNodeAndScore(t *testing.T) {
	testCases := []struct {
		name            string
		hyperNodeScores map[float64][]string
		expectedNodes   []string
		expectedScore   float64
	}{
		{
			name:            "has no nodes",
			hyperNodeScores: map[float64][]string{},
			expectedNodes:   []string{},
			expectedScore:   0.0,
		},
		{
			name: "has only one node",
			hyperNodeScores: map[float64][]string{
				1.0: {"node1"},
			},
			expectedNodes: []string{"node1"},
			expectedScore: 1.0,
		},
		{
			name: "has multiple nodes with the same score",
			hyperNodeScores: map[float64][]string{
				1.0: {"node1", "node2"},
			},
			expectedNodes: []string{"node1", "node2"},
			expectedScore: 1.0,
		},
		{
			name: "has multiple nodes with varying scores",
			hyperNodeScores: map[float64][]string{
				1.0: {"node1"},
				2.0: {"node2"},
				3.0: {"node3"},
			},
			expectedNodes: []string{"node3"},
			expectedScore: 3.0,
		},
		{
			name: "has multiple nodes with different scores, but the highest score is shared among several nodes",
			hyperNodeScores: map[float64][]string{
				1.0: {"node1", "node2"},
				1.5: {"node3", "node4"},
				2.0: {"node5", "node6", "node7"},
			},
			expectedNodes: []string{"node5", "node6", "node7"},
			expectedScore: 2.0,
		},
	}
	oneOf := func(node string, nodes []string) bool {
		if len(nodes) == 0 && len(node) == 0 {
			return true
		}
		for _, v := range nodes {
			if node == v {
				return true
			}
		}
		return false
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, score := SelectBestHyperNodeAndScore(tc.hyperNodeScores)
			if !oneOf(result, tc.expectedNodes) {
				t.Errorf("Test case '%s' failed. Expected node: %#v, but got: %#v",
					tc.name, tc.expectedNodes, result)
			}
			if score != tc.expectedScore {
				t.Errorf("Test case '%s' failed. Expected score: %#v, but got: %#v",
					tc.name, tc.expectedScore, score)
			}
		})
	}
}

func TestPrioritizeNodes(t *testing.T) {
	task := &api.TaskInfo{Name: "task1", Namespace: "default"}
	node1 := &api.NodeInfo{Name: "node1"}
	node2 := &api.NodeInfo{Name: "node2"}
	node3 := &api.NodeInfo{Name: "node3"}

	noopBatch := func(*api.TaskInfo, []*api.NodeInfo) (map[string]float64, error) {
		return nil, nil
	}
	noopMap := func(*api.TaskInfo, *api.NodeInfo) (map[string]float64, float64, error) {
		return nil, 0, nil
	}
	noopReduce := func(*api.TaskInfo, map[string]fwk.NodeScoreList) (map[string]float64, error) {
		return nil, nil
	}

	testCases := []struct {
		name     string
		nodes    []*api.NodeInfo
		batchFn  api.BatchNodeOrderFn
		mapFn    api.NodeOrderMapFn
		reduceFn api.NodeOrderReduceFn
		expected map[string]float64
	}{
		{
			name:     "empty node list",
			batchFn:  noopBatch,
			mapFn:    noopMap,
			reduceFn: noopReduce,
		},
		{
			name:  "scores from batch, order and reduce are summed",
			nodes: []*api.NodeInfo{node1, node2},
			batchFn: func(_ *api.TaskInfo, _ []*api.NodeInfo) (map[string]float64, error) {
				return map[string]float64{"node1": 10, "node2": 20}, nil
			},
			mapFn: func(_ *api.TaskInfo, n *api.NodeInfo) (map[string]float64, float64, error) {
				order := map[string]float64{"node1": 1, "node2": 2}
				return map[string]float64{"plugin": 5}, order[n.Name], nil
			},
			reduceFn: func(_ *api.TaskInfo, m map[string]fwk.NodeScoreList) (map[string]float64, error) {
				out := map[string]float64{}
				for _, list := range m {
					for _, ns := range list {
						out[ns.Name] += 3
					}
				}
				return out, nil
			},
			// node1: batch(10) + order(1) + reduce(3) = 14
			// node2: batch(20) + order(2) + reduce(3) = 25
			expected: map[string]float64{"node1": 14, "node2": 25},
		},
		{
			name:    "mapFn error on one node still scores the others",
			nodes:   []*api.NodeInfo{node1, node2, node3},
			batchFn: noopBatch,
			mapFn: func(_ *api.TaskInfo, n *api.NodeInfo) (map[string]float64, float64, error) {
				if n.Name == "node2" {
					return nil, 0, fmt.Errorf("map failed")
				}
				return nil, 7, nil
			},
			reduceFn: noopReduce,
			// node1: order(7) = 7
			// node2: mapFn errored, skipped during merge, defaults to 0
			// node3: order(7) = 7
			expected: map[string]float64{"node1": 7, "node2": 0, "node3": 7},
		},
		{
			name:  "batchFn error returns empty result",
			nodes: []*api.NodeInfo{node1, node2},
			batchFn: func(*api.TaskInfo, []*api.NodeInfo) (map[string]float64, error) {
				return nil, fmt.Errorf("batch failed")
			},
			mapFn:    noopMap,
			reduceFn: noopReduce,
		},
		{
			name:    "reduceFn error returns empty result",
			nodes:   []*api.NodeInfo{node1, node2},
			batchFn: noopBatch,
			mapFn:   noopMap,
			reduceFn: func(*api.TaskInfo, map[string]fwk.NodeScoreList) (map[string]float64, error) {
				return nil, fmt.Errorf("reduce failed")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := PrioritizeNodes(task, tc.nodes, tc.batchFn, tc.mapFn, tc.reduceFn)
			got := map[string]float64{}
			for score, nodes := range result {
				for _, n := range nodes {
					got[n.Name] = score
				}
			}
			if tc.expected == nil {
				if len(got) != 0 {
					t.Errorf("Test case '%s' failed. Expected empty result, but got: %v", tc.name, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("Test case '%s' failed. Expected: %v, but got: %v", tc.name, tc.expected, got)
			}
		})
	}
}

// TestPrioritizeNodesNoRace runs PrioritizeNodes over a large number of nodes
// under the Go race detector to verify that the lock-free map phase does not
// cause any data races or let any node scores get dropped or overwritten.
func TestPrioritizeNodesNoRace(t *testing.T) {
	const numNodes = 500
	task := &api.TaskInfo{Name: "task1", Namespace: "default"}
	nodes := make([]*api.NodeInfo, numNodes)
	nameToIdx := make(map[string]int, numNodes)
	for i := range nodes {
		name := fmt.Sprintf("node-%d", i)
		nodes[i] = &api.NodeInfo{Name: name}
		nameToIdx[name] = i
	}

	result := PrioritizeNodes(
		task, nodes,
		func(_ *api.TaskInfo, ns []*api.NodeInfo) (map[string]float64, error) {
			scores := make(map[string]float64, len(ns))
			for _, n := range ns {
				scores[n.Name] = float64(nameToIdx[n.Name] * 10)
			}
			return scores, nil
		},
		func(_ *api.TaskInfo, n *api.NodeInfo) (map[string]float64, float64, error) {
			return nil, float64(nameToIdx[n.Name]), nil
		},
		func(*api.TaskInfo, map[string]fwk.NodeScoreList) (map[string]float64, error) {
			return nil, nil
		},
	)

	got := map[string]float64{}
	for score, ns := range result {
		for _, n := range ns {
			got[n.Name] = score
		}
	}
	for i, n := range nodes {
		// node-i: order(i) + batch(i*10) = i*11
		expected := float64(i * 11)
		if got[n.Name] != expected {
			t.Errorf("Node %s: expected score %v, but got %v", n.Name, expected, got[n.Name])
		}
	}
}
