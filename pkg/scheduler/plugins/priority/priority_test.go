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

package priority

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
}

var (
	trueValue         = true
	falseValue        = false
	priority          = int32(1000)
	pluginEnableEvict = conf.PluginOption{
		Name:               PluginName,
		EnabledTaskOrder:   &trueValue,
		EnabledJobOrder:    &trueValue,
		EnabledPreemptable: &trueValue,
		EnabledReclaimable: &trueValue,
		EnabledJobStarving: &trueValue,
	}
	pluginDisableEvict = conf.PluginOption{
		Name:               PluginName,
		EnabledTaskOrder:   &trueValue,
		EnabledJobOrder:    &trueValue,
		EnabledPreemptable: &falseValue,
		EnabledReclaimable: &falseValue,
		EnabledJobStarving: &trueValue,
	}
)

func TestPreempt(t *testing.T) {
	actions := []framework.Action{allocate.New(), preempt.New()}
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	tests := []struct {
		uthelper.TestCommonStruct
		actions []framework.Action
		tiers   []conf.Tier
	}{
		{
			actions: actions,
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginEnableEvict},
				},
			},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "enable preempt low priority: enablePreemptable:true",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.MakePriorityClass().Name("low-priority").SetValue(100).Obj(),
					util.MakePriorityClass().Name("high-priority").SetValue(1000).Obj(),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("ns1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(map[string]int32{}).
						Phase(vcapisv1.PodGroupInqueue).
						PriorityClassName("low-priority").
						Obj(),
					util.MakePodGroup().
						Name("pg2").
						Namespace("ns2").
						Queue("q1").
						MinMember(1).
						MinTaskMember(map[string]int32{}).
						Phase(vcapisv1.PodGroupInqueue).
						PriorityClassName("high-priority").
						Obj(),
				},
				Pods: []*v1.Pod{ // as preemptee victims are searched by node, priority can not be guaranteed cross nodes
					util.MakePod().
						Namespace("ns1").
						Name("preemptee1").
						NodeName("node1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg1").
						Labels(map[string]string{vcapisv1.PodPreemptable: "true"}).
						NodeSelector(make(map[string]string)).
						Obj(),
					util.MakePod().
						Namespace("ns1").
						Name("preemptee2").
						NodeName("node1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg1").
						Labels(map[string]string{vcapisv1.PodPreemptable: "true"}).
						NodeSelector(make(map[string]string)).
						Priority(&priority).
						Obj(),
					util.MakePod().
						Namespace("ns2").
						Name("preemptor1").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg2").
						Labels(make(map[string]string)).
						NodeSelector(make(map[string]string)).
						Obj(),
				},
				Nodes: []*v1.Node{
					
					util.MakeNode().
						Name("node1").
						Allocatable(api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),

					util.MakeNode().
						Name("node2").
						Allocatable(api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
				Queues: []*vcapisv1.Queue{
					util.MakeQueue().Name("q1").Weight(1).State(vcapisv1.QueueStateOpen).Capability(api.BuildResourceList("6", "6G")).Obj(),
				},
				ExpectEvicted:   []string{"ns1/preemptee1"},
				ExpectEvictNum:  1,
				ExpectPipeLined: map[string][]string{"ns2/pg2": {"node1"}},
			},
		},
		{
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginDisableEvict},
				},
			},
			actions: actions,
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "disable preempt low priority: enablePreemptable:false",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.MakePriorityClass().Name("low-priority").SetValue(100).Obj(),
					util.MakePriorityClass().Name("high-priority").SetValue(1000).Obj(),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("ns1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(map[string]int32{}).
						Phase(vcapisv1.PodGroupInqueue).
						PriorityClassName("low-priority").
						Obj(),
					util.MakePodGroup().
						Name("pg2").
						Namespace("ns2").
						Queue("q1").
						MinMember(1).
						MinTaskMember(map[string]int32{}).
						Phase(vcapisv1.PodGroupInqueue).
						PriorityClassName("high-priority").
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("ns1").
						Name("preemptee1").
						NodeName("node1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg1").
						Labels(map[string]string{vcapisv1.PodPreemptable: "true"}).
						NodeSelector(make(map[string]string)).
						Obj(),
					util.MakePod().
						Namespace("ns1").
						Name("preemptee2").
						NodeName("node2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg1").
						Labels(map[string]string{vcapisv1.PodPreemptable: "false"}).
						NodeSelector(make(map[string]string)).
						Priority(&priority).
						Obj(),
					util.MakePod().
						Namespace("ns2").
						Name("preemptor1").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("3", "3G")).
						GroupName("pg2").
						Labels(make(map[string]string)).
						NodeSelector(make(map[string]string)).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("node1").
						Allocatable(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),

					util.MakeNode().
						Name("node2").
						Allocatable(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
				Queues: []*vcapisv1.Queue{
					util.MakeQueue().Name("q1").State(vcapisv1.QueueStateOpen).Weight(1).Capability(api.BuildResourceList("6", "6G")).Obj(),
				},
				ExpectEvicted:   []string{},
				ExpectEvictNum:  0,
				ExpectPipeLined: map[string][]string{},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(test.tiers, nil)
			defer test.Close()
			test.Run(test.actions)
			err := test.CheckAll(i)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
