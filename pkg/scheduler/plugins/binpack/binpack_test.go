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

package binpack

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
		"binpack.weight":                    10,
		"binpack.cpu":                       5,
		"binpack.memory":                    2,
		"binpack.resources":                 "nvidia.com/gpu, example.com/foo",
		"binpack.resources.nvidia.com/gpu":  7,
		"binpack.resources.example.com/foo": -3,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)
	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	binpack, ok := plugin.(*binpackPlugin)
	if !ok {
		t.Fatalf("plugin should be %T, but not %T", binpack, plugin)
	}

	weight := binpack.weight
	if weight.BinPackingWeight != 10 {
		t.Errorf("weight should be 10, but not %v", weight.BinPackingWeight)
	}
	if weight.BinPackingCPU != 5 {
		t.Errorf("cpu should be 5, but not %v", weight.BinPackingCPU)
	}
	if weight.BinPackingMemory != 2 {
		t.Errorf("memory should be 2, but not %v", weight.BinPackingMemory)
	}
	for name, weight := range weight.BinPackingResources {
		switch name {
		case "nvidia.com/gpu":
			if weight != 7 {
				t.Errorf("gpu should be 7, but not %v", weight)
			}
		case "example.com/foo":
			if weight != 1 {
				t.Errorf("example.com/foo should be 1, but not %v", weight)
			}
		case v1.ResourceCPU:
			if weight != 5 {
				t.Errorf("%v should be 5, but not %v", v1.ResourceCPU, weight)
			}
		case v1.ResourceMemory:
			if weight != 2 {
				t.Errorf("%v should be 2, but not %v", v1.ResourceMemory, weight)
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
	GPU := v1.ResourceName("nvidia.com/gpu")
	FOO := v1.ResourceName("example.com/foo")

	p1 := util.MakePod().
		Namespace("c1").
		Name("p1").
		NodeName("n1").
		PodPhase(v1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p2 := util.MakePod().
		Namespace("c1").
		Name("p2").
		NodeName("n3").
		PodPhase(v1.PodPending).
		ResourceList(api.BuildResourceList("1.5", "0Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p3 := util.MakePod().
		Namespace("c1").
		Name("p3").
		NodeName("").
		PodPhase(v1.PodPending).
		ResourceList(api.BuildResourceList("2", "10Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	addResource(p3.Spec.Containers[0].Resources.Requests, GPU, "2")
	p4 := util.MakePod().
		Namespace("c1").
		Name("p4").
		NodeName("").
		PodPhase(v1.PodPending).
		ResourceList(api.BuildResourceList("3", "4Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	addResource(p4.Spec.Containers[0].Resources.Requests, FOO, "3")

	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(make(map[string]string)).
		Obj()
	n2 := util.MakeNode().
		Name("n2").
		Allocatable(api.BuildResourceList("4", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("4", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(make(map[string]string)).
		Obj()
	addResource(n2.Status.Allocatable, GPU, "4")
	n3 := util.MakeNode().
		Name("n3").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(make(map[string]string)).
		Obj()
	addResource(n3.Status.Allocatable, FOO, "16")

	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("c1").
		Queue("c1").
		MinMember(0).
		MinTaskMember(nil).
		Phase("").
		Obj()
	queue1 := util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj()
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
				"binpack.weight":                    10,
				"binpack.cpu":                       2,
				"binpack.memory":                    3,
				"binpack.resources":                 "nvidia.com/gpu, example.com/foo",
				"binpack.resources.nvidia.com/gpu":  7,
				"binpack.resources.example.com/foo": 8,
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 700,
					"n2": 137.5,
					"n3": 0, // n3 has used 1.5c by pod p2, idle cpu not enough for p1
				},
				"c1/p2": {
					"n1": 0,
					"n2": 375,
					"n3": 0,
				},
				"c1/p3": {
					"n1": 0,
					"n2": 531.25,
					"n3": 0,
				},
				"c1/p4": {
					"n1": 0,
					"n2": 173.076923076,
					"n3": 0, // required 3c, but node only has 2c
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
				"binpack.weight":                   1,
				"binpack.cpu":                      1,
				"binpack.memory":                   1,
				"binpack.resources":                "nvidia.com/gpu",
				"binpack.resources.nvidia.com/gpu": 23,
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 75,
					"n2": 15.625,
					"n3": 0,
				},
				"c1/p2": {
					"n1": 0,
					"n2": 37.5,
					"n3": 0,
				},
				"c1/p3": {
					"n1": 0,
					"n2": 50.5,
					"n3": 0,
				},
				"c1/p4": {
					"n1": 0,
					"n2": 50,
					"n3": 0,
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
						t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i, taskID, node.Name, expectScore, score)
					}
				}
			}
		}
	}
}
