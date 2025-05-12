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

package capacity

import (
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/actions/reclaim"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}

func Test_capacityPlugin_OnSessionOpenWithoutHierarchy(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New, predicates.PluginName: predicates.New, gang.PluginName: gang.New}
	trueValue := true
	actions := []framework.Action{allocate.New(), reclaim.New()}

	// nodes
	n1 := util.MakeNode("n1").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	n2 := util.MakeNode("n2").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Obj()
	// resources for test case 0
	// pod
	p1 := util.BuildPod("ns1", "p1", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), map[string]string{"selector": "worker"})
	// podgroup
	pg1 := util.MakePodGroup("pg1", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg2 := util.MakePodGroup("pg2", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	// queue
	queue1 := util.MakeQueue("q1").Weight(1).Capability(api.BuildResourceList("2", "2Gi")).Obj()

	// resources for test case 1
	// pod
	p3 := util.BuildPod("ns1", "p3", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg3", make(map[string]string), make(map[string]string))
	p4 := util.BuildPod("ns1", "p4", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg4", make(map[string]string), make(map[string]string))
	// podgroup
	pg3 := util.MakePodGroup("pg3", "ns1").Queue("q2").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg4 := util.MakePodGroup("pg4", "ns1").Queue("q2").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	// queue
	queue2 := util.MakeQueue("q2").Weight(1).Capability(api.BuildResourceList("1.5", "1.5Gi")).Obj()

	// resources for test case 2
	// pod
	p5 := util.BuildPod("ns1", "p5", "n1", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg5", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p6 := util.BuildPod("ns1", "p6", "n2", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg5", make(map[string]string), make(map[string]string))
	p7 := util.BuildPod("ns1", "p7", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg6", make(map[string]string), make(map[string]string))
	// podgroup
	pg5 := util.MakePodGroup("pg5", "ns1").Queue("q3").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg6 := util.MakePodGroup("pg6", "ns1").Queue("q4").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	// queue
	queue3 := util.MakeQueue("q3").Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Obj()
	queue4 := util.MakeQueue("q4").Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Obj()

	// resources for test case3
	// nodes
	n3 := util.MakeNode("n3").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	n4 := util.MakeNode("n4").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Obj()

	// pod
	p8 := util.BuildPod("ns1", "p8", "n3", corev1.PodRunning, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg7", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p9 := util.BuildPod("ns1", "p9", "n4", corev1.PodRunning, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg7", make(map[string]string), make(map[string]string))

	p10 := util.BuildPod("ns1", "p10", "n3", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg8", make(map[string]string), make(map[string]string))
	p11 := util.BuildPod("ns1", "p11", "n4", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg8", make(map[string]string), make(map[string]string))

	p12 := util.BuildPod("ns1", "p12", "", corev1.PodPending, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg9", make(map[string]string), make(map[string]string))

	// podgroup
	pg7 := util.MakePodGroup("pg7", "ns1").Queue("q5").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg8 := util.MakePodGroup("pg8", "ns1").Queue("q6").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg9 := util.MakePodGroup("pg9", "ns1").Queue("q6").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()

	// queue
	queue5 := util.MakeQueue("q5").Weight(1).Deserved(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).Obj()
	queue6 := util.MakeQueue("q6").Weight(1).Deserved(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).Obj()

	// resource for test case 4
	// nodes
	n5 := util.MakeNode("n5").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Obj()
	n6 := util.MakeNode("n6").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Obj()
	// pod
	p13 := util.BuildPod("ns1", "p13", "n5", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg10", make(map[string]string), make(map[string]string))
	p14 := util.BuildPod("ns1", "p14", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg11", make(map[string]string), make(map[string]string))
	p15 := util.BuildPod("ns1", "p15", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg12", make(map[string]string), make(map[string]string))

	// podgroup
	pg10 := util.MakePodGroup("pg10", "ns1").Queue("q7").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg11 := util.MakePodGroup("pg11", "ns1").Queue("q8").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	pg12 := util.MakePodGroup("pg12", "ns1").Queue("q9").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()

	// queue
	queue7 := util.MakeQueue("q7").Weight(1).Priority(5).Capability(api.BuildResourceList("2", "4Gi")).Obj()
	queue8 := util.MakeQueue("q8").Weight(1).Priority(1).Capability(api.BuildResourceList("2", "4Gi")).Obj()
	queue9 := util.MakeQueue("q9").Weight(1).Priority(10).Capability(api.BuildResourceList("2", "4Gi")).Obj()

	// case5: p16 + p17 in queue10 will exceed queue's deserved, is not preemptive
	p16 := util.BuildPod("ns1", "p16", "n1", corev1.PodRunning, api.BuildResourceList("1", "3Gi"), "pg16", make(map[string]string), nil)
	p17 := util.BuildPod("ns1", "p17", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg17", make(map[string]string), nil)
	p18 := util.BuildPod("ns1", "p18", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg18", make(map[string]string), nil)
	// podgroup
	pg16 := util.MakePodGroup("pg16", "ns1").Queue("q10").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg17 := util.MakePodGroup("pg17", "ns1").Queue("q10").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	pg18 := util.MakePodGroup("pg18", "ns1").Queue("q11").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	// queue
	queue10 := util.MakeQueue("q10").Weight(1).Deserved(api.BuildResourceList("2", "2Gi")).Capability(api.BuildResourceList("4", "4Gi")).Obj()
	queue11 := util.MakeQueue("q11").Weight(1).Deserved(api.BuildResourceList("0", "0Gi")).Capability(api.BuildResourceList("2", "2Gi")).Obj()

	tests := []uthelper.TestCommonStruct{
		{
			Name:      "case0: Pod allocatable when queue has not exceed capability",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p1, p2},
			Nodes:     []*corev1.Node{n1, n2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2},
			Queues:    []*schedulingv1beta1.Queue{queue1},
			ExpectBindMap: map[string]string{
				"ns1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name:           "case1: Pod not allocatable when queue exceed queue capability",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p3, p4},
			Nodes:          []*corev1.Node{n1, n2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg3, pg4},
			Queues:         []*schedulingv1beta1.Queue{queue2},
			ExpectBindsNum: 0,
		},
		{
			Name:      "case2: Can reclaim from other queues when allocated < deserved",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p5, p6, p7},
			Nodes:     []*corev1.Node{n1, n2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg5, pg6},
			Queues:    []*schedulingv1beta1.Queue{queue3, queue4},
			ExpectPipeLined: map[string][]string{
				"ns1/pg6": {"n2"},
			},
			ExpectEvicted:  []string{"ns1/p6"},
			ExpectEvictNum: 1,
		},
		{
			Name:      "case3: CPU & Memory are overused, scalar resource is not overused, but candidate pod has not request CPU & Memory, reclaim should happen",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p8, p9, p10, p11, p12},
			Nodes:     []*corev1.Node{n3, n4},
			PodGroups: []*schedulingv1beta1.PodGroup{pg7, pg8, pg9},
			Queues:    []*schedulingv1beta1.Queue{queue5, queue6},
			ExpectPipeLined: map[string][]string{
				"ns1/pg9": {"n4"},
			},
			ExpectEvicted:  []string{"ns1/p9"},
			ExpectEvictNum: 1,
		},
		{
			Name:      "case4: Pods are assigned according to the order of Queue Priority in which PGs are placed",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p13, p14, p15},
			Nodes:     []*corev1.Node{n5, n6},
			PodGroups: []*schedulingv1beta1.PodGroup{pg10, pg11, pg12},
			Queues:    []*schedulingv1beta1.Queue{queue7, queue8, queue9},
			ExpectBindMap: map[string]string{
				"ns1/p15": "n6",
			},

			ExpectBindsNum: 1,
		},
		{
			Name:            "case5: Can not reclaim from other queues when allocated + req > deserved",
			Plugins:         plugins,
			Pods:            []*corev1.Pod{p16, p17, p18},
			Nodes:           []*corev1.Node{n1},
			PodGroups:       []*schedulingv1beta1.PodGroup{pg16, pg17, pg18},
			Queues:          []*schedulingv1beta1.Queue{queue10, queue11},
			ExpectPipeLined: map[string][]string{},
			ExpectEvicted:   []string{},
			ExpectEvictNum:  0,
		},
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnablePreemptive:   &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledJobStarving: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
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

func TestEnqueueAndAllocatable(t *testing.T) {
	// nodes
	n1 := util.MakeNode("n1").
		Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Obj()
	n2 := util.MakeNode("n2").
		Allocatable(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Obj()

	// resources
	res1c3g := api.BuildResourceList("1", "3G")
	res3c1g := api.BuildResourceList("3", "1G")
	res1c0g := api.BuildResourceList("1", "0G")
	res0c1g := api.BuildResourceList("0", "1G")
	res1c1g := api.BuildResourceList("1", "1G")
	// pod
	p1 := util.BuildPod("ns1", "pod1", "n1", corev1.PodRunning, res1c3g, "pg1", nil, nil)
	p2 := util.BuildPod("ns1", "pod2", "n2", corev1.PodRunning, res3c1g, "pg2", nil, nil)
	p3 := util.BuildPod("ns1", "pod3", "", corev1.PodPending, res1c0g, "pg3", nil, nil)
	p4 := util.BuildPod("ns1", "pod4", "", corev1.PodPending, res0c1g, "pg4", nil, nil)
	p5 := util.BuildPod("ns1", "pod5", "", corev1.PodPending, res1c1g, "pg5", nil, nil)
	p6 := util.BuildPod("ns1", "pod6", "", corev1.PodPending, res1c1g, "pg6", nil, nil)

	// podgroup
	pg1 := util.MakePodGroup("pg1", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg2 := util.MakePodGroup("pg2", "ns1").Queue("q2").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg3 := util.MakePodGroup("pg3", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupPending).Obj()
	pg4 := util.MakePodGroup("pg4", "ns1").Queue("q2").MinMember(1).Phase(schedulingv1beta1.PodGroupPending).Obj()
	pg5 := util.MakePodGroup("pg5", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupPending).Obj()
	pg6WithClosedQueue := util.MakePodGroup("pg6", "ns1").Queue("q3").MinMember(1).Phase(schedulingv1beta1.PodGroupPending).Obj()
	pg1.Spec.MinResources = &res1c3g
	pg2.Spec.MinResources = &res3c1g
	pg3.Spec.MinResources = &res1c0g
	pg4.Spec.MinResources = &res0c1g
	pg5.Spec.MinResources = &res1c1g
	pg6WithClosedQueue.Spec.MinResources = &res1c1g

	queue1 := util.MakeQueue("q1").Weight(1).Deserved(api.BuildResourceList("2", "2Gi")).Capability(api.BuildResourceList("2", "2Gi")).Obj()
	queue2 := util.MakeQueue("q2").Weight(1).Deserved(api.BuildResourceList("2", "2Gi")).Capability(api.BuildResourceList("3", "3Gi")).Obj()
	closedQueue3 := util.MakeQueue("q3").Weight(1).Capability(api.BuildResourceList("3", "3Gi")).State(schedulingv1beta1.QueueStateClosed).Obj()

	plugins := map[string]framework.PluginBuilder{PluginName: New}
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnablePreemptive:   &trueValue,
					EnabledOverused:    &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
			},
		},
	}
	tests := []uthelper.TestCommonStruct{
		{
			Name:           "case0: memory exceed derserved, job only request cpu can be enqueued and allocated",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p1, p2, p3},
			Nodes:          []*corev1.Node{n1, n2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg3},
			Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
			ExpectBindsNum: 1,
			ExpectBindMap:  map[string]string{"ns1/pod3": "n1"},
		},
		{
			Name:           "case1: cpu exceed derserved, job only request memory can be enqueued and allocated",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p1, p2, p4},
			Nodes:          []*corev1.Node{n1, n2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg4},
			Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
			ExpectBindsNum: 1,
			ExpectBindMap:  map[string]string{"ns1/pod4": "n2"},
		},
		{
			Name:           "case2: exceed capacity, can not enqueue",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p1, p2, p5},
			Nodes:          []*corev1.Node{n1, n2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg5},
			Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
			ExpectBindsNum: 0,
			ExpectBindMap:  map[string]string{},
		},
		{
			Name:           "case4: queue with non-open state, can not enqueue",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p6},
			Nodes:          []*corev1.Node{n1, n2},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg6WithClosedQueue},
			Queues:         []*schedulingv1beta1.Queue{closedQueue3},
			ExpectBindsNum: 0,
			ExpectBindMap:  map[string]string{},
		},
	}
	actions := []framework.Action{enqueue.New(), allocate.New()}

	for i, test := range tests {
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

func Test_capacityPlugin_OnSessionOpenWithHierarchy(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New, predicates.PluginName: predicates.New, gang.PluginName: gang.New}
	trueValue := true
	actions := []framework.Action{enqueue.New(), reclaim.New(), allocate.New()}

	// nodes
	n1 := util.MakeNode("n1").
		Allocatable(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "pods", Value: "11"}}...)).
		Capacity(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "pods", Value: "11"}}...)).
		Obj()
	// resources for test case 0
	// pod
	p1 := util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), map[string]string{})
	// podgroup
	pg1 := util.MakePodGroup("pg1", "ns1").Queue("q11").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	// queue
	root := util.MakeQueue("root").Weight(1).Parent("").Obj()
	queue1 := util.MakeQueue("q1").Weight(1).Parent("root").Capability(api.BuildResourceList("4", "4Gi")).Obj()
	queue2 := util.MakeQueue("q2").Weight(1).Parent("root").Capability(api.BuildResourceList("4", "4Gi")).Obj()
	queue11 := util.MakeQueue("q11").Weight(1).Parent("q1").Capability(api.BuildResourceList("1", "1Gi")).Obj()
	queue12 := util.MakeQueue("q12").Weight(1).Parent("q1").Capability(api.BuildResourceList("3", "3Gi")).Obj()

	// resources for test case 1
	// pod
	p2 := util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), map[string]string{})
	// podgroup
	pg2 := util.MakePodGroup("pg2", "ns1").Queue("q1").MinMember(1).Phase(schedulingv1beta1.PodGroupPending).Obj()

	// resources for test case 2
	// pod
	p3 := util.BuildPod("ns1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg3", make(map[string]string), map[string]string{})
	p4 := util.BuildPod("ns1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg3", make(map[string]string), map[string]string{})
	// podgroup
	pg3 := util.MakePodGroup("pg3", "ns1").Queue("q31").MinMember(2).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	// queue
	queue3 := util.MakeQueue("q3").Weight(1).Parent("root").Deserved(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).Obj()
	queue4 := util.MakeQueue("q4").Weight(1).Parent("root").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Obj()
	queue31 := util.MakeQueue("q31").Weight(1).Parent("q3").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Obj()
	queue32 := util.MakeQueue("q32").Weight(1).Parent("q3").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Obj()
	queue33 := util.MakeQueue("q33").Weight(1).Parent("q3").Deserved(api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Obj()

	// resources for test case 3
	// pod
	p5 := util.BuildPod("ns1", "p5", "n1", corev1.PodRunning, api.BuildResourceList("4", "4Gi"), "pg4", map[string]string{}, make(map[string]string))
	p6 := util.BuildPod("ns1", "p6", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg5", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p7 := util.BuildPod("ns1", "p7", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg5", make(map[string]string), make(map[string]string))
	p8 := util.BuildPod("ns1", "p8", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg6", make(map[string]string), map[string]string{})
	p9 := util.BuildPod("ns1", "p9", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg7", make(map[string]string), map[string]string{})
	p10 := util.BuildPod("ns1", "p10", "", corev1.PodPending, api.BuildResourceList("3", "3Gi"), "pg8", make(map[string]string), make(map[string]string))

	// podgroup
	pg4 := util.MakePodGroup("pg4", "ns1").Queue("q4").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg5 := util.MakePodGroup("pg5", "ns1").Queue("q31").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg6 := util.MakePodGroup("pg6", "ns1").Queue("q32").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()
	pg7 := util.MakePodGroup("pg7", "ns1").Queue("q32").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	pg8 := util.MakePodGroup("pg8", "ns1").Queue("q33").MinMember(1).Phase(schedulingv1beta1.PodGroupInqueue).Obj()

	// resources for test case 5
	// queue
	queue5 := util.MakeQueue("q5").Weight(1).Parent("root").Capability(api.BuildResourceList("", "4Gi")).Obj()
	queue51 := util.MakeQueue("q51").Weight(1).Parent("q5").Capability(api.BuildResourceList("", "2Gi")).Obj()
	// podgroup
	pg9 := util.MakePodGroup("pg9", "ns1").Queue("q51").MinMember(1).Phase(schedulingv1beta1.PodGroupRunning).Obj()
	// pod
	p11 := util.BuildPod("ns1", "p11", "", corev1.PodPending, api.BuildResourceList("1", ""), "pg9", make(map[string]string), map[string]string{})

	// resources for test case 6
	// queue
	queue6 := util.MakeQueue("q6").Weight(1).Parent("root").Capability(api.BuildResourceList("2", "4Gi")).Obj()
	// sub queue 61 and 62's capability is not specified, should be inherited from parent queue
	queue61 := util.MakeQueue("q61").Weight(1).Parent("q6").Obj()
	queue62 := util.MakeQueue("q62").Weight(1).Parent("q6").Obj()
	// podgroup
	pg10 := util.MakePodGroup("pg10", "ns1").Queue("q61").MinMember(1).
		MinResources(api.BuildResourceList("2", "4Gi")).Phase(schedulingv1beta1.PodGroupPending).Obj()
	pg11 := util.MakePodGroup("pg11", "ns1").Queue("q62").MinMember(1).
		MinResources(api.BuildResourceList("2", "4Gi")).Phase(schedulingv1beta1.PodGroupPending).Obj()

	// resources for test case 7
	// queue
	queue7 := util.MakeQueue("q7").Weight(1).Parent("root").Capability(api.BuildResourceList("6", "4Gi")).Obj()
	// the sum of sub queue 71 and 72's guarantee exceeds the capacity of queue7, but should not panic
	queue71 := util.MakeQueue("q71").Weight(1).Parent("q7").Capability(api.BuildResourceList("6", "4Gi")).Obj()
	queue72 := util.MakeQueue("q72").Weight(1).Parent("q7").Capability(api.BuildResourceList("6", "4Gi")).Obj()
	queue71.Spec.Guarantee = schedulingv1beta1.Guarantee{
		Resource: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("3Gi"),
		},
	}
	queue72.Spec.Guarantee = schedulingv1beta1.Guarantee{
		Resource: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("3Gi"),
		},
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name:      "case0: Pod allocatable when queue is leaf queue",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p1},
			Nodes:     []*corev1.Node{n1},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1},
			Queues:    []*schedulingv1beta1.Queue{root, queue1, queue2, queue11, queue12},
			ExpectBindMap: map[string]string{
				"ns1/p1": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name:           "case1: Pod not allocatable when queue is not leaf queue",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p2},
			Nodes:          []*corev1.Node{n1},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg2},
			Queues:         []*schedulingv1beta1.Queue{root, queue1, queue2, queue11, queue12},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
		{
			Name:      "case2: Pod allocatable when queue has not exceed capability",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p3, p4},
			Nodes:     []*corev1.Node{n1},
			PodGroups: []*schedulingv1beta1.PodGroup{pg3},
			Queues:    []*schedulingv1beta1.Queue{root, queue3, queue4, queue31, queue32},
			ExpectBindMap: map[string]string{
				"ns1/p3": "n1",
				"ns1/p4": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name:      "case3: Can reclaim from other queues when allocated < deserved",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p5, p6, p7, p8},
			Nodes:     []*corev1.Node{n1},
			PodGroups: []*schedulingv1beta1.PodGroup{pg4, pg5, pg6},
			Queues:    []*schedulingv1beta1.Queue{root, queue3, queue31, queue32, queue4},
			ExpectPipeLined: map[string][]string{
				"ns1/pg6": {"n1"},
			},
			ExpectEvicted:  []string{"ns1/p7"},
			ExpectEvictNum: 1,
		},
		{
			Name:           "case4: Pod is not allocatable when ancestor queue's real capability not enough",
			Plugins:        plugins,
			Pods:           []*corev1.Pod{p6, p9, p10},
			Nodes:          []*corev1.Node{n1},
			PodGroups:      []*schedulingv1beta1.PodGroup{pg5, pg7, pg8},
			Queues:         []*schedulingv1beta1.Queue{root, queue3, queue31, queue32, queue33},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
		{
			Name:      "case5: If the capability cpu or memory is not specified, the value should be inherited from parent queue",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p11},
			Nodes:     []*corev1.Node{n1},
			PodGroups: []*schedulingv1beta1.PodGroup{pg9},
			Queues:    []*schedulingv1beta1.Queue{root, queue5, queue51},
			ExpectBindMap: map[string]string{
				"ns1/p11": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name:      "case6: podgroup can't be enqueued when any ancestor queue's capability is not enough",
			Plugins:   plugins,
			Nodes:     []*corev1.Node{n1},
			PodGroups: []*schedulingv1beta1.PodGroup{pg10, pg11},
			Queues:    []*schedulingv1beta1.Queue{root, queue6, queue61, queue62},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"ns1/pg10": scheduling.PodGroupInqueue,
				"ns1/pg11": scheduling.PodGroupPending,
			},
		},
		{
			Name:    "case7: the sum of sub queue 71 and 72's guarantee exceeds the capacity of queue7, but should not panic",
			Plugins: plugins,
			Nodes:   []*corev1.Node{n1},
			Queues:  []*schedulingv1beta1.Queue{root, queue7, queue71, queue72},
		},
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnablePreemptive:   &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnabledHierarchy:   &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledJobStarving: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
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
