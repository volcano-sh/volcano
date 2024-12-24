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
	"k8s.io/apimachinery/pkg/util/sets"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"

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
