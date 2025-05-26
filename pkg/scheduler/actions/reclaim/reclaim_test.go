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

package reclaim

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestReclaim(t *testing.T) {
	tests := []uthelper.TestCommonStruct{
		{
			Name: "Two Queue with one Queue overusing resource, should reclaim",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(1).
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
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
				util.MakeQueue("q2").Weight(1).Obj(),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"}, // let pod2 in the middle when sort tasks be preemptable and will not disturb
		},
		{
			Name: "sort reclaimees when reclaiming from overusing queue",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				priority.PluginName:    priority.New,
				proportion.PluginName:  proportion.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.MakePriorityClass("low-priority").Value(100).Obj(),
				util.MakePriorityClass("mid-priority").Value(500).Obj(),
				util.MakePriorityClass("high-priority").Value(1000).Obj(),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					PriorityClassName("mid-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(1).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg3", "c1").Queue("q3").MinMember(1).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee1-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg2").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg2").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg3").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
				util.MakeQueue("q2").Weight(1).Obj(),
				util.MakeQueue("q3").Weight(1).Obj(),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2-1"}, // low priority job's preemptable pod is evicted
		},
		{
			Name: "sort reclaimees when reclaiming from overusing queues with different queue priority",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					PriorityClassName("mid-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(1).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg3", "c1").Queue("q3").MinMember(1).
					PriorityClassName("high-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "preemptee1-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee1-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg2").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "true"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptee2-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).NodeName("n1").GroupName("pg2").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg3").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Priority(5).Obj(),
				util.MakeQueue("q2").Weight(1).Priority(10).Obj(),
				util.MakeQueue("q3").Weight(1).Priority(1).Obj(),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1-1"}, // low queue priority job's preemptable pod is evicted
		},
		{
			// case about #3642
			Name: "can not reclaim resources when task preemption policy is never",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.MakePriorityClass("low-priority").Value(100).Obj(),
				util.MakePriorityClass("high-priority").Value(1000).PreemptionPolicy(v1.PreemptNever).Obj(),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(0).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupInqueue).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(0).
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
				util.MakeQueue("q1").Weight(5).Obj(),
				util.MakeQueue("q2").Weight(10).Obj(),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			Name: "can not reclaim resources when queue is not open(proportion plugin)",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupRunning).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(1).
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
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
				util.MakeQueue("q2").Weight(1).State(schedulingv1beta1.QueueStateClosed).Obj(),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{},
		},
		{
			Name: "can not reclaim resources when queue is not open(capacity plugin)",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				capacity.PluginName:    capacity.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("q1").MinMember(1).
					PriorityClassName("low-priority").Phase(schedulingv1beta1.PodGroupRunning).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("q2").MinMember(1).
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
				).NodeName("n1").GroupName("pg1").Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "preemptor1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Nodes: []*v1.Node{
				util.MakeNode("n1").
					Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("q1").Weight(1).Obj(),
				util.MakeQueue("q2").Weight(1).State(schedulingv1beta1.QueueStateClosed).Obj(),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{},
		},
	}

	reclaim := New()
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledReclaimable: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledJobStarving: &trueValue,
				},
				{ // proportion plugin will cause deserved resource larger than preemptable pods' usage, and return less victims
					Name:               proportion.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnablePreemptive:   &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnablePreemptive:   &trueValue,
				},
				{
					Name:             priority.PluginName,
					EnabledJobOrder:  &trueValue,
					EnabledTaskOrder: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{reclaim})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
