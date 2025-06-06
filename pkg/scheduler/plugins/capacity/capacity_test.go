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
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"selector": "worker"})
	n2 := util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})

	// resources for test case 0
	// pod
	p1 := util.BuildPod("ns1", "p1", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), map[string]string{"selector": "worker"})
	// podgroup
	pg1 := util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg2 := util.BuildPodGroup("pg2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// queue
	queue1 := util.BuildQueueWithResourcesQuantity("q1", nil, api.BuildResourceList("2", "2Gi"))

	// resources for test case 1
	// pod
	p3 := util.BuildPod("ns1", "p3", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg3", make(map[string]string), make(map[string]string))
	p4 := util.BuildPod("ns1", "p4", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg4", make(map[string]string), make(map[string]string))
	// podgroup
	pg3 := util.BuildPodGroup("pg3", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg4 := util.BuildPodGroup("pg4", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// queue
	queue2 := util.BuildQueueWithResourcesQuantity("q2", nil, api.BuildResourceList("1.5", "1.5Gi"))

	// resources for test case 2
	// pod
	p5 := util.BuildPod("ns1", "p5", "n1", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg5", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p6 := util.BuildPod("ns1", "p6", "n2", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg5", make(map[string]string), make(map[string]string))
	p7 := util.BuildPod("ns1", "p7", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg6", make(map[string]string), make(map[string]string))
	// podgroup
	pg5 := util.BuildPodGroup("pg5", "ns1", "q3", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg6 := util.BuildPodGroup("pg6", "ns1", "q4", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// queue
	queue3 := util.BuildQueueWithResourcesQuantity("q3", api.BuildResourceList("2", "4Gi"), nil)
	queue4 := util.BuildQueueWithResourcesQuantity("q4", api.BuildResourceList("2", "4Gi"), nil)

	// resources for test case3
	// nodes
	n3 := util.BuildNode("n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...), map[string]string{"selector": "worker"})
	n4 := util.BuildNode("n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}, {Name: "pods", Value: "10"}}...), map[string]string{})

	// pod
	p8 := util.BuildPod("ns1", "p8", "n3", corev1.PodRunning, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg7", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p9 := util.BuildPod("ns1", "p9", "n4", corev1.PodRunning, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg7", make(map[string]string), make(map[string]string))

	p10 := util.BuildPod("ns1", "p10", "n3", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg8", make(map[string]string), make(map[string]string))
	p11 := util.BuildPod("ns1", "p11", "n4", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg8", make(map[string]string), make(map[string]string))

	p12 := util.BuildPod("ns1", "p12", "", corev1.PodPending, api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), "pg9", make(map[string]string), make(map[string]string))

	// podgroup
	pg7 := util.BuildPodGroup("pg7", "ns1", "q5", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg8 := util.BuildPodGroup("pg8", "ns1", "q6", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg9 := util.BuildPodGroup("pg9", "ns1", "q6", 1, nil, schedulingv1beta1.PodGroupInqueue)

	// queue
	queue5 := util.BuildQueueWithResourcesQuantity("q5", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), nil)
	queue6 := util.BuildQueueWithResourcesQuantity("q6", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "nvidia.com/A100", Value: "10"}}...), nil)

	// resource for test case 4
	// nodes
	n5 := util.BuildNode("n5", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n6 := util.BuildNode("n6", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))

	// pod
	p13 := util.BuildPod("ns1", "p13", "n5", corev1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg10", make(map[string]string), make(map[string]string))
	p14 := util.BuildPod("ns1", "p14", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg11", make(map[string]string), make(map[string]string))
	p15 := util.BuildPod("ns1", "p15", "", corev1.PodPending, api.BuildResourceList("2", "4Gi"), "pg12", make(map[string]string), make(map[string]string))

	// podgroup
	pg10 := util.BuildPodGroup("pg10", "ns1", "q7", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg11 := util.BuildPodGroup("pg11", "ns1", "q8", 1, nil, schedulingv1beta1.PodGroupInqueue)
	pg12 := util.BuildPodGroup("pg12", "ns1", "q9", 1, nil, schedulingv1beta1.PodGroupInqueue)

	// queue
	queue7 := util.BuildQueueWithPriorityAndResourcesQuantity("q7", 5, nil, api.BuildResourceList("2", "4Gi"))
	queue8 := util.BuildQueueWithPriorityAndResourcesQuantity("q8", 1, nil, api.BuildResourceList("2", "4Gi"))
	queue9 := util.BuildQueueWithPriorityAndResourcesQuantity("q9", 10, nil, api.BuildResourceList("2", "4Gi"))

	// case5: p16 + p17 in queue10 will exceed queue's deserved, is not preemptive
	p16 := util.BuildPod("ns1", "p16", "n1", corev1.PodRunning, api.BuildResourceList("1", "3Gi"), "pg16", make(map[string]string), nil)
	p17 := util.BuildPod("ns1", "p17", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg17", make(map[string]string), nil)
	p18 := util.BuildPod("ns1", "p18", "n1", corev1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg18", make(map[string]string), nil)
	// podgroup
	pg16 := util.BuildPodGroup("pg16", "ns1", "q10", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg17 := util.BuildPodGroup("pg17", "ns1", "q10", 1, nil, schedulingv1beta1.PodGroupInqueue)
	pg18 := util.BuildPodGroup("pg18", "ns1", "q11", 1, nil, schedulingv1beta1.PodGroupRunning)
	// queue
	queue10 := util.BuildQueueWithResourcesQuantity("q10", api.BuildResourceList("2", "2Gi"), api.BuildResourceList("4", "4Gi"))
	queue11 := util.BuildQueueWithResourcesQuantity("q11", api.BuildResourceList("0", "0Gi"), api.BuildResourceList("2", "2Gi"))

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
	n1 := util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)
	n2 := util.BuildNode("n2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

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
	pg1 := util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg2 := util.BuildPodGroup("pg2", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg3 := util.BuildPodGroup("pg3", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
	pg4 := util.BuildPodGroup("pg4", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupPending)
	pg5 := util.BuildPodGroup("pg5", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
	pg6WithClosedQueue := util.BuildPodGroup("pg6", "ns1", "q3", 1, nil, schedulingv1beta1.PodGroupPending)
	pg1.Spec.MinResources = &res1c3g
	pg2.Spec.MinResources = &res3c1g
	pg3.Spec.MinResources = &res1c0g
	pg4.Spec.MinResources = &res0c1g
	pg5.Spec.MinResources = &res1c1g
	pg6WithClosedQueue.Spec.MinResources = &res1c1g

	queue1 := util.BuildQueueWithResourcesQuantity("q1", api.BuildResourceList("2", "2G"), api.BuildResourceList("2", "2G"))
	queue2 := util.BuildQueueWithResourcesQuantity("q2", api.BuildResourceList("2", "2G"), api.BuildResourceList("3", "3G"))
	closedQueue3 := util.BuildQueueWithState("q3", 1, api.BuildResourceList("3", "3G"), schedulingv1beta1.QueueStateClosed)

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
	n1 := util.BuildNode("n1", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "pods", Value: "11"}}...), map[string]string{})

	// resources for test case 0
	// pod
	p1 := util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), map[string]string{})
	// podgroup
	pg1 := util.BuildPodGroup("pg1", "ns1", "q11", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// queue
	root := buildQueueWithParents("root", "", nil, nil)
	queue1 := buildQueueWithParents("q1", "root", nil, api.BuildResourceList("4", "4Gi"))
	queue2 := buildQueueWithParents("q2", "root", nil, api.BuildResourceList("4", "4Gi"))
	queue11 := buildQueueWithParents("q11", "q1", nil, api.BuildResourceList("1", "1Gi"))
	queue12 := buildQueueWithParents("q12", "q1", nil, api.BuildResourceList("3", "3Gi"))

	// resources for test case 1
	// pod
	p2 := util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), map[string]string{})
	// podgroup
	pg2 := util.BuildPodGroup("pg2", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)

	// resources for test case 2
	// pod
	p3 := util.BuildPod("ns1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg3", make(map[string]string), map[string]string{})
	p4 := util.BuildPod("ns1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg3", make(map[string]string), map[string]string{})
	// podgroup
	pg3 := util.BuildPodGroup("pg3", "ns1", "q31", 2, nil, schedulingv1beta1.PodGroupInqueue)
	// queue
	queue3 := buildQueueWithParents("q3", "root", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...))
	queue4 := buildQueueWithParents("q4", "root", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...))
	queue31 := buildQueueWithParents("q31", "q3", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...))
	queue32 := buildQueueWithParents("q32", "q3", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...))
	queue33 := buildQueueWithParents("q33", "q3", api.BuildResourceList("0", "0Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "4"}}...))

	// resources for test case 3
	// pod
	p5 := util.BuildPod("ns1", "p5", "n1", corev1.PodRunning, api.BuildResourceList("4", "4Gi"), "pg4", map[string]string{}, make(map[string]string))
	p6 := util.BuildPod("ns1", "p6", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg5", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string))
	p7 := util.BuildPod("ns1", "p7", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg5", make(map[string]string), make(map[string]string))
	p8 := util.BuildPod("ns1", "p8", "", corev1.PodPending, api.BuildResourceList("2", "2Gi"), "pg6", make(map[string]string), map[string]string{})
	p9 := util.BuildPod("ns1", "p9", "n1", corev1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg7", make(map[string]string), map[string]string{})
	p10 := util.BuildPod("ns1", "p10", "", corev1.PodPending, api.BuildResourceList("3", "3Gi"), "pg8", make(map[string]string), make(map[string]string))

	// podgroup
	pg4 := util.BuildPodGroup("pg4", "ns1", "q4", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg5 := util.BuildPodGroup("pg5", "ns1", "q31", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg6 := util.BuildPodGroup("pg6", "ns1", "q32", 1, nil, schedulingv1beta1.PodGroupInqueue)
	pg7 := util.BuildPodGroup("pg7", "ns1", "q32", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg8 := util.BuildPodGroup("pg8", "ns1", "q33", 1, nil, schedulingv1beta1.PodGroupInqueue)

	// resources for test case 5
	// queue
	queue5 := buildQueueWithParents("q5", "root", nil, api.BuildResourceList("", "4Gi", []api.ScalarResource{}...))
	queue51 := buildQueueWithParents("q51", "q5", nil, api.BuildResourceList("", "2Gi", []api.ScalarResource{}...))
	// podgroup
	pg9 := util.BuildPodGroup("pg9", "ns1", "q51", 1, nil, schedulingv1beta1.PodGroupRunning)
	// pod
	p11 := util.BuildPod("ns1", "p11", "", corev1.PodPending, api.BuildResourceList("1", ""), "pg9", make(map[string]string), map[string]string{})

	// resources for test case 6
	// queue
	queue6 := buildQueueWithParents("q6", "root", nil, api.BuildResourceList("2", "4Gi", []api.ScalarResource{}...))
	// sub queue 61 and 62's capability is not specified, should be inherited from parent queue
	queue61 := buildQueueWithParents("q61", "q6", nil, nil)
	queue62 := buildQueueWithParents("q62", "q6", nil, nil)
	// podgroup
	pg10 := util.BuildPodGroupWithMinResources("pg10", "ns1", "q61", 1, nil, api.BuildResourceList("2", "4Gi"), schedulingv1beta1.PodGroupPending)
	pg11 := util.BuildPodGroupWithMinResources("pg11", "ns1", "q62", 1, nil, api.BuildResourceList("2", "4Gi"), schedulingv1beta1.PodGroupPending)

	// resources for test case 7
	// queue
	queue7 := buildQueueWithParents("q7", "root", nil, api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...))
	// the sum of sub queue 71 and 72's guarantee exceeds the capacity of queue7, but should not panic
	queue71 := buildQueueWithParents("q71", "q7", nil, api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...))
	queue72 := buildQueueWithParents("q72", "q7", nil, api.BuildResourceList("6", "4Gi", []api.ScalarResource{}...))
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
	gpuNode := util.BuildNode("n-gpu", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}}...), map[string]string{})
	// podgroup
	pg12 := util.BuildPodGroup("pg12", "ns1", "q81", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// pod
	p12 := util.BuildPod("ns1", "p12", "", corev1.PodPending, api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}}...), "pg12", make(map[string]string), make(map[string]string))

	// resources for test case 9
	queue9 := buildQueueWithParents("q9", "root", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "hugepages-1Gi", Value: "0"}}...), nil)
	queue91 := buildQueueWithParents("q91", "q9", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "4"}, {Name: "hugepages-1Gi", Value: "0"}}...), nil)
	// node
	n2 := util.BuildNode("n2", api.BuildResourceList("8", "8Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "8"}, {Name: "pods", Value: "10"}, {Name: "hugepages-1Gi", Value: "0"}}...), map[string]string{})
	// podgroup
	pg13 := util.BuildPodGroup("pg13", "ns1", "q91", 1, nil, schedulingv1beta1.PodGroupInqueue)
	// pod
	p13 := util.BuildPod("ns1", "p13", "", corev1.PodPending, api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}}...), "pg13", make(map[string]string), make(map[string]string))

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

func buildQueueWithParents(name string, parent string, deserved corev1.ResourceList, cap corev1.ResourceList) *schedulingv1beta1.Queue {
	queue := util.BuildQueueWithResourcesQuantity(name, deserved, cap)
	queue.Spec.Parent = parent
	return queue
}
