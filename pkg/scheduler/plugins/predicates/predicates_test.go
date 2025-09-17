/*
Copyright 2021 The Volcano Authors.

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

package predicates

import (
	"testing"

	apiv1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/backfill"
	"volcano.sh/volcano/pkg/scheduler/actions/preempt"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func getWorkerAffinity() *apiv1.Affinity {
	return &apiv1.Affinity{
		PodAntiAffinity: &apiv1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []apiv1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "role",
								Operator: "In",
								Values:   []string{"worker"},
							},
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

func TestEventHandler(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:          New,
		gang.PluginName:     gang.New,
		priority.PluginName: priority.New,
	}

	// pending pods
	w1 := util.MakePod().
		Namespace("ns1").
		Name("worker-1").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("3", "3k")).
		GroupName("pg1").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(map[string]string{"selector": "worker"}).
		Obj()
	w2 := util.MakePod().
		Namespace("ns1").
		Name("worker-2").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("5", "5k")).
		GroupName("pg1").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Obj()
	w3 := util.MakePod().
		Namespace("ns1").
		Name("worker-3").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("4", "4k")).
		GroupName("pg2").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Obj()
	w1.Spec.Affinity = getWorkerAffinity()
	w2.Spec.Affinity = getWorkerAffinity()
	w3.Spec.Affinity = getWorkerAffinity()

	// nodes

	n1 := util.MakeNode().
		Name("node1").
		Allocatable(api.BuildResourceList("14", "14k", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("14", "14k", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	n2 := util.MakeNode().
		Name("node2").
		Allocatable(api.BuildResourceList("3", "3k", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("3", "3k", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	n1.Labels["kubernetes.io/hostname"] = "node1"
	n2.Labels["kubernetes.io/hostname"] = "node2"

	// priority
	p1 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Value: 1}
	p2 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p2"}, Value: 2}
	// podgroup
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q1").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		PriorityClassName(p2.Name).
		Obj()
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		PriorityClassName(p1.Name).
		Obj()
	// queue
	queue1 := util.MakeQueue().Name("q1").Weight(0).Capability(nil).Obj()

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:      "pod-deallocate",
			Plugins:   plugins,
			Pods:      []*apiv1.Pod{w1, w2, w3},
			Nodes:     []*apiv1.Node{n1, n2},
			PriClass:  []*schedulingv1.PriorityClass{p1, p2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:    []*schedulingv1beta1.Queue{queue1},
			ExpectBindMap: map[string]string{ // podKey -> node
				"ns1/worker-3": "node1",
			},
			ExpectBindsNum: 1,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
					{
						Name:                gang.PluginName,
						EnabledJobReady:     &trueValue,
						EnabledJobPipelined: &trueValue,
					},
					{
						Name:            priority.PluginName,
						EnabledJobOrder: &trueValue,
					},
				},
			},
		}
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

func TestNodeNum(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	// pending pods

	w1 := util.MakePod().
		Namespace("ns1").
		Name("worker-1").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(nil).
		GroupName("pg1").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(map[string]string{"selector": "worker"}).
		Obj()
	w2 := util.MakePod().
		Namespace("ns1").
		Name("worker-2").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(nil).
		GroupName("pg1").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Obj()
	w3 := util.MakePod().
		Namespace("ns1").
		Name("worker-3").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(nil).
		GroupName("pg2").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Obj()
	// nodes

	n1 := util.MakeNode().
		Name("node1").
		Allocatable(api.BuildResourceList("4", "4k", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).
		Capacity(api.BuildResourceList("4", "4k", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	// priority
	p1 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Value: 1}
	p2 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p2"}, Value: 2}

	// podgroup
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q1").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		PriorityClassName(p2.Name).
		Obj()
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		PriorityClassName(p1.Name).
		Obj()
	// queue
	queue1 := util.MakeQueue().Name("q1").Weight(0).Capability(nil).Obj()

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:      "pod-predicate",
			Plugins:   plugins,
			Pods:      []*apiv1.Pod{w1, w2, w3},
			Nodes:     []*apiv1.Node{n1},
			PriClass:  []*schedulingv1.PriorityClass{p1, p2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:    []*schedulingv1beta1.Queue{queue1},
			ExpectBindMap: map[string]string{ // podKey -> node
				"ns1/worker-1": "node1",
				"ns1/worker-2": "node1",
			},
			ExpectBindsNum: 2,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New(), backfill.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
				},
			},
		}
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

func TestPodAntiAffinity(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:          New,
		priority.PluginName: priority.New,
	}

	highPrio := util.MakePriorityClass().Name("high-priority").SetValue(100000).Obj()
	lowPrio := util.MakePriorityClass().Name("low-priority").SetValue(10).Obj()

	w1 := util.MakePod().
		Namespace("ns1").
		Name("worker-1").
		NodeName("n1").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("3", "3G")).
		GroupName("pg1").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Priority(&highPrio.Value).
		Obj()
	w2 := util.MakePod().
		Namespace("ns1").
		Name("worker-2").
		NodeName("n1").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("3", "3G")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Priority(&lowPrio.Value).
		Obj()
	w3 := util.MakePod().
		Namespace("ns1").
		Name("worker-3").
		NodeName("").
		PodPhase(apiv1.PodPending).
		ResourceList(api.BuildResourceList("3", "3G")).
		GroupName("pg2").
		Labels(map[string]string{"role": "worker"}).
		NodeSelector(make(map[string]string)).
		Priority(&highPrio.Value).
		Obj()
	w1.Spec.Affinity = getWorkerAffinity()
	w3.Spec.Affinity = getWorkerAffinity()

	// nodes
	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).
		Capacity(api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	n1.Labels["kubernetes.io/hostname"] = "node1"

	// podgroup
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q1").
		MinMember(0).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		PriorityClassName(lowPrio.Name).
		Obj()
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		PriorityClassName(highPrio.Name).
		Obj()
	// queue
	queue1 := util.MakeQueue().Name("q1").Weight(0).Capability(api.BuildResourceList("9", "9G")).Obj()

	// tests
	tests := []uthelper.TestCommonStruct{
		{
			Name:            "pod-anti-affinity",
			Plugins:         plugins,
			Pods:            []*apiv1.Pod{w1, w2, w3},
			Nodes:           []*apiv1.Node{n1},
			PriClass:        []*schedulingv1.PriorityClass{lowPrio, highPrio},
			PodGroups:       []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:          []*schedulingv1beta1.Queue{queue1},
			ExpectPipeLined: map[string][]string{},
			ExpectEvicted:   []string{},
			ExpectEvictNum:  0,
		},
	}

	for i, test := range tests {
		// allocate
		actions := []framework.Action{allocate.New(), preempt.New()}
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledPredicate: &trueValue,
					},
					{
						Name:               priority.PluginName,
						EnabledPreemptable: &trueValue,
						EnabledJobStarving: &trueValue,
					},
				},
			},
		}
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[1].Name(),
				Arguments: map[string]interface{}{preempt.EnableTopologyAwarePreemptionKey: true}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
