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
		expected   map[string][]*api.NodeInfo
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
			expected: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
					{Name: "node2"},
				},
				"hyperNode2": {
					{Name: "node3"},
				},
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
			expected: map[string][]*api.NodeInfo{
				"hyperNode1": {
					{Name: "node1"},
				},
				"hyperNode2": {
					{Name: "node3"},
				},
			},
		},
		{
			name:       "Empty hyperNodes",
			hyperNodes: map[string]sets.Set[string]{},
			allNodes: map[string]*api.NodeInfo{
				"node1": {Name: "node1"},
				"node2": {Name: "node2"},
			},
			expected: map[string][]*api.NodeInfo{},
		},
		{
			name: "Empty allNodes",
			hyperNodes: map[string]sets.Set[string]{
				"hyperNode1": sets.New[string]("node1", "node2"),
			},
			allNodes: map[string]*api.NodeInfo{},
			expected: map[string][]*api.NodeInfo{
				"hyperNode1": {},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetHyperNodeList(tc.hyperNodes, tc.allNodes)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFindLCAHyperNode(t *testing.T) {
	testCases := []struct {
		name          string
		hyperNodeName string
		JobHyperNode  string
		hyperNodeTree []map[string][]string
		expectedNode  string
		expectedLevel int
	}{
		{
			name:          "Job hyperNode is empty",
			hyperNodeName: "hyperNode3",
			JobHyperNode:  "",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "hyperNode3",
			expectedLevel: 1,
		},
		{
			name:          "Job hyperNode equals input hyperNodeName",
			hyperNodeName: "hyperNode3",
			JobHyperNode:  "hyperNode3",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "hyperNode3",
			expectedLevel: 1,
		},
		{
			name:          "Normal LCA find for non-leaf node",
			hyperNodeName: "hyperNode4",
			JobHyperNode:  "hyperNode1",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "hyperNode1",
			expectedLevel: 2,
		},
		{
			name:          "Find LCA for hyperNodes in different branches",
			hyperNodeName: "hyperNode5",
			JobHyperNode:  "hyperNode1",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "hyperNode0",
			expectedLevel: 3,
		},
		{
			name:          "Find LCA for hyperNodes in different branches",
			hyperNodeName: "hyperNode1",
			JobHyperNode:  "hyperNode2",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "hyperNode0",
			expectedLevel: 3,
		},
		{
			name:          "No LCA hyperNode found for non-existent node",
			hyperNodeName: "nonExistentNode",
			JobHyperNode:  "",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{},
					"hyperNode4": []string{},
					"hyperNode5": []string{},
					"hyperNode6": []string{},
				},
			},
			expectedNode:  "",
			expectedLevel: -1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resultNode, resultLevel := FindLCAHyperNode(tc.hyperNodeName, tc.JobHyperNode, tc.hyperNodeTree)
			if resultNode != tc.expectedNode || resultLevel != tc.expectedLevel {
				t.Errorf("Test case '%s' failed. Expected node: %s, level: %d. Got node: %s, level: %d",
					tc.name, tc.expectedNode, tc.expectedLevel, resultNode, resultLevel)
			}
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
