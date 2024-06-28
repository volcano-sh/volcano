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
	"volcano.sh/volcano/pkg/scheduler/actions/reclaim"
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

func TestReclaim(t *testing.T) {
	actions := []framework.Action{allocate.New(), reclaim.New()}
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
				Name:    "enable reclaim low priority: enableReclaimable:true",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q2", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "reclaimee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPod("ns1", "reclaimee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "false"}, make(map[string]string)),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
					util.BuildQueue("q2", 1, api.BuildResourceList("6", "6G")),
				},
				ExpectEvicted:   []string{"ns1/reclaimee1"},
				ExpectEvictNum:  1,
				ExpectPipeLined: map[string][]string{"ns2/pg2": {"node1"}},
			},
		},
		{
			actions: actions,
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginDisableEvict},
				},
			},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "disable reclaim low priority: enableReclaimable:false",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q2", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "reclaimee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPod("ns1", "reclaimee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "false"}, make(map[string]string)),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
					util.BuildQueue("q2", 1, api.BuildResourceList("6", "6G")),
				},
				ExpectEvicted:   []string{},
				ExpectEvictNum:  0,
				ExpectPipeLined: map[string][]string{},
			},
		},
		{ // test allocate task order and job order
			actions: actions,
			tiers: []conf.Tier{
				{
					Plugins: []conf.PluginOption{pluginEnableEvict},
				},
			},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "high priority job allocate first, and higher priority task allocate first in same job",
				Plugins: plugins,
				PriClass: []*schedulingv1.PriorityClass{
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "pod1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg1", map[string]string{}, make(map[string]string)),
					util.BuildPodWithPriority("ns1", "pod2", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg1", map[string]string{}, make(map[string]string), &priority),
					util.BuildPodWithPriority("ns2", "pod3", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string), &priority),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
				},
				ExpectBindsNum: 1, ExpectEvictNum: 0,
				ExpectBindMap: map[string]string{"ns1/pod2": "node1"},
				ExpectEvicted: []string{}, ExpectPipeLined: map[string][]string{},
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
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPodWithPriority("ns1", "preemptee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string), &priority),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
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
					util.BuildPriorityClass("low-priority", 100),
					util.BuildPriorityClass("high-priority", 1000),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "ns2", "q1", 1, map[string]int32{}, vcapisv1.PodGroupInqueue, "high-priority"),
				},
				Pods: []*v1.Pod{
					util.BuildPod("ns1", "preemptee1", "node1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "true"}, make(map[string]string)),
					util.BuildPod("ns1", "preemptee2", "node2", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{vcapisv1.PodPreemptable: "false"}, make(map[string]string)),
					util.BuildPod("ns2", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("node2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
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
