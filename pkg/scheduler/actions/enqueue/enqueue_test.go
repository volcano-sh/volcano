/*
 Copyright 2023 The Volcano Authors.
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

package enqueue

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingk8sv1 "k8s.io/api/scheduling/v1"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestAllocateWithQueuePriorityClass(t *testing.T) {
	var tmp *cache.SchedulerCache
	patches := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "AddBindTask", func(scCache *cache.SchedulerCache, task *api.TaskInfo) error {
		scCache.Binder.Bind(nil, []*api.TaskInfo{task})
		return nil
	})
	defer patches.Reset()

	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("drf", drf.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)

	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name                  string
		priorityWeight        float64
		drfWeight             float64
		proportionWeight      float64
		enableDrfHierarchical bool
		podGroups             []*schedulingv1.PodGroup
		pods                  []*v1.Pod
		nodes                 []*v1.Node
		queues                []*schedulingv1.Queue
		priorityClasss        []*schedulingk8sv1.PriorityClass
		expected              []string
	}{
		{
			name:                  "no drf, two pending job in two empty and different priority class queue, priority play a crucial role",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q1-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q2-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "q1-pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "q2-pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("4", "8Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-node-critical",
					},
				},
			},
			priorityClasss: []*schedulingk8sv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-cluster-critical",
					},
					Value: 2000000000,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-node-critical",
					},
					Value: 2000001000,
				},
			},
			expected: []string{
				"q2-pg1",
				"q1-pg1",
			},
		},
		{
			name:                  "no drf, one running job in high priority queue, one pending job in hight priority queue, one pending job in low priority queue， proportion play a crucial role",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q1-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q2-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q2-pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "q1-pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "q2-pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p3", "", v1.PodPending, util.BuildResourceList("1", "1G"), "q2-pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("4", "8Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-node-critical",
					},
				},
			},
			priorityClasss: []*schedulingk8sv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-cluster-critical",
					},
					Value: 2000000000,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-node-critical",
					},
					Value: 2000001000,
				},
			},
			expected: []string{
				"q1-pg1",
				"q2-pg2",
			},
		},
		{
			name:                  "one pending&running job in  in high priority queue, one pending&running job in  in low priority queue, proportion weight is the same, drf play a crucial role",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: true,
			podGroups: []*schedulingv1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q1-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q1-pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q1",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q2-pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupInqueue,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "q2-pg2",
						Namespace: "c1",
					},
					Spec: schedulingv1.PodGroupSpec{
						Queue: "q2",
					},
					Status: schedulingv1.PodGroupStatus{
						Phase: schedulingv1.PodGroupPending,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "q1-pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodRunning, util.BuildResourceList("2", "1Gi"), "q1-pg2", make(map[string]string), make(map[string]string)),

				util.BuildPod("c1", "p3", "", v1.PodRunning, util.BuildResourceList("1", "8Gi"), "q2-pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p4", "", v1.PodPending, util.BuildResourceList("1", "8Gi"), "q2-pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("12", "32Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/q1",
							"volcano.sh/hierarchy-weights": "root/1",
						},
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
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
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-node-critical",
					},
				},
			},
			priorityClasss: []*schedulingk8sv1.PriorityClass{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-cluster-critical",
					},
					Value: 99,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-node-critical",
					},
					Value: 100,
				},
			},
			expected: []string{
				"q1-pg1",
				"q2-pg2",
			},
		},
	}

	enqueue := New()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binder := &util.FakeBinder{
				BindSlice: make([]string, 0),
				Binds:     map[string]string{},
				Channel:   make(chan string),
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
							EnabledJobEnqueued:     &trueValue,
						},
						{
							Name:                   "drf",
							EnabledQueueScoreOrder: &trueValue,
							EnabledPreemptable:     &trueValue,
							EnabledJobOrder:        &trueValue,
							EnabledNamespaceOrder:  &trueValue,
							EnabledHierarchy:       &enableDrfHierarchical,
						},
						{
							Name:                   "proportion",
							EnabledQueueScoreOrder: &trueValue,
							EnabledQueueOrder:      &trueValue,
							EnabledReclaimable:     &trueValue,
						},
					},
				},
			}, []conf.Configuration{
				{
					Name: "enqueue",
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
			ssn.AddJobEnqueuedFn("priority", func(obj interface{}) {
				job := obj.(*api.JobInfo)
				actual = append(actual, job.Name)
			})
			enqueue.Execute(ssn)

			if !reflect.DeepEqual(test.expected, actual) {
				t.Errorf("expected: %v, got %v ", test.expected, actual)
			}
		})
	}
}
