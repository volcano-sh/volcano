/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Rewritten tests using TestCommonStruct framework with comprehensive preemption scenarios
- Added TestTopologyAwarePreempt for topology-aware preemption testing

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

package preempt

import (
	"flag"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
	klog.InitFlags(nil)
	flag.Set("v", "4")
}

func TestPreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 3, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("5", "5G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPriority("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, v1.ResourceList{}, "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTopologyAwarePreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
		predicates.PluginName:  predicates.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 3, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("5", "5G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPriority("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, v1.ResourceList{}, "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			Name: "preempt low-priority pod when high-priority pod has PodAntiAffinity conflict",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				buildPodWithPodAntiAffinity("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true", "test": "test"}, make(map[string]string), "kubernetes.io/hostname"),

				buildPodWithPodAntiAffinity("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", map[string]string{"test": "test"}, make(map[string]string), "kubernetes.io/hostname"),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), map[string]string{"kubernetes.io/hostname": "n1"}),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("2", "2G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnabledPredicate:   &trueValue,
				},
				{
					Name:               predicates.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledPredicate:   &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: true}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func buildPodWithPodAntiAffinity(name, namespace, node string, phase v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string, topologyKey string) *v1.Pod {
	pod := util.BuildPod(name, namespace, node, phase, req, groupName, labels, selector)

	pod.Spec.Affinity = &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: labels,
					},
					TopologyKey: topologyKey,
				},
			},
		},
	}

	return pod
}
