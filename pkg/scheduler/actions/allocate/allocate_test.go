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
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	schedulingk8sv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/kube"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
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

	framework.RegisterPluginBuilder("drf", drf.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)

	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name      string
		podGroups []*schedulingv1.PodGroup
		pods      []*v1.Pod
		nodes     []*v1.Node
		queues    []*schedulingv1.Queue
		expected  map[string]string
	}{
		{
			name: "one Job with two Pods on one node",
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("2", "4Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight: 1,
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
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("2", "4G"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
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
			expected: map[string]string{
				"c2/pg2-p-1": "n1",
				"c1/pg1-p-1": "n1",
			},
		},
		{
			name: "high priority queue should not block others",
			podGroups: []*schedulingv1.PodGroup{
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

			pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("3", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under ns:c1/q:c2
				util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("2", "4G"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
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
			expected: map[string]string{
				"c1/p2": "n1",
			},
		},
	}

	allocate := New()

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
							Name:                  "drf",
							EnabledPreemptable:    &trueValue,
							EnabledJobOrder:       &trueValue,
							EnabledNamespaceOrder: &trueValue,
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
		name            string
		pods            []*v1.Pod
		nodes           []*v1.Node
		pvs             []*v1.PersistentVolume
		pvcs            []*v1.PersistentVolumeClaim
		sc              *storagev1.StorageClass
		expectedBind    map[string]string
		expectedActions map[string][]string
	}{
		{
			name: "resource not match",
			pods: []*v1.Pod{
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("1", "4Gi"), make(map[string]string)),
			},
			sc:           sc,
			pvcs:         []*v1.PersistentVolumeClaim{pvc, pvc1},
			expectedBind: map[string]string{},
			expectedActions: map[string][]string{
				"c1/p1": {"GetPodVolumes", "AllocateVolumes", "RevertVolumes"},
			},
		},
		{
			name: "node changed with enough resource",
			pods: []*v1.Pod{
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n2", util.BuildResourceList("2", "4Gi"), make(map[string]string)),
			},
			sc:   sc,
			pvcs: []*v1.PersistentVolumeClaim{pvc, pvc1},
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
		t.Run(test.name, func(t *testing.T) {
			kubeClient := fake.NewSimpleClientset()
			kubeClient.StorageV1().StorageClasses().Create(context.TODO(), test.sc, metav1.CreateOptions{})
			for _, pv := range test.pvs {
				kubeClient.CoreV1().PersistentVolumes().Create(context.TODO(), pv, metav1.CreateOptions{})
			}
			for _, pvc := range test.pvcs {
				kubeClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
			}

			fakeVolumeBinder := util.NewFakeVolumeBinder(kubeClient)
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
			schedulerCache.VolumeBinder = fakeVolumeBinder
			schedulerCache.Recorder = record.NewFakeRecorder(100)

			schedulerCache.AddQueueV1beta1(queue)
			schedulerCache.AddPodGroupV1beta1(pg)
			for i, pod := range test.pods {
				priority := int32(-i)
				pod.Spec.Priority = &priority
				schedulerCache.AddPod(pod)
			}
			for _, node := range test.nodes {
				schedulerCache.AddNode(node)
			}

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
			name:                  "no drf, two job in two empty and different priority class queue",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("2", "4Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c2",
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
				"pg2-p1",
				"pg1-p1",
			},
		},
		{
			name:                  "no drf, one pending job in high priority queue, two pending job in low priority queue",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("4", "8Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c2",
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
				"pg1-p1",
				"pg2-p1",
				"pg1-p2",
			},
		},
		{
			name:                  "no drf, priority is important than drf and proportion",
			priorityWeight:        1.2,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: false,
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p3", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p3", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("6", "12Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-cluster-critical",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c2",
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
					Value: 50,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-node-critical",
					},
					Value: 100,
				},
			},
			expected: []string{
				"pg2-p1",
				"pg2-p2",
				"pg1-p1",
				"pg2-p3",
				"pg1-p2",
				"pg1-p3",
			},
		},
		{
			name:                  "drf & proportion & priority",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: true,
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p3", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p3", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("10", "16Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/c1",
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
						Name: "c2",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/c2",
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
					Value: 50,
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "system-node-critical",
					},
					Value: 100,
				},
			},
			expected: []string{
				"pg2-p1",
				"pg1-p1",
				"pg2-p2",
				"pg1-p2",
				"pg2-p3",
				"pg1-p3",
			},
		},
		{
			name:                  "only one priority in kubernetes, the denominator is 0 in priority",
			priorityWeight:        1,
			drfWeight:             1,
			proportionWeight:      1,
			enableDrfHierarchical: true,
			podGroups: []*schedulingv1.PodGroup{
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
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg1-p1", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p2", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg1-p3", "", v1.PodPending, util.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p1", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p2", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "pg2-p3", "", v1.PodPending, util.BuildResourceList("1", "4Gi"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("10", "16Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/c1",
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
						Name: "c2",
						Annotations: map[string]string{
							"volcano.sh/hierarchy":         "root/c2",
							"volcano.sh/hierarchy-weights": "root/1",
						},
					},
					Spec: schedulingv1.QueueSpec{
						Weight:            1,
						PriorityClassName: "system-node-critical",
					},
				},
			},
			priorityClasss: []*schedulingk8sv1.PriorityClass{},
			expected: []string{
				"pg1-p1",
				"pg2-p1",
				"pg1-p2",
				"pg2-p2",
				"pg1-p3",
				"pg2-p3",
			},
		},
	}

	allocate := New()

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
					Name: "allocate",
					Arguments: map[string]interface{}{
						"QueueScoreOrderEnable":             true,
						"QueueScoreOrder.priority.weight":   test.priorityWeight,
						"QueueScoreOrder.drf.weight":        test.drfWeight,
						"QueueScoreOrder.proportion.weight": test.proportionWeight,
					},
				},
			})
			defer framework.CloseSession(ssn)

			allocate.Execute(ssn)

			if !reflect.DeepEqual(test.expected, binder.BindSlice) {
				t.Errorf("expected: %v, got %v ", test.expected, binder.BindSlice)
			}
		})
	}
}
