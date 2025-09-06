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
	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	n2 := util.MakeNode().
		Name("n2").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	// resources for test case 0
	// pod
	p1 := util.MakePod().
		Namespace("ns1").
		Name("p1").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p2 := util.MakePod().
		Namespace("ns1").
		Name("p2").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg2").
		Labels(make(map[string]string)).
		NodeSelector(map[string]string{"selector": "worker"}).
		Obj()
	// podgroup
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// queue
	queue1 := util.MakeQueue().Name("q1").Weight(1).Deserved(api.BuildResourceList("2", "2Gi")).State(schedulingv1beta1.QueueStateOpen).Capability(nil).Obj()
	// resources for test case 1
	// pod

	p3 := util.MakePod().
		Namespace("ns1").
		Name("p3").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg3").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p4 := util.MakePod().
		Namespace("ns1").
		Name("p4").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg4").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// podgroup

	pg3 := util.MakePodGroup().
		Name("pg3").
		Namespace("ns1").
		Queue("q2").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg4 := util.MakePodGroup().
		Name("pg4").
		Namespace("ns1").
		Queue("q2").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// queue
	queue2 := util.MakeQueue().Name("q2").State(schedulingv1beta1.QueueStateOpen).Weight(1).Deserved(nil).Capability(api.BuildResourceList("1.5", "1.5Gi")).Obj()

	// resources for test case 2
	// pod
	p5 := util.MakePod().
		Namespace("ns1").
		Name("p5").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg5").
		Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).
		NodeSelector(make(map[string]string)).
		Obj()
	p6 := util.MakePod().
		Namespace("ns1").
		Name("p6").
		NodeName("n2").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg5").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p7 := util.MakePod().
		Namespace("ns1").
		Name("p7").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg6").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()

	// podgroup

	pg5 := util.MakePodGroup().
		Name("pg5").
		Namespace("ns1").
		Queue("q3").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg6 := util.MakePodGroup().
		Name("pg6").
		Namespace("ns1").
		Queue("q4").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// queue

	queue3 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q3").Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Capability(nil).Obj()
	queue4 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q4").Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Capability(nil).Obj()

	// resources for test case3
	// nodes

	n3 := util.MakeNode().
		Name("n3").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{"selector": "worker"}).
		Obj()
	n4 := util.MakeNode().
		Name("n4").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	// pod

	p8 := util.MakePod().
		Namespace("ns1").
		Name("p8").
		NodeName("n3").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).
		GroupName("pg7").
		Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).
		NodeSelector(make(map[string]string)).
		Obj()
	p9 := util.MakePod().
		Namespace("ns1").
		Name("p9").
		NodeName("n4").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).
		GroupName("pg7").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p10 := util.MakePod().
		Namespace("ns1").
		Name("p10").
		NodeName("n3").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg8").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p11 := util.MakePod().
		Namespace("ns1").
		Name("p11").
		NodeName("n4").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg8").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p12 := util.MakePod().
		Namespace("ns1").
		Name("p12").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).
		GroupName("pg9").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()

		// podgroup

		pg7 := util.MakePodGroup().
		Name("pg7").
		Namespace("ns1").
		Queue("q5").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg8 := util.MakePodGroup().
		Name("pg8").
		Namespace("ns1").
		Queue("q6").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg9 := util.MakePodGroup().
		Name("pg9").
		Namespace("ns1").
		Queue("q6").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// queue

	queue5 := util.MakeQueue().Name("q5").Weight(1).State(schedulingv1beta1.QueueStateOpen).Deserved(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).Capability(nil).Obj()
	queue6 := util.MakeQueue().Name("q6").Weight(1).State(schedulingv1beta1.QueueStateOpen).Deserved(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...)).Capability(nil).Obj()

	// resource for test case 4
	// nodes

	n5 := util.MakeNode().
		Name("n5").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(make(map[string]string)).
		Obj()
	n6 := util.MakeNode().
		Name("n6").
		Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(make(map[string]string)).
		Obj()
	// pod

	p13 := util.MakePod().
		Namespace("ns1").
		Name("p13").
		NodeName("n5").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg10").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p14 := util.MakePod().
		Namespace("ns1").
		Name("p14").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg11").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p15 := util.MakePod().
		Namespace("ns1").
		Name("p15").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "4Gi")).
		GroupName("pg12").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// podgroup

	pg10 := util.MakePodGroup().
		Name("pg10").
		Namespace("ns1").
		Queue("q7").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg11 := util.MakePodGroup().
		Name("pg11").
		Namespace("ns1").
		Queue("q8").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	pg12 := util.MakePodGroup().
		Name("pg12").
		Namespace("ns1").
		Queue("q9").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// queue

	queue7 := util.MakeQueue().Name("q7").State(schedulingv1beta1.QueueStateOpen).Priority(5).Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Capability(nil).Obj()
	queue8 := util.MakeQueue().Name("q8").State(schedulingv1beta1.QueueStateOpen).Priority(1).Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Capability(nil).Obj()
	queue9 := util.MakeQueue().Name("q9").State(schedulingv1beta1.QueueStateOpen).Priority(10).Weight(1).Deserved(api.BuildResourceList("2", "4Gi")).Capability(nil).Obj()

	// case5: p16 + p17 in queue10 will exceed queue's deserved, is not preemptive

	p16 := util.MakePod().
		Namespace("ns1").
		Name("p16").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("1", "3Gi")).
		GroupName("pg16").
		Labels(make(map[string]string)).
		NodeSelector(nil).
		Obj()
	p17 := util.MakePod().
		Namespace("ns1").
		Name("p17").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg17").
		Labels(make(map[string]string)).
		NodeSelector(nil).
		Obj()
	p18 := util.MakePod().
		Namespace("ns1").
		Name("p18").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg18").
		Labels(make(map[string]string)).
		NodeSelector(nil).
		Obj()
		// podgroup
		
		pg16 := util.MakePodGroup().
		Name("pg16").
		Namespace("ns1").
		Queue("q10").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg17 := util.MakePodGroup().
		Name("pg17").
		Namespace("ns1").
		Queue("q10").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	pg18 := util.MakePodGroup().
		Name("pg18").
		Namespace("ns1").
		Queue("q11").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	// queue

	queue10 := util.MakeQueue().Name("q10").State(schedulingv1beta1.QueueStateOpen).Weight(1).Deserved(api.BuildResourceList("4", "4Gi")).Capability(api.BuildResourceList("2", "2Gi")).Obj()
	queue11 := util.MakeQueue().Name("q11").State(schedulingv1beta1.QueueStateOpen).Weight(1).Deserved(api.BuildResourceList("2", "2Gi")).Capability(api.BuildResourceList("0", "0Gi")).Obj()

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

	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(nil).
		Obj()
	n2 := util.MakeNode().
		Name("n2").
		Allocatable(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(nil).
		Obj()

	// resources
	res1c3g := api.BuildResourceList("1", "3G")
	res3c1g := api.BuildResourceList("3", "1G")
	res1c0g := api.BuildResourceList("1", "0G")
	res0c1g := api.BuildResourceList("0", "1G")
	res1c1g := api.BuildResourceList("1", "1G")
	// pod
	// p1 := util.BuildPod("ns1", "pod1", "n1", corev1.PodRunning, res1c3g, "pg1", nil, nil)
	// p2 := util.BuildPod("ns1", "pod2", "n2", corev1.PodRunning, res3c1g, "pg2", nil, nil)
	// p3 := util.BuildPod("ns1", "pod3", "", corev1.PodPending, res1c0g, "pg3", nil, nil)
	// p4 := util.BuildPod("ns1", "pod4", "", corev1.PodPending, res0c1g, "pg4", nil, nil)
	// p5 := util.BuildPod("ns1", "pod5", "", corev1.PodPending, res1c1g, "pg5", nil, nil)
	// p6 := util.BuildPod("ns1", "pod6", "", corev1.PodPending, res1c1g, "pg6", nil, nil)
	
	p1 := util.MakePod().
		Namespace("ns1").
		Name("pod1").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(res1c3g).
		GroupName("pg1").
		Labels(nil).
		NodeSelector(nil).
		Obj()
	p2 := util.MakePod().
		Namespace("ns1").
		Name("pod2").
		NodeName("n2").
		PodPhase(corev1.PodRunning).
		ResourceList(res3c1g).
		GroupName("pg2").
		Labels(nil).
		NodeSelector(nil).
		Obj()
	p3 := util.MakePod().
		Namespace("ns1").
		Name("pod3").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(res1c0g).
		GroupName("pg3").
		Labels(nil).
		NodeSelector(nil).
		Obj()
	p4 := util.MakePod().
		Namespace("ns1").
		Name("pod4").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(res0c1g).
		GroupName("pg4").
		Labels(nil).
		NodeSelector(nil).
		Obj()
	p5 := util.MakePod().
		Namespace("ns1").
		Name("pod5").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(res1c1g).
		GroupName("pg5").
		Labels(nil).
		NodeSelector(nil).
		Obj()
	p6 := util.MakePod().
		Namespace("ns1").
		Name("pod6").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(res1c1g).
		GroupName("pg6").
		Labels(nil).
		NodeSelector(nil).
		Obj()
		// podgroup
		// pg1 := util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning)
		// pg2 := util.BuildPodGroup("pg2", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning)
		// pg3 := util.BuildPodGroup("pg3", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
		// pg4 := util.BuildPodGroup("pg4", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupPending)
		// pg5 := util.BuildPodGroup("pg5", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
		// pg6WithClosedQueue := util.BuildPodGroup("pg6", "ns1", "q3", 1, nil, schedulingv1beta1.PodGroupPending)

	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q2").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg3 := util.MakePodGroup().
		Name("pg3").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()

	pg4 := util.MakePodGroup().
		Name("pg4").
		Namespace("ns1").
		Queue("q2").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()
	pg5 := util.MakePodGroup().
		Name("pg5").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()
	pg6WithClosedQueue := util.MakePodGroup().
		Name("pg6").
		Namespace("ns1").
		Queue("q3").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()

	pg1.Spec.MinResources = &res1c3g
	pg2.Spec.MinResources = &res3c1g
	pg3.Spec.MinResources = &res1c0g
	pg4.Spec.MinResources = &res0c1g
	pg5.Spec.MinResources = &res1c1g
	pg6WithClosedQueue.Spec.MinResources = &res1c1g

	// queue1 := util.BuildQueueWithResourcesQuantity("q1", api.BuildResourceList("2", "2G"), api.BuildResourceList("2", "2G"))
	// queue2 := util.BuildQueueWithResourcesQuantity("q2", api.BuildResourceList("2", "2G"), api.BuildResourceList("3", "3G"))
	queue1 := util.MakeQueue().Name("q1").Weight(1).State(schedulingv1beta1.QueueStateOpen).Deserved(api.BuildResourceList("2", "2G")).Capability(api.BuildResourceList("2", "2G")).Obj()
	queue2 := util.MakeQueue().Name("q2").Weight(1).State(schedulingv1beta1.QueueStateOpen).Deserved(api.BuildResourceList("2", "2G")).Capability(api.BuildResourceList("3", "3G")).Obj()

	// closedQueue3 := util.BuildQueueWithState("q3", 1, api.BuildResourceList("3", "3G"), schedulingv1beta1.QueueStateClosed)
	closedQueue3 := util.MakeQueue().Name("q3").Weight(1).State(schedulingv1beta1.QueueStateClosed).Capability(api.BuildResourceList("3", "3G")).Obj()

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
	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "pods", Value: "11"}}...)).
		Capacity(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "pods", Value: "11"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()

	// resources for test case 0
	// pod
	p1 := util.MakePod().
		Namespace("ns1").
		Name("p1").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg1").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// podgroup
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("ns1").
		Queue("q11").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	// queue
	root := util.MakeQueue().Name("root").Parent("").Deserved(nil).Capability(nil).Weight(1).Obj()
	root1 := util.MakeQueue().Name("root").Parent("").Deserved(nil).Capability(api.BuildResourceList("16", "16Gi")).Weight(1).Obj()
	
	queue1 := util.MakeQueue().Name("q1").Parent("root").Deserved(nil).Capability(api.BuildResourceList("4", "4Gi")).Weight(1).Obj()
	queue2 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q2").Parent("root").Deserved(nil).Capability(api.BuildResourceList("4", "4Gi")).Weight(1).Obj()
	queue11 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q11").Parent("q1").Deserved(nil).Capability(api.BuildResourceList("1", "1Gi")).Weight(1).Obj()
	queue12 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q12").Parent("q1").Deserved(nil).Capability(api.BuildResourceList("3", "3Gi")).Weight(1).Obj()

	// resources for test case 1
	// pod
	p2 := util.MakePod().
		Namespace("ns1").
		Name("p2").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi")).
		GroupName("pg2").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
		// podgroup
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("ns1").
		Queue("q1").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()
	// resources for test case 2
	// pod
	p3 := util.MakePod().
		Namespace("ns1").
		Name("p3").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg3").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p4 := util.MakePod().
		Namespace("ns1").
		Name("p4").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg3").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// podgroup
	pg3 := util.MakePodGroup().
		Name("pg3").
		Namespace("ns1").
		Queue("q31").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	// queue

	queue3 := util.MakeQueue().Name("q3").State(schedulingv1beta1.QueueStateOpen).Parent("root").Deserved(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).Weight(1).Obj()
	queue4 := util.MakeQueue().Name("q4").State(schedulingv1beta1.QueueStateOpen).Parent("root").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Weight(1).Obj()
	queue31 := util.MakeQueue().Name("q31").State(schedulingv1beta1.QueueStateOpen).Parent("q3").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Weight(1).Obj()
	queue32 := util.MakeQueue().Name("q32").State(schedulingv1beta1.QueueStateOpen).Parent("q3").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Weight(1).Obj()
	queue33 := util.MakeQueue().Name("q33").State(schedulingv1beta1.QueueStateOpen).Parent("q3").Deserved(api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...)).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...)).Weight(1).Obj()

	// resources for test case 3
	// pod

	p5 := util.MakePod().
		Namespace("ns1").
		Name("p5").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("4", "4Gi")).
		GroupName("pg4").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p6 := util.MakePod().
		Namespace("ns1").
		Name("p6").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg5").
		Labels(map[string]string{schedulingv1beta1.PodPreemptable: "false"}).
		NodeSelector(make(map[string]string)).
		Obj()
	p7 := util.MakePod().
		Namespace("ns1").
		Name("p7").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg5").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p8 := util.MakePod().
		Namespace("ns1").
		Name("p8").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg6").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p9 := util.MakePod().
		Namespace("ns1").
		Name("p9").
		NodeName("n1").
		PodPhase(corev1.PodRunning).
		ResourceList(api.BuildResourceList("2", "2Gi")).
		GroupName("pg7").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p10 := util.MakePod().
		Namespace("ns1").
		Name("p10").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("3", "3Gi")).
		GroupName("pg8").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// podgroup

	pg4 := util.MakePodGroup().
		Name("pg4").
		Namespace("ns1").
		Queue("q4").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg5 := util.MakePodGroup().
		Name("pg5").
		Namespace("ns1").
		Queue("q31").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg6 := util.MakePodGroup().
		Name("pg6").
		Namespace("ns1").
		Queue("q32").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	pg7 := util.MakePodGroup().
		Name("pg7").
		Namespace("ns1").
		Queue("q32").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()
	pg8 := util.MakePodGroup().
		Name("pg8").
		Namespace("ns1").
		Queue("q33").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// resources for test case 5
	// queue

	queue5 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q5").Parent("root").Deserved(nil).Capability(api.BuildResourceList("", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()
	queue51 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q51").Parent("q5").Deserved(nil).Capability(api.BuildResourceList("", "2Gi", []api.ScalarResource{}...)).Weight(1).Obj()

	// podgroup
	pg9 := util.MakePodGroup().
		Name("pg9").
		Namespace("ns1").
		Queue("q51").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupRunning).
		Obj()

	// pod
	p11 := util.MakePod().
		Namespace("ns1").
		Name("p11").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "")).
		GroupName("pg9").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()

	// resources for test case 6
	// queue
	queue6 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q6").Parent("root").Deserved(nil).Capability(api.BuildResourceList("2", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()

	// sub queue 61 and 62's capability is not specified, should be inherited from parent queue

	queue61 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q61").Parent("q6").Deserved(nil).Capability(nil).Weight(1).Obj()
	queue62 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q62").Parent("q6").Deserved(nil).Capability(nil).Weight(1).Obj()

	// podgroup
	pg10 := util.MakePodGroup().
		Name("pg10").
		Namespace("ns1").
		Queue("q61").
		MinResources(api.BuildResourceList("2", "4Gi")).
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()
	pg11 := util.MakePodGroup().
		Name("pg11").
		Namespace("ns1").
		Queue("q62").
		MinResources(api.BuildResourceList("2", "4Gi")).
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupPending).
		Obj()
	// resources for test case 7
	// queue
	queue7 := util.MakeQueue().Name("q7").State(schedulingv1beta1.QueueStateOpen).Parent("root").Deserved(nil).Capability(api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()

	// the sum of sub queue 71 and 72's guarantee exceeds the capacity of queue7, but should not panic
	queue71 := util.MakeQueue().Name("q71").State(schedulingv1beta1.QueueStateOpen).Parent("q7").Deserved(nil).Capability(api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()
	queue72 := util.MakeQueue().Name("q72").State(schedulingv1beta1.QueueStateOpen).Parent("q7").Deserved(nil).Capability(api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()

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

	// resources for test case 8
	queue8 := buildQueueWithParents("q8", "root", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}}...), nil)
	queue81 := buildQueueWithParents("q81", "q8", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...), nil)
	queue82 := buildQueueWithParents("q81", "q8", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...), nil)
	// node
	gpuNode := util.MakeNode().
		Name("n-gpu").
		Allocatable(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}}...)).
		Capacity(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()

	// podgroup
	pg12 := util.MakePodGroup().
		Name("pg12").
		Namespace("ns1").
		Queue("q81").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// pod
	p12 := util.MakePod().
		Namespace("ns1").
		Name("p12").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}}...)).
		GroupName("pg12").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()

	// resources for test case 9
	queue9 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q9").Parent("root").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "hugepages-1Gi", Value: "0"}}...)).Capability(nil).Weight(1).Obj()
	queue91 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("q91").Parent("q9").Deserved(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}, {Name: "hugepages-1Gi", Value: "0"}}...)).Capability(nil).Weight(1).Obj()

	// node
	n2 := util.MakeNode().
		Name("n2").
		Allocatable(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}, {Name: "hugepages-1Gi", Value: "0"}}...)).
		Capacity(api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}, {Name: "hugepages-1Gi", Value: "0"}}...)).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	// podgroup
	pg13 := util.MakePodGroup().
		Name("pg13").
		Namespace("ns1").
		Queue("q91").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	// pod
	p13 := util.MakePod().
		Namespace("ns1").
		Name("p13").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}}...)).
		GroupName("pg13").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	// queue
	queue10 := util.MakeQueue().Name("q10").Parent("root").Deserved(nil).Capability(api.BuildResourceList("10", "4Gi", []api.ScalarResource{}...)).Weight(1).Obj()

	// resources for test case 11
	// queue
	case11_queue1 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("case11_queue1").Parent("root").Deserved(nil).Capability(nil).Weight(1).Obj()
	case11_queue11 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("case11_queue11").Parent("case11_queue1").Deserved(nil).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...)).Weight(1).Obj()
	case11_queue12 := util.MakeQueue().State(schedulingv1beta1.QueueStateOpen).Name("case11_queue12").Parent("case11_queue1").Deserved(nil).Capability(api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...)).Weight(1).Obj()

	// podgroup

	pg14 := util.MakePodGroup().
		Name("pg14").
		Namespace("ns1").
		Queue("case11_queue11").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()
	pg15 := util.MakePodGroup().
		Name("pg15").
		Namespace("ns1").
		Queue("case11_queue12").
		MinMember(1).
		MinTaskMember(nil).
		Phase(schedulingv1beta1.PodGroupInqueue).
		Obj()

	// pod
	p14 := util.MakePod().
		Namespace("ns1").
		Name("p14").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...)).
		GroupName("pg14").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
	p15 := util.MakePod().
		Namespace("ns1").
		Name("p15").
		NodeName("").
		PodPhase(corev1.PodPending).
		ResourceList(api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}}...)).
		GroupName("pg15").
		Labels(make(map[string]string)).
		NodeSelector(make(map[string]string)).
		Obj()
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
		{
			Name:      "case8: When the queue's deserved value has a scalar resource set, the check can pass",
			Plugins:   plugins,
			Nodes:     []*corev1.Node{gpuNode},
			PodGroups: []*schedulingv1beta1.PodGroup{pg12},
			Pods:      []*corev1.Pod{p12},
			Queues:    []*schedulingv1beta1.Queue{root, queue8, queue81, queue82},
			ExpectBindMap: map[string]string{
				"ns1/p12": "n-gpu",
			},
			ExpectBindsNum: 1,
		},
		{
			Name:      "case9: When some scalar resources are 0 in deserved, the check can still pass",
			Plugins:   plugins,
			Nodes:     []*corev1.Node{n2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg13},
			Pods:      []*corev1.Pod{p13},
			Queues:    []*schedulingv1beta1.Queue{root, queue9, queue91},
			ExpectBindMap: map[string]string{
				"ns1/p13": "n2",
			},
			ExpectBindsNum: 1,
		},
		{
			Name:    "case10: root queue's capability > cluster total resource, but should not panic",
			Plugins: plugins,
			Nodes:   []*corev1.Node{n1},
			Queues:  []*schedulingv1beta1.Queue{root1, queue10},
		},
		{
			Name:      "case11: If the capability scalar resources is not specified, the value should be inherited from parent queue",
			Plugins:   plugins,
			Pods:      []*corev1.Pod{p14, p15},
			Nodes:     []*corev1.Node{n2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg14, pg15},
			Queues:    []*schedulingv1beta1.Queue{root, case11_queue1, case11_queue11, case11_queue12},
			ExpectBindMap: map[string]string{
				"ns1/p14": "n2",
				"ns1/p15": "n2",
			},
			ExpectBindsNum: 2,
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

// func buildQueueWithParents(name string, parent string, deserved corev1.ResourceList, cap corev1.ResourceList) *schedulingv1beta1.Queue {
// 	queue := util.BuildQueueWithResourcesQuantity(name, deserved, cap)
// 	queue.Spec.Parent = parent
// 	return queue
// }
