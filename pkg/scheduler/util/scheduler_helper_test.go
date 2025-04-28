/*
Copyright 2019 The Kubernetes Authors.

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
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"

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
			result := GetRealNodesListByHyperNode(tc.hyperNodes, tc.allNodes)
			nodesSet := make(map[string]sets.Set[string])
			for name, nodes := range result {
				s := sets.New[string]()
				for _, node := range nodes {
					s.Insert(node.Name)
				}
				nodesSet[name] = s
			}
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
			result := FindJobTaskNumOfHyperNode(tc.hyperNodeName, job, tc.hyperNodes)
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
