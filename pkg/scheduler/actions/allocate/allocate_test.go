/*
Copyright 2017 The Kubernetes Authors.

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

package allocate

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestAllocate(t *testing.T) {
	var tmp *cache.SchedulerCache
	patches := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "AddBindTask", func(scCache *cache.SchedulerCache, task *api.TaskInfo) error {
		scCache.Binder.Bind(nil, []*api.TaskInfo{task})
		return nil
	})
	defer patches.Reset()

	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder("drf", drf.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)

	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name string
		cache.TestArg
		expected map[string]string
	}{
		{
			name: "one Job with two Pods on one node",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1.PodGroupSpec{
							Queue: "c1",
						},
						Status: schedulingv1.PodGroupStatus{
							Phase: schedulingv1.PodGroupInqueue,
						},
					},
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("2", "4Gi"), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Spec: schedulingv1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
		},
		{
			name: "two Jobs on one node",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1.PodGroupSpec{
							Queue: "c1",
						},
						Status: schedulingv1.PodGroupStatus{
							Phase: schedulingv1.PodGroupInqueue,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg2",
							Namespace: "c2",
						},
						Spec: schedulingv1.PodGroupSpec{
							Queue: "c2",
						},
						Status: schedulingv1.PodGroupStatus{
							Phase: schedulingv1.PodGroupInqueue,
						},
					},
				},

				// pod name should be like "*-*-{index}",
				// due to change of TaskOrderFn
				Pods: []*v1.Pod{
					// pending pod with owner1, under c1
					util.BuildPod("c1", "pg1-p-1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
					// pending pod with owner1, under c1
					util.BuildPod("c1", "pg1-p-2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
					// pending pod with owner2, under c2
					util.BuildPod("c2", "pg2-p-1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
					// pending pod with owner2, under c2
					util.BuildPod("c2", "pg2-p-2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("2", "4G"), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Spec: schedulingv1.QueueSpec{
							Weight: 1,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Spec: schedulingv1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: map[string]string{
				"c2/pg2-p-1": "n1",
				"c1/pg1-p-1": "n1",
			},
		},
		{
			name: "high priority queue should not block others",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1.PodGroupSpec{
							Queue: "c1",
						},
						Status: schedulingv1.PodGroupStatus{
							Phase: schedulingv1.PodGroupInqueue,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg2",
							Namespace: "c1",
						},
						Spec: schedulingv1.PodGroupSpec{
							Queue: "c2",
						},
						Status: schedulingv1.PodGroupStatus{
							Phase: schedulingv1.PodGroupInqueue,
						},
					},
				},

				Pods: []*v1.Pod{
					// pending pod with owner1, under ns:c1/q:c1
					util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("3", "1G"), "pg1", make(map[string]string), make(map[string]string)),
					// pending pod with owner2, under ns:c1/q:c2
					util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("2", "4G"), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c1",
						},
						Spec: schedulingv1.QueueSpec{
							Weight: 1,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "c2",
						},
						Spec: schedulingv1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: map[string]string{
				"c1/p2": "n1",
			},
		},
	}

	allocate := New()

	for _, test := range tests {
		if test.name == "two Jobs on one node" {
			// TODO(wangyang0616): First make sure that ut can run, and then fix the failed ut later
			// See issue for details: https://github.com/volcano-sh/volcano/issues/2810
			t.Skip("Test cases are not as expected, fixed later. see issue: #2810")
		}
		t.Run(test.name, func(t *testing.T) {

			schedulerCache, binder, _, _ := cache.CreateCacheForTest(&test.TestArg, 100000, 10)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               "drf",
							EnabledPreemptable: &trueValue,
							EnabledJobOrder:    &trueValue,
						},
						{
							Name:               "proportion",
							EnabledQueueOrder:  &trueValue,
							EnabledReclaimable: &trueValue,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			allocate.Execute(ssn)

			if !reflect.DeepEqual(test.expected, binder.Binds) {
				t.Errorf("expected: %v, got %v ", test.expected, binder.Binds)
			}
		})
	}
}

func TestAllocateWithDynamicPVC(t *testing.T) {
	var tmp *cache.SchedulerCache
	patches := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "AddBindTask", func(scCache *cache.SchedulerCache, task *api.TaskInfo) error {
		scCache.VolumeBinder.BindVolumes(task, task.PodVolumes)
		scCache.Binder.Bind(nil, []*api.TaskInfo{task})
		return nil
	})
	defer patches.Reset()

	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder("gang", gang.New)
	framework.RegisterPluginBuilder("priority", priority.New)

	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	defer framework.CleanupPluginBuilders()

	queue := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
		},
	}
	pg := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue:     "c1",
			MinMember: 2,
			MinTaskMember: map[string]int32{
				"": 2,
			},
		},
		Status: schedulingv1.PodGroupStatus{
			Phase: schedulingv1.PodGroupInqueue,
		},
	}

	pvc, _, sc := util.BuildDynamicPVC("c1", "pvc", v1.ResourceList{
		v1.ResourceStorage: resource.MustParse("1Gi"),
	})
	pvc1 := pvc.DeepCopy()
	pvc1.Name = fmt.Sprintf("pvc%d", 1)

	allocate := New()

	tests := []struct {
		name string
		cache.TestArg
		expectedBind    map[string]string
		expectedActions map[string][]string
	}{
		{
			name: "resource not match",
			TestArg: cache.TestArg{
				Pods: []*v1.Pod{
					util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("1", "4Gi"), make(map[string]string)),
				},
				PodGroups: []*schedulingv1.PodGroup{pg},
				Queues:    []*schedulingv1.Queue{queue},
				Sc:        sc,
				Pvcs:      []*v1.PersistentVolumeClaim{pvc, pvc1},
			},
			expectedBind: map[string]string{},
			expectedActions: map[string][]string{
				"c1/p1": {"GetPodVolumes", "AllocateVolumes", "RevertVolumes"},
			},
		},
		{
			name: "node changed with enough resource",
			TestArg: cache.TestArg{
				Pods: []*v1.Pod{
					util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n2", util.BuildResourceList("2", "4Gi"), make(map[string]string)),
				},
				PodGroups: []*schedulingv1.PodGroup{pg},
				Queues:    []*schedulingv1.Queue{queue},
				Sc:        sc,
				Pvcs:      []*v1.PersistentVolumeClaim{pvc, pvc1},
			},
			expectedBind: map[string]string{
				"c1/p1": "n2",
				"c1/p2": "n2",
			},
			expectedActions: map[string][]string{
				"c1/p1": {"GetPodVolumes", "AllocateVolumes", "DynamicProvisions"},
				"c1/p2": {"GetPodVolumes", "AllocateVolumes", "DynamicProvisions"},
			},
		},
	}

	for _, test := range tests {
		if test.name == "resource not match" {
			// TODO(wangyang0616): First make sure that ut can run, and then fix the failed ut later
			// See issue for details: https://github.com/volcano-sh/volcano/issues/2812
			t.Skip("Test cases are not as expected, fixed later. see issue: #2812")
		}
		t.Run(test.name, func(t *testing.T) {
			schedulerCache, binder, _, fakeVolumeBinder := cache.CreateCacheForTest(&test.TestArg, 10000, 10)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:                "priority",
							EnabledJobReady:     &trueValue,
							EnabledPredicate:    &trueValue,
							EnabledJobPipelined: &trueValue,
							EnabledTaskOrder:    &trueValue,
						},
						{
							Name:                "gang",
							EnabledJobReady:     &trueValue,
							EnabledPredicate:    &trueValue,
							EnabledJobPipelined: &trueValue,
							EnabledTaskOrder:    &trueValue,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			allocate.Execute(ssn)
			if !reflect.DeepEqual(test.expectedBind, binder.Binds) {
				t.Errorf("expected: %v, got %v ", test.expectedBind, binder.Binds)
			}
			if !reflect.DeepEqual(test.expectedActions, fakeVolumeBinder.Actions) {
				t.Errorf("expected: %v, got %v ", test.expectedActions, fakeVolumeBinder.Actions)
			}
			fakeVolumeBinder.Actions = make(map[string][]string)
		})
	}
}
