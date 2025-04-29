/*
Copyright 2024 The Volcano Authors.

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

package sra

import (
	"fmt"
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	eps = 1e-8
)

func TestArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		"sra.weight":                   3,
		"sra.resources":                "nvidia.com/t4, nvidia.com/a10",
		"sra.resources.nvidia.com/t4":  1,
		"sra.resources.nvidia.com/a10": 2,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)
	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	binpack, ok := plugin.(*sraPlugin)
	if !ok {
		t.Fatalf("plugin should be %T, but not %T", binpack, plugin)
	}

	weight := binpack.weight
	if weight.SraPluginWeight != 3 {
		t.Errorf("weight should be 3, but not %v", weight.SraResources)
	}
	for name, weight := range weight.SraResources {
		switch name {
		case "nvidia.com/t4":
			if weight != 1 {
				t.Errorf("gpu should be 1, but not %v", weight)
			}
		case "nvidia.com/a10":
			if weight != 2 {
				t.Errorf("nvidia.com/a10 should be 2, but not %v", weight)
			}
		default:
			t.Errorf("resource %s with weight %d should not appear", name, weight)
		}
	}
}

func addResource(resourceList v1.ResourceList, name v1.ResourceName, need string) {
	resourceList[name] = resource.MustParse(need)
}

func TestNode(t *testing.T) {
	GPUT4 := v1.ResourceName("nvidia.com/t4")
	GPUA10 := v1.ResourceName("nvidia.com/a10")

	p1 := util.BuildPod("c1", "p1", "n1", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "n3", v1.PodPending, api.BuildResourceList("1.5", "0Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p2.Spec.Containers[0].Resources.Requests, GPUT4, "2")
	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p3.Spec.Containers[0].Resources.Requests, GPUA10, "2")
	p4 := util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("3", "4Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p4.Spec.Containers[0].Resources.Requests, GPUT4, "1")
	addResource(p4.Spec.Containers[0].Resources.Requests, GPUA10, "3")

	n1 := util.BuildNode("n1", api.BuildResourceList("32", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n2.Status.Allocatable, GPUT4, "16")
	n3 := util.BuildNode("n3", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n3.Status.Allocatable, GPUA10, "16")
	n4 := util.BuildNode("n4", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n4.Status.Allocatable, GPUT4, "8")
	addResource(n4.Status.Allocatable, GPUA10, "16")

	pg1 := util.BuildPodGroup("pg1", "c1", "c1", 0, nil, "")
	queue1 := util.BuildQueue("c1", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		arguments framework.Arguments
		expected  map[string]map[string]float64
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{

				Name:      "single job",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2, p3, p4},
				Nodes:     []*v1.Node{n1, n2, n3},
			},
			arguments: framework.Arguments{
				"sra.weight":                   5,
				"sra.resources":                "nvidia.com/t4, nvidia.com/a10",
				"sra.resources.nvidia.com/t4":  2,
				"sra.resources.nvidia.com/a10": 3,
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 500,
					"n2": 300,
					"n3": 200,
					"n4": 0,
				},
				"c1/p2": {
					"n1": 0,
					"n2": 300,
					"n3": 0,
					"n4": 0,
				},
				"c1/p3": {
					"n1": 0,
					"n2": 0,
					"n3": 200,
					"n4": 0,
				},
				"c1/p4": {
					"n1": 0,
					"n2": 0,
					"n3": 0,
					"n4": 0,
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "single job",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2, p3, p4},
				Nodes:     []*v1.Node{n1, n2, n3},
			},
			arguments: framework.Arguments{
				"sra.weight":                  5,
				"sra.resources":               "nvidia.com/t4",
				"sra.resources.nvidia.com/t4": 2,
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 500,
					"n2": 0,
					"n3": 500,
					"n4": 0,
				},
				"c1/p2": {
					"n1": 0,
					"n2": 0,
					"n3": 0,
					"n4": 0,
				},
				"c1/p3": {
					"n1": 0,
					"n2": 0,
					"n3": 500,
					"n4": 0,
				},
				"c1/p4": {
					"n1": 0,
					"n2": 0,
					"n3": 0,
					"n4": 0,
				},
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
		for _, job := range ssn.Jobs {
			for _, task := range job.Tasks {
				taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
				for _, node := range ssn.Nodes {
					score, err := ssn.NodeOrderFn(task, node)
					if err != nil {
						t.Errorf("case%d: task %s on node %s has err %v", i, taskID, node.Name, err)
						continue
					}
					if expectScore := test.expected[taskID][node.Name]; math.Abs(expectScore-score) > eps {
						t.Errorf("case%d: task %s on node %s expect have score %v, but get %v, resq= %v, cap = %v", i, taskID, node.Name, expectScore, score, task.Resreq.Get("nvidia.com/a10"), node.Capacity.Get("nvidia.com/a10"))
						t.Errorf("---case%d: task %s on node %s expect have score %v, but get %v, resq= %v, cap = %v", i, taskID, node.Name, expectScore, score, task.Resreq.Get("nvidia.com/t4"), node.Capacity.Get("nvidia.com/t4"))
					}
				}
			}
		}
	}
}
