/*
Copyright 2018 The Kubernetes Authors.

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
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	highPrio := util.MakePriorityClass("high-priority").Value(100000).Obj()
	lowPrio := util.MakePriorityClass("low-priority").Value(10).Obj()
	options.Default()

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(3).
					TaskMinMember(map[string]int32{"": 3}).Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodPending).Obj(),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					TaskMinMember(map[string]int32{"": 2}).Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).
					TaskMinMember(map[string]int32{"": 2}).Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
				util.MakePod("c1", "preemptor2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 2}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 2}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
				util.MakePod("c1", "preemptor2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 3}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee3").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("5", "5G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("6", "6Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("6", "6Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).TaskMinMember(map[string]int32{}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "3G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "3G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).TaskMinMember(map[string]int32{}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "3G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "3G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("6", "6Gi")).Obj(),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee3").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Priority(&highPrio.Value).Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("3", "3Gi")).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).TaskMinMember(map[string]int32{}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "3G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).
					Capacity(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("6", "6Gi")).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).TaskMinMember(map[string]int32{}).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).TaskMinMember(map[string]int32{"": 1}).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).
					Capacity(api.BuildResourceList("12", "12Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("6", "6Gi")).Obj(),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q1").MinMember(1).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").PreemptionPolicy(v1.PreemptNever).Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
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
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
