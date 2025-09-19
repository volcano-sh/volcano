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

package networktopologyaware

import (
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
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
		t.Errorf("weight should be 2, but get %v", weight)
	}

	binpackConfig := calculateBinpackWeight(networkTopologyAware.pluginArguments)
	expectedStr := "cpu[1], memory[1]"
	if binpackConfig.String() != expectedStr {
		t.Errorf("binpack config string should be %s, but get %s", expectedStr, binpackConfig.String())
	}
}

func TestBinpackArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		"weight":                           2,
		"binpack.cpu":                      1,
		"binpack.memory":                   2,
		"binpack.resources":                "nvidia.com/gpu",
		"binpack.resources.nvidia.com/gpu": 10,
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
	binpackConfig := calculateBinpackWeight(networkTopologyAware.pluginArguments)
	if binpackConfig.BinPackingCPU != 1 {
		t.Errorf("cpu should be 1, but get %v", binpackConfig.BinPackingCPU)
	}
	if binpackConfig.BinPackingMemory != 2 {
		t.Errorf("memory should be 2, but get %v", binpackConfig.BinPackingMemory)
	}
	if len(binpackConfig.BinPackingResources) != 3 {
		t.Errorf("extend resources length should be 3, but get %v", len(binpackConfig.BinPackingResources))
	}
	if binpackConfig.BinPackingResources["nvidia.com/gpu"] != 10 {
		t.Errorf("nvidia.com/gpu should be 10, but get %v", binpackConfig.BinPackingResources["nvidia.com/gpu"])
	}

	binpackConfigStr := binpackConfig.String()
	expectedStr := "cpu[1], memory[2], nvidia.com/gpu[10]"
	if binpackConfigStr != expectedStr {
		t.Errorf("binpack config string should be %s, but get %s", expectedStr, binpackConfigStr)
	}
}

func TestNetworkTopologyAwareNodeScore_Hard(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments  framework.Arguments
		scoreNodes []string
		expected   map[string]float64
	}{
		{
			name: "first scheduling with no allocated hypernode scores all nodes equally",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n1",
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s3-n2": 100.0,
				"s4-n1": 100.0,
				"s4-n2": 100.0,
				"s5-n1": 100.0,
				"s5-n2": 100.0,
				"s6-n1": 100.0,
				"s6-n2": 100.0,
			},
		},
		{
			name: "hard mode restricts nodes outside same hypernode tree branch",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s4-n1": 100.0,
				"s4-n2": 100.0,
				"s5-n1": 0.0,
				"s5-n2": 0.0,
				"s6-n1": 0.0,
				"s6-n2": 0.0,
			},
		},
		{
			name: "prioritizes nodes within same hypernode as existing pods",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n1",
				"s3-n2",
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s3-n2": 100.0,
			},
		},
		{
			name: "scores nodes by distance from allocated hypernode in three-tier topology",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n1",
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s3-n2": 100.0,
				"s4-n1": 17.19941772880241,
				"s4-n2": 17.19941772880241,
				"s5-n1": 0.0,
				"s5-n2": 0.0,
				"s6-n1": 0.0,
				"s6-n2": 0.0,
			},
		},
		{
			name: "restricts scheduling across different hypernodes in two-tier topology",
			TestCommonStruct: createTwoTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s1-n2",
				"s2-n1",
				"s2-n2",
			},
			expected: map[string]float64{
				"s1-n2": 100.0,
				"s2-n1": 0.0,
				"s2-n2": 0.0,
			},
		},
		{
			name: "restricts scheduling across different hypernodes in single-tier topology",
			TestCommonStruct: createSingleTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s1-n1",
				"s1-n2",
				"s2-n1",
				"s2-n2",
			},
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s1-n2": 100.0,
				"s2-n1": 0.0,
				"s2-n2": 0.0,
			},
		},
		{
			name: "applies weight multiplier to node scores correctly",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 2,
			},
			scoreNodes: []string{
				"s3-n1",
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n1": 200.0,
				"s3-n2": 200.0,
				"s4-n1": 34.39883545760482,
				"s4-n2": 34.39883545760482,
				"s5-n1": 0.0,
				"s5-n2": 0.0,
				"s6-n1": 0.0,
				"s6-n2": 0.0,
			},
		},
	}
	trueValue := true
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	for i, test := range tests {
		test.Plugins = plugins
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                  PluginName,
						EnabledHyperNodeOrder: &trueValue,
						EnabledNodeOrder:      &trueValue,
						Arguments:             test.arguments,
					},
				},
			},
		}
		// create session
		ssn := test.RegisterSession(tiers, nil)
		defer test.Close()

		scoreNodes := make([]*api.NodeInfo, len(test.scoreNodes))
		for i, nodeName := range test.scoreNodes {
			scoreNodes[i] = ssn.Nodes[nodeName]
		}

		nodeScores, err := ssn.BatchNodeOrderFn(parseTask(ssn.Jobs), scoreNodes)
		if err != nil {
			t.Errorf("case%d: task %s  has err %v", i, test.Name, err)
			continue
		}
		for node, expected := range test.expected {
			if math.Abs(nodeScores[node]-expected) > eps {
				t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i+1, test.name, node, expected, nodeScores[node])
			}
		}
	}
}

func TestNetworkTopologyAwareNodeScore_Soft(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments  framework.Arguments
		scoreNodes []string
		tasks      map[string]string
		expected   map[string]float64
	}{
		{
			name: "first scheduling with no allocated hypernode scores all nodes equally",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n1",
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s3-n2": 100.0,
				"s4-n1": 100.0,
				"s4-n2": 100.0,
				"s5-n1": 100.0,
				"s5-n2": 100.0,
				"s6-n1": 100.0,
				"s6-n2": 100.0,
			},
		},
		{
			name: "hypernode which can satisfy resource requirement with less idle resources scores higher",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n1",
				"s4-n1",
				"s5-n1",
			},
			// s3 and s4 belong to same hypernode s1, s5 belongs to a different hypernode s2
			// s2 has less idle resources than s1, so s5 should get higher score
			expected: map[string]float64{
				"s3-n1": 0.0,
				"s4-n1": 0.0,
				"s5-n1": 100.0,
			},
		},
		{
			name: "nodes within same hypernode score equally",
			TestCommonStruct: createTwoTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s1-n1",
				"s1-n2",
			},
			// s1-n1 and s1-n2 belong to same hypernode s1, with same score
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s1-n2": 100.0,
			},
		},
		{
			name: "hypernode which can satisfy resource requirement with existing pods scores higher",
			TestCommonStruct: createTwoTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s1-n2",
				"s2-n1",
			},
			expected: map[string]float64{
				"s1-n2": 100.0,
				"s2-n1": 0.0,
			},
		},
		{
			name: "hypernode must have enough idle resources to schedule pending pod",
			TestCommonStruct: createTwoTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s1-n1",
				"s2-n1",
			},
			expected: map[string]float64{
				"s1-n1": 0,
				"s2-n1": 100.0,
			},
		},
		{
			name: "soft mode allows sibling hypernode placement in three-tier topology",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s4-n1": 100.0,
				"s4-n2": 100.0,
				"s5-n1": 0.0,
				"s5-n2": 0.0,
				"s6-n1": 0.0,
				"s6-n2": 0.0,
			},
		},

		{
			name: "binpacking with existing pods in same hypernode",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
					util.BuildPodGroupWithNetWorkTopologies("pg2", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p-other", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg2", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n2": 100.0,
				"s4-n1": 19.999999999999986,
				"s4-n2": 19.999999999999986,
				"s5-n1": 0.0,
				"s5-n2": 0.0,
				"s6-n1": 0.0,
				"s6-n2": 0.0,
			},
		},
		{
			name: "binpacking with existing pods with best tier",
			TestCommonStruct: createThreeTierTestStruct(
				[]*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
					util.BuildPodGroupWithNetWorkTopologies("pg2", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				[]*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p-other", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg2", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
			),
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []string{
				"s3-n2",
				"s4-n1",
				"s4-n2",
				"s5-n1",
				"s5-n2",
				"s6-n1",
				"s6-n2",
			},
			expected: map[string]float64{
				"s3-n2": 0.0,
				"s4-n1": 100.0,
				"s4-n2": 100.0,
				"s5-n1": 74.04293950677567,
				"s5-n2": 74.04293950677567,
				"s6-n1": 74.04293950677567,
				"s6-n2": 74.04293950677567,
			},
		},
	}
	trueValue := true
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	for i, test := range tests {
		test.Plugins = plugins
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                  PluginName,
						EnabledHyperNodeOrder: &trueValue,
						EnabledNodeOrder:      &trueValue,
						Arguments:             test.arguments,
					},
				},
			},
		}
		// create session
		ssn := test.RegisterSession(tiers, nil)
		defer test.Close()

		scoreNodes := make([]*api.NodeInfo, len(test.scoreNodes))
		for i, nodeName := range test.scoreNodes {
			scoreNodes[i] = ssn.Nodes[nodeName]
		}

		nodeScores, err := ssn.BatchNodeOrderFn(parseTask(ssn.Jobs), scoreNodes)
		if err != nil {
			t.Errorf("case%d: %s has err %v", i, test.name, err)
			continue
		}
		for node, expected := range test.expected {
			if math.Abs(nodeScores[node]-expected) > eps {
				t.Errorf("case%d: %s on node %s expect have score %v, but get %v", i+1, test.name, node, expected, nodeScores[node])
			}
		}
	}
}

func parseTask(jobInfoMap map[api.JobID]*api.JobInfo) *api.TaskInfo {
	var job *api.JobInfo
	for _, jobInfo := range jobInfoMap {
		job = jobInfo
	}
	if job == nil {
		return nil
	}
	jobAllocatedHyperNode := job.PodGroup.GetAnnotations()[api.JobAllocatedHyperNode]
	for _, task := range job.Tasks {
		if task.Pod.Status.Phase == v1.PodPending {
			task.JobAllocatedHyperNode = jobAllocatedHyperNode
			return task
		}
	}
	return nil
}

// buildThreeTierTopology creates a 3-tier network topology with 8 nodes
//
//		        s0
//		      /    \
//		    s1      s2
//		   /  \    /  \
//		  s3  s4  s5   s6
//		/ \   / \ / \  / \
//	  n1 n2 n1 n2 n1 n2 n1 n2
//
// each node has 2 CPU, 4Gi memory and 10 pods allocatable
func buildThreeTierTopology() (
	nodes []*v1.Node,
	hyperNodesSetByTier map[int]sets.Set[string],
	hyperNodesMap map[string]*api.HyperNodeInfo,
	hyperNodes map[string]sets.Set[string],
) {
	nodes = []*v1.Node{
		util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
	}

	hyperNodesSetByTier = map[int]sets.Set[string]{
		1: sets.New("s3", "s4", "s5", "s6"),
		2: sets.New("s1", "s2"),
		3: sets.New("s0"),
	}

	hyperNodesMap = map[string]*api.HyperNodeInfo{
		"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
			{
				Name:     "s1",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
			{
				Name:     "s2",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
		})),
		"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
			{
				Name:     "s3",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
			{
				Name:     "s4",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
		})),
		"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
			{
				Name:     "s5",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
			{
				Name:     "s6",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
		})),
		"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
			{
				Name:     "s3-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s3-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
		"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
			{
				Name:     "s4-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s4-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
		"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
			{
				Name:     "s5-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s5-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
		"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
			{
				Name:     "s6-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s6-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
	}

	hyperNodes = map[string]sets.Set[string]{
		"s0": sets.New("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
		"s1": sets.New("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
		"s2": sets.New("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
		"s3": sets.New("s3-n1", "s3-n2"),
		"s4": sets.New("s4-n1", "s4-n2"),
		"s5": sets.New("s5-n1", "s5-n2"),
		"s6": sets.New("s6-n1", "s6-n2"),
	}

	return nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes
}

// buildTwoTierTopology creates a 2-tier network topology with 4 nodes
//
//		    s0
//		   /  \
//		  s1    s2
//		/ \    / \
//	  n1  n2 n1  n2
//
// each node has 2 CPU, 4Gi memory and 10 pods allocatable
func buildTwoTierTopology() (
	nodes []*v1.Node,
	hyperNodesSetByTier map[int]sets.Set[string],
	hyperNodesMap map[string]*api.HyperNodeInfo,
	hyperNodes map[string]sets.Set[string],
) {
	nodes = []*v1.Node{
		util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
	}

	hyperNodesSetByTier = map[int]sets.Set[string]{
		1: sets.New("s1", "s2"),
		2: sets.New("s0"),
	}

	hyperNodesMap = map[string]*api.HyperNodeInfo{
		"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 2, []api.MemberConfig{
			{
				Name:     "s1",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
			{
				Name:     "s2",
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			},
		})),
		"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
			{
				Name:     "s1-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s1-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
		"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
			{
				Name:     "s2-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s2-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
	}

	hyperNodes = map[string]sets.Set[string]{
		"s0": sets.New("s1-n1", "s1-n2", "s2-n1", "s2-n2"),
		"s1": sets.New("s1-n1", "s1-n2"),
		"s2": sets.New("s2-n1", "s2-n2"),
	}

	return nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes
}

// buildSingleTierTopology creates a 1-tier network topology with 4 nodes
//
//		  s1    s2
//		/ \    / \
//	  n1  n2 n1  n2
//
// each node has 2 CPU, 4Gi memory and 10 pods allocatable
func buildSingleTierTopology() (
	nodes []*v1.Node,
	hyperNodesSetByTier map[int]sets.Set[string],
	hyperNodesMap map[string]*api.HyperNodeInfo,
	hyperNodes map[string]sets.Set[string],
) {
	nodes = []*v1.Node{
		util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
	}

	hyperNodesSetByTier = map[int]sets.Set[string]{
		1: sets.New("s1", "s2"),
	}

	hyperNodesMap = map[string]*api.HyperNodeInfo{
		"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
			{
				Name:     "s1-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s1-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
		"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
			{
				Name:     "s2-n1",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
			{
				Name:     "s2-n2",
				Type:     topologyv1alpha1.MemberTypeNode,
				Selector: "exact",
			},
		})),
	}

	hyperNodes = map[string]sets.Set[string]{
		"s1": sets.New("s1-n1", "s1-n2"),
		"s2": sets.New("s2-n1", "s2-n2"),
	}

	return nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes
}

// buildTestCommonStruct creates a TestCommonStruct with the specified topology
func buildTestCommonStruct(
	nodes []*v1.Node,
	hyperNodesSetByTier map[int]sets.Set[string],
	hyperNodesMap map[string]*api.HyperNodeInfo,
	hyperNodes map[string]sets.Set[string],
) uthelper.TestCommonStruct {
	return uthelper.TestCommonStruct{
		Plugins:             map[string]framework.PluginBuilder{PluginName: New},
		Nodes:               nodes,
		HyperNodesSetByTier: hyperNodesSetByTier,
		HyperNodesMap:       hyperNodesMap,
		HyperNodes:          hyperNodes,
		Queues: []*schedulingv1.Queue{
			util.BuildQueue("q1", 1, nil),
		},
	}
}

// createThreeTierTestStruct creates a TestCommonStruct with 3-tier topology and custom pods/podgroups
func createThreeTierTestStruct(podGroups []*schedulingv1.PodGroup, pods []*v1.Pod) uthelper.TestCommonStruct {
	nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes := buildThreeTierTopology()
	testStruct := buildTestCommonStruct(nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes)
	testStruct.PodGroups = podGroups
	testStruct.Pods = pods
	return testStruct
}

// createTwoTierTestStruct creates a TestCommonStruct with 2-tier topology and custom pods/podgroups
func createTwoTierTestStruct(podGroups []*schedulingv1.PodGroup, pods []*v1.Pod) uthelper.TestCommonStruct {
	nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes := buildTwoTierTopology()
	testStruct := buildTestCommonStruct(nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes)
	testStruct.PodGroups = podGroups
	testStruct.Pods = pods
	return testStruct
}

// createSingleTierTestStruct creates a TestCommonStruct with 1-tier topology and custom pods/podgroups
func createSingleTierTestStruct(podGroups []*schedulingv1.PodGroup, pods []*v1.Pod) uthelper.TestCommonStruct {
	nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes := buildSingleTierTopology()
	testStruct := buildTestCommonStruct(nodes, hyperNodesSetByTier, hyperNodesMap, hyperNodes)
	testStruct.PodGroups = podGroups
	testStruct.Pods = pods
	return testStruct
}
