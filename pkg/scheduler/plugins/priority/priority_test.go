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
					util.MakePriorityClass("low-priority").Value(100).Obj(),
					util.MakePriorityClass("high-priority").Value(1000).Obj(),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.MakePodGroup("pg1", "ns1").Queue("q1").MinMember(1).
						PriorityClassName("low-priority").Phase(vcapisv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg2", "ns2").Queue("q1").MinMember(1).
						PriorityClassName("high-priority").Phase(vcapisv1.PodGroupInqueue).Obj(),
				},
				Pods: []*v1.Pod{ // as preemptee victims are searched by node, priority can not be guaranteed cross nodes
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPodWithPriority("ns1", "preemptee2", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string), &priority),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.MakeNode("node1").
						Allocatable(api.BuildResourceList("6", "6Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("6", "6Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Obj(),
					util.MakeNode("node2").
						Allocatable(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Obj(),
				},
				Queues: []*vcapisv1.Queue{
					util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("6", "6Gi")).Obj(),
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
					util.MakePriorityClass("low-priority").Value(100).Obj(),
					util.MakePriorityClass("high-priority").Value(1000).Obj(),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.MakePodGroup("pg1", "ns1").Queue("q1").MinMember(1).
						PriorityClassName("low-priority").Phase(vcapisv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg2", "ns2").Queue("q1").MinMember(1).
						PriorityClassName("high-priority").Phase(vcapisv1.PodGroupInqueue).Obj(),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPod("ns1", "preemptee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "false"}, make(map[string]string)),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.MakeNode("node1").
						Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Obj(),
					util.MakeNode("node2").
						Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Obj(),
				},
				Queues: []*vcapisv1.Queue{
					util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("6", "6Gi")).Obj(),
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
