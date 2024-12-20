/*
Copyright 2019 The Volcano Authors.

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

package networktopologyaware

import (
	"math"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
)

const (
	eps = 1e-1
)

func TestArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		"weight": 2,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)
	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	networkTopologyAware, ok := plugin.(*networkTopologyAwarePlugin)
	if !ok {
		t.Fatalf("plugin should be %T, but not %T", networkTopologyAware, plugin)
	}
	weight := calculateWeight(networkTopologyAware.pluginArguments)
	if weight != 2 {
		t.Errorf("weight should be 0.5, but get %v", weight)
	}
}

func TestNetworkTopologyAwareHyperNodeScore(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments     framework.Arguments
		hyperNodes    map[string][]*api.NodeInfo
		hyperNodeTree []map[string][]string
		jobHyperNode  string
		expected      map[string]float64
	}{
		// test case 1：Job first scheduler when the `LCAHyperNode` of the job is empty and we are looking for the Lowest Common Ancestor (LCA) of job with the hyperNode,
		// now the LCA hyperNode is the hyperNode self, so it is expected to return 100.0 for the score of the hyperNode.
		{
			name: "Job LCAHyperNode empty for leaf node, hyperNode score should be 100.0",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode3": nil,
				"hyperNode4": nil,
				"hyperNode5": nil,
			},
			jobHyperNode: "",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"hyperNode3": 100.0,
				"hyperNode4": 100.0,
				"hyperNode5": 100.0,
			},
		},
		// test case 2：Job is not first scheduler when the `LCAHyperNode` of the job is not empty and the currentLCAHyperNode hyperNode3,
		// for the hyperNode3, it is equls to the LCAHyperNode, it is the best choice for the job, so it is expected to return 100.0 for the score.
		// for the hyperNode4, find the LCA hyperNode is the hyperNode1 of tier index 2, it is a not good choice. according to calculate to return 36.9 for the score.
		// for the hyperNode5, find the LCA hyperNode is the hyperNode0 of tier index 3, it is a worst choice. so it is expected to return 0.0 for the score.
		{
			name: "Normal LCA hyperNode to score for hyperNodes",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode3": nil,
				"hyperNode4": nil,
				"hyperNode5": nil,
			},
			jobHyperNode: "hyperNode3",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"hyperNode3": 100.0,
				"hyperNode4": 36.9,
				"hyperNode5": 0.0,
			},
		},
		{
			// test case 3：Job is not first scheduler and the plugin weight is 2.
			name: "Normal LCA hyperNode to score for hyperNodes with weight 2",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 2,
			},
			hyperNodes: map[string][]*api.NodeInfo{
				"hyperNode3": nil,
				"hyperNode4": nil,
				"hyperNode5": nil,
			},
			jobHyperNode: "hyperNode3",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"hyperNode3": 200.0,
				"hyperNode4": 73.8,
				"hyperNode5": 0.0,
			},
		},
	}

	trueValue := true
	for i, test := range tests {
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                   PluginName,
						EnabledNetworkTopology: &trueValue,
						Arguments:              test.arguments,
					},
				},
			},
		}
		ssn := test.RegisterSession(tiers, nil)
		ssn.HyperNodes = test.hyperNodes
		// mock for test
		HyperNodeTree = test.hyperNodeTree
		job := &api.JobInfo{
			Name:     "test-job",
			PodGroup: &api.PodGroup{},
		}
		job.PodGroup.SetAnnotations(map[string]string{api.TopologyAllocateLCAHyperNode: test.jobHyperNode})
		scores, err := ssn.HyperNodeOrderMapFn(job, ssn.HyperNodes)
		if err != nil {
			t.Errorf("case%d: task %s  has err %v", i, test.Name, err)
			continue
		}
		hyperNodesScore := scores[PluginName]
		for hypernode, expected := range test.expected {
			if math.Abs(hyperNodesScore[hypernode]-expected) > eps {
				t.Errorf("case%d: task %s on hypernode %s expect have score %v, but get %v", i+1, test.Name, hypernode, expected, hyperNodesScore[hypernode])
			}
		}
	}
}

func TestNetworkTopologyAwareNodeScore(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments     framework.Arguments
		nodes         []*api.NodeInfo
		hyperNodeTree []map[string][]string
		jobHyperNode  string
		expected      map[string]float64
	}{
		{
			name: "job first scheduler when the `LCAHyperNode` of the job is empty when node score is 100.0",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			nodes: []*api.NodeInfo{
				{
					Name: "node1",
				},
				{
					Name: "node3",
				},
				{
					Name: "node5",
				},
			},
			jobHyperNode: "",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"node1": 100.0,
				"node3": 100.0,
				"node5": 100.0,
			},
		},
		{
			name: "Normal LCA hyperNode to score for nodes",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			nodes: []*api.NodeInfo{
				{
					Name: "node1",
				},
				{
					Name: "node3",
				},
				{
					Name: "node5",
				},
			},
			jobHyperNode: "hyperNode3",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"node1": 100.0,
				"node3": 36.9,
				"node5": 0.0,
			},
		},
		{
			name: "Normal LCA hyperNode to score for nodes with weight 2",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: framework.Arguments{
				"weight": 2,
			},
			nodes: []*api.NodeInfo{
				{
					Name: "node1",
				},
				{
					Name: "node3",
				},
				{
					Name: "node5",
				},
			},
			jobHyperNode: "hyperNode3",
			hyperNodeTree: []map[string][]string{
				{
					"hyperNode0": []string{"hyperNode1", "hyperNode2"},
				},
				{
					"hyperNode1": []string{"hyperNode3", "hyperNode4"},
					"hyperNode2": []string{"hyperNode5", "hyperNode6"},
				},
				{
					"hyperNode3": []string{"node1", "node2"},
					"hyperNode4": []string{"node3", "node4"},
					"hyperNode5": []string{"node5", "node6"},
					"hyperNode6": []string{"node7", "node8"},
				},
			},
			expected: map[string]float64{
				"node1": 200.0,
				"node3": 73.8,
				"node5": 0.0,
			},
		},
	}

	trueValue := true
	for i, test := range tests {
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledNodeOrder: &trueValue,
						Arguments:        test.arguments,
					},
				},
			},
		}
		ssn := test.RegisterSession(tiers, nil)
		// mock for test
		HyperNodeTree = test.hyperNodeTree
		// mock job
		job := &api.JobInfo{
			UID:      "test-job",
			Name:     "test-job",
			PodGroup: &api.PodGroup{},
		}
		job.PodGroup.SetAnnotations(map[string]string{api.TopologyAllocateLCAHyperNode: test.jobHyperNode})
		ssn.Jobs = map[api.JobID]*api.JobInfo{
			job.UID: job,
		}
		// mock task
		task := &api.TaskInfo{
			Name: "test-task",
			Job:  job.UID,
		}
		scores, err := ssn.BatchNodeOrderFn(task, test.nodes)
		if err != nil {
			t.Errorf("case%d: task %s  has err %v", i, test.Name, err)
			continue
		}
		for node, expected := range test.expected {
			if math.Abs(scores[node]-expected) > eps {
				t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i+1, test.Name, node, scores[node], expected)
			}
		}
	}
}
