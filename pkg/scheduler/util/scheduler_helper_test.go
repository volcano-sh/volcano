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
	"sync/atomic"
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

	noopBatchFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		return map[string]float64{}, nil
	}
	noopMapFn := func(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
		return map[string]float64{}, 0.0, nil
	}
	noopReduceFn := func(task *api.TaskInfo, pluginNodeScoreMap map[string]fwk.NodeScoreList) (map[string]float64, error) {
		return map[string]float64{}, nil
	}

	tests := []struct {
		name       string
		nodes      []*api.NodeInfo
		batchFn    api.BatchNodeOrderFn
		mapFn      api.NodeOrderMapFn
		reduceFn   api.NodeOrderReduceFn
		wantScores map[string]float64 // expected total score per node name
		wantEmpty  bool               // expect empty result map
	}{
		{
			name:      "empty nodes list returns empty map",
			nodes:     []*api.NodeInfo{},
			batchFn:   noopBatchFn,
			mapFn:     noopMapFn,
			reduceFn:  noopReduceFn,
			wantEmpty: true,
		},
		{
			name:  "scores from all three functions are correctly aggregated",
			nodes: []*api.NodeInfo{node1, node2},
			batchFn: func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
				return map[string]float64{"node1": 10.0, "node2": 20.0}, nil
			},
			mapFn: func(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
				// orderScore: node1=1.0, node2=2.0
				scores := map[string]float64{"plugin-a": 5.0}
				orderScore := map[string]float64{"node1": 1.0, "node2": 2.0}
				return scores, orderScore[node.Name], nil
			},
			reduceFn: func(task *api.TaskInfo, pluginNodeScoreMap map[string]fwk.NodeScoreList) (map[string]float64, error) {
				// reduce returns 3.0 per node
				result := map[string]float64{}
				for _, scores := range pluginNodeScoreMap {
					for _, ns := range scores {
						result[ns.Name] += 3.0
					}
				}
				return result, nil
			},
			// node1: batch=10 + order=1 + reduce=3 = 14
			// node2: batch=20 + order=2 + reduce=3 = 25
			wantScores: map[string]float64{"node1": 14.0, "node2": 25.0},
		},
		{
			name:  "batchFn error causes batch scores to be discarded, map/reduce scores still applied",
			nodes: []*api.NodeInfo{node1, node2},
			batchFn: func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
				return nil, fmt.Errorf("batch error")
			},
			mapFn: func(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
				return map[string]float64{}, 5.0, nil
			},
			reduceFn: noopReduceFn,
			// batch discarded; only orderScore=5.0 per node
			wantScores: map[string]float64{"node1": 5.0, "node2": 5.0},
		},
		{
			name:    "mapFn error on one node skips that node but others are scored",
			nodes:   []*api.NodeInfo{node1, node2, node3},
			batchFn: noopBatchFn,
			mapFn: func(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
				if node.Name == "node2" {
					return nil, 0, fmt.Errorf("map error for node2")
				}
				return map[string]float64{}, 7.0, nil
			},
			reduceFn: noopReduceFn,
			// node1=7, node2 skipped (defaults to 0), node3=7
			wantScores: map[string]float64{"node1": 7.0, "node2": 0.0, "node3": 7.0},
		},
		{
			name:    "reduceFn error returns empty nodeScores",
			nodes:   []*api.NodeInfo{node1, node2},
			batchFn: noopBatchFn,
			mapFn:   noopMapFn,
			reduceFn: func(task *api.TaskInfo, pluginNodeScoreMap map[string]fwk.NodeScoreList) (map[string]float64, error) {
				return nil, fmt.Errorf("reduce error")
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PrioritizeNodes(task, tt.nodes, tt.batchFn, tt.mapFn, tt.reduceFn)
			if tt.wantEmpty {
				assert.Empty(t, result)
				return
			}
			// Build a name->score map from the result for easy assertion
			got := map[string]float64{}
			for score, nodes := range result {
				for _, n := range nodes {
					got[n.Name] = score
				}
			}
			assert.Equal(t, tt.wantScores, got)
		})
	}
}

func TestPrioritizeNodesConcurrentCorrectness(t *testing.T) {
	task := &api.TaskInfo{Name: "task1", Namespace: "default"}

	const numNodes = 100
	nodes := make([]*api.NodeInfo, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes[i] = &api.NodeInfo{Name: fmt.Sprintf("node%d", i)}
	}

	// Each node gets a unique score from batchFn to verify no data races or overwrites
	var callCount int64
	result := PrioritizeNodes(
		task,
		nodes,
		func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
			scores := make(map[string]float64, len(nodes))
			for i, n := range nodes {
				scores[n.Name] = float64(i) * 2
			}
			return scores, nil
		},
		func(task *api.TaskInfo, node *api.NodeInfo) (map[string]float64, float64, error) {
			atomic.AddInt64(&callCount, 1)
			return map[string]float64{}, 1.0, nil
		},
		func(task *api.TaskInfo, pluginNodeScoreMap map[string]fwk.NodeScoreList) (map[string]float64, error) {
			return map[string]float64{}, nil
		},
	)

	// All nodes must appear in the result
	assert.Equal(t, int64(numNodes), callCount, "mapFn should be called once per node")
	totalNodes := 0
	for _, ns := range result {
		totalNodes += len(ns)
	}
	assert.Equal(t, numNodes, totalNodes, "all nodes must appear in result")
}
