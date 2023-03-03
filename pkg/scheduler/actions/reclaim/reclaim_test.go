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
	"reflect"
	"testing"
	"time"

	"volcano.sh/volcano/pkg/scheduler/api"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestReclaim(t *testing.T) {
	framework.RegisterPluginBuilder("conformance", conformance.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name      string
		podGroups []*schedulingv1beta1.PodGroup
		pods      []*v1.Pod
		nodes     []*v1.Node
		queues    []*schedulingv1beta1.Queue
		expected  int
	}{
		{
			name: "Two Queue with one Queue overusing resource, should reclaim",
			podGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue:             "q1",
						PriorityClassName: "low-priority",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue:             "q2",
						PriorityClassName: "high-priority",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("3", "3Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight: 1,
					},
				},
			},
			expected: 1,
		},
	}

	reclaim := New()

	for i, test := range tests {
		binder := &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		}
		evictor := &util.FakeEvictor{
			Channel: make(chan string),
		}

		option := options.NewServerOption()
		option.RegisterOptions()
		config, err := kube.BuildConfig(option.KubeClientOptions)
		if err != nil {
			return
		}

		sc := cache.New(config, option.SchedulerNames, option.DefaultQueue, option.NodeSelector)
		schedulerCache := sc.(*cache.SchedulerCache)
		schedulerCache.Binder = binder
		schedulerCache.Evictor = evictor
		schedulerCache.StatusUpdater = &util.FakeStatusUpdater{}
		schedulerCache.VolumeBinder = &util.FakeVolumeBinder{}
		schedulerCache.Recorder = record.NewFakeRecorder(100)

		schedulerCache.PriorityClasses["high-priority"] = &schedulingv1.PriorityClass{
			Value: 100000,
		}
		schedulerCache.PriorityClasses["low-priority"] = &schedulingv1.PriorityClass{
			Value: 10,
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, ss := range test.podGroups {
			schedulerCache.AddPodGroupV1beta1(ss)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueueV1beta1(q)
		}

		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:               "conformance",
						EnabledReclaimable: &trueValue,
					},
					{
						Name:               "gang",
						EnabledReclaimable: &trueValue,
					},
					{
						Name:               "proportion",
						EnabledReclaimable: &trueValue,
					},
				},
			},
		}, nil)
		defer framework.CloseSession(ssn)

		reclaim.Execute(ssn)

		for i := 0; i < test.expected; i++ {
			select {
			case <-evictor.Channel:
			case <-time.After(3 * time.Second):
				t.Errorf("Failed to get Evictor request.")
			}
		}

		if test.expected != len(evictor.Evicts()) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expected, len(evictor.Evicts()))
		}
	}
}

func TestReclaimWithQueuePriorityClass(t *testing.T) {
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("drf", drf.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name                  string
		priorityWeight        float64
		drfWeight             float64
		proportionWeight      float64
		enableDrfHierarchical bool
		podGroups             []*schedulingv1beta1.PodGroup
		pods                  []*v1.Pod
		nodes                 []*v1.Node
		queues                []*schedulingv1beta1.Queue
		priorityClasss        []*schedulingv1.PriorityClass
		expected              []string
	}{
		{
			name:                  "no drf, two pending job in two queue, priority play a crucial role ",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg3",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q3",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg3-p1", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg3-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("4", "4Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "low",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "middle",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q3",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
			},
			priorityClasss: []*schedulingv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "low",
					},
					Value: 70,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "middle",
					},
					Value: 80,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "high",
					},
					Value: 100,
				},
			},
			expected: []string{
				"pg3-p2",
				"pg2-p2",
			},
		},
		{
			name:                  "no drf, two pending job in two queue, proportion play a crucial role ",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg3",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q3",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg3-p1", "", v1.PodRunning, util.BuildResourceList("2", "1G"), "pg3", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg3-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("5", "4Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "low",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q3",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
			},
			priorityClasss: []*schedulingv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "low",
					},
					Value: 70,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "middle",
					},
					Value: 80,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "high",
					},
					Value: 100,
				},
			},
			expected: []string{
				"pg2-p2",
				"pg3-p2",
			},
		},
		{
			name:                  "two pending job in two queue, drf play a crucial role ",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: true,
			podGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg3",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "q3",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodRunning, util.BuildResourceList("3", "4Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodRunning, util.BuildResourceList("3", "4Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p3", "", v1.PodRunning, util.BuildResourceList("3", "4Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p4", "", v1.PodRunning, util.BuildResourceList("3", "4Gi"), "pg1", make(map[string]string), make(map[string]string)),

				util.BuildPod("c1", "pg2-p1", "", v1.PodRunning, util.BuildResourceList("4", "1Gi"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("4", "1Gi"), "pg2", make(map[string]string), make(map[string]string)),

				util.BuildPod("c1", "pg3-p1", "", v1.PodRunning, util.BuildResourceList("1", "7Gi"), "pg3", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg3-p2", "", v1.PodPending, util.BuildResourceList("1", "7Gi"), "pg3", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("36", "48Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/q1",
							"volcano.sh/hierarchy-weights": "root/1",
						},
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/q2",
							"volcano.sh/hierarchy-weights": "root/1",
						},
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q3",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/q3",
							"volcano.sh/hierarchy-weights": "root/1",
						},
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight:            1,
						PriorityClassName: "high",
					},
				},
			},
			priorityClasss: []*schedulingv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "low",
					},
					Value: 70,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "middle",
					},
					Value: 80,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "high",
					},
					Value: 100,
				},
			},
			expected: []string{
				"pg2-p2",
				"pg3-p2",
			},
		},
	}

	reclaim := New()

	for i, test := range tests {

		option := options.NewServerOption()
		option.RegisterOptions()
		config, err := kube.BuildConfig(option.KubeClientOptions)
		if err != nil {
			return
		}

		sc := cache.New(config, option.SchedulerNames, option.DefaultQueue, option.NodeSelector)
		schedulerCache := sc.(*cache.SchedulerCache)
		schedulerCache.StatusUpdater = &util.FakeStatusUpdater{}
		schedulerCache.VolumeBinder = &util.FakeVolumeBinder{}
		schedulerCache.Recorder = record.NewFakeRecorder(100)

		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		priorityPod := int32(100)
		for _, pod := range test.pods {
			priorityPod--
			pod.Spec.Priority = &priorityPod
			schedulerCache.AddPod(pod)
		}

		for _, ss := range test.podGroups {
			schedulerCache.AddPodGroupV1beta1(ss)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueueV1beta1(q)
		}

		for _, pc := range test.priorityClasss {
			schedulerCache.AddPriorityClass(pc)
		}

		trueValue := true
		enableDrfHierarchical := test.enableDrfHierarchical
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                   "priority",
						EnabledQueueScoreOrder: &trueValue,
						EnabledPreemptable:     &trueValue,
						EnabledJobOrder:        &trueValue,
						EnabledTaskOrder:       &trueValue,
						EnabledNamespaceOrder:  &trueValue,
						EnabledAllocatable:     &trueValue,
					},
					{
						Name:                   "drf",
						EnabledQueueScoreOrder: &trueValue,
						EnabledPreemptable:     &trueValue,
						EnabledJobOrder:        &trueValue,
						EnabledNamespaceOrder:  &trueValue,
						EnabledHierarchy:       &enableDrfHierarchical,
						EnabledAllocatable:     &trueValue,
					},
					{
						Name:                   "proportion",
						EnabledQueueScoreOrder: &trueValue,
						EnabledQueueOrder:      &trueValue,
						EnabledReclaimable:     &trueValue,
						EnabledAllocatable:     &trueValue,
					},
				},
			},
		}, []conf.Configuration{
			{
				Name: "reclaim",
				Arguments: map[string]interface{}{
					"QueueScoreOrderEnable":             true,
					"QueueScoreOrder.priority.weight":   test.priorityWeight,
					"QueueScoreOrder.drf.weight":        test.drfWeight,
					"QueueScoreOrder.proportion.weight": test.proportionWeight,
				},
			},
		})
		defer framework.CloseSession(ssn)
		actual := make([]string, 0)
		ssn.AddAllocatableFn("priority", func(queue *api.QueueInfo, task *api.TaskInfo) bool {
			actual = append(actual, task.Name)
			return true
		})

		reclaim.Execute(ssn)

		if !reflect.DeepEqual(test.expected, actual) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expected, actual)
		}
	}
}
