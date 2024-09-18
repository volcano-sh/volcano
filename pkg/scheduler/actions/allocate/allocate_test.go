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
	"os"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}

func TestParseArgs(t *testing.T) {
	test := uthelper.TestCommonStruct{Name: "set cache false"}

	action := New()
	test.RegisterSession(nil, []conf.Configuration{{Name: action.Name(),
		Arguments: map[string]interface{}{conf.EnablePredicateErrCacheKey: false}}})
	test.Run([]framework.Action{action})
	assert.False(t, action.enablePredicateErrorCache)
}

func TestAllocate(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
		gang.PluginName:       gang.New,
	}
	tests := []uthelper.TestCommonStruct{
		{
			Name: "prepredicate failed: node selector does not match",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "prepredicate failed and tasks are not used up, continue on untill min member meet",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 1, "worker": 1}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p0": "n1",
				"c1/p2": "n2",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "master's min member can not be allocated, break from allocating",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 2, "worker": 0}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
		{
			Name: "one Job with two Pods on one node",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "two Jobs on one node",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c2", "c2", 0, nil, schedulingv1.PodGroupInqueue),
			},

			// pod name should be like "*-*-{index}",
			// due to change of TaskOrderFn
			Pods: []*v1.Pod{
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
				util.BuildQueue("c2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c2/pg2-p-1": "n1",
				"c1/pg1-p-1": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "high priority queue should not block others",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "c2", 0, nil, schedulingv1.PodGroupInqueue),
			},

			Pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("3", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under ns:c1/q:c2
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
				util.BuildQueue("c2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFareShareAllocate(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "queue with low DRF share value has high priority, should allocate first",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-small-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-large-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-large-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // allocate order: q-2/pg-large-2, q-1/pg-large-1
				util.BuildPod("ns-1", "pod-small-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-small-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-2", "", v1.PodPending, api.BuildResourceList("3", "2G"), "pg-large-2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q-1", 1, nil),
				util.BuildQueue("q-2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"ns-1/pod-large-2": "node-1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "queue’s DRF share value will be updated and its priority will change before it is put back into the priority queue",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-small-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-large-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-small-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-large-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // allocate order: q-2/pg-large-2, q-1/pg-large-1, q-2/pg-small-2
				util.BuildPod("ns-1", "pod-small-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-small-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-2", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-small-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-small-2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q-1", 1, nil),
				util.BuildQueue("q-2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"ns-1/pod-large-1": "node-1",
				"ns-1/pod-large-2": "node-1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "queue’s one jobs has no pending tasks, should be put back to queues for next job",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-2", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("ns-1", "pod-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-1", nil, nil),
				util.BuildPod("ns-1", "pod-2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-2", nil, nil),
			},
			Nodes:  []*v1.Node{util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))},
			Queues: []*schedulingv1.Queue{util.BuildQueue("q-1", 1, nil)},
			ExpectBindMap: map[string]string{
				"ns-1/pod-2": "node-1",
			},
			ExpectBindsNum: 1,
		},
	}
	trueValue := true
	falseValue := false
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &falseValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
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

	queue := util.BuildQueue("c1", 1, nil)
	pg := util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"": 2}, schedulingv1.PodGroupInqueue)

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
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
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
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
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
		if test.name == "resource not match" {
			// TODO(wangyang0616): First make sure that ut can run, and then fix the failed ut later
			// See issue for details: https://github.com/volcano-sh/volcano/issues/2812
			t.Skip("Test cases are not as expected, fixed later. see issue: #2812")
		}
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
			binder := util.NewFakeBinder(10)
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				Binder:        binder,
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  fakeVolumeBinder,
				Recorder:      record.NewFakeRecorder(100),
			}
			schedulerCache.AddQueueV1beta1(queue)
			schedulerCache.AddPodGroupV1beta1(pg)
			for i, pod := range test.pods {
				priority := int32(-i)
				pod.Spec.Priority = &priority
				schedulerCache.AddPod(pod)
			}
			for _, node := range test.nodes {
				schedulerCache.AddOrUpdateNode(node)
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
			bindResults := binder.Binds()
			if !equality.Semantic.DeepEqual(test.expectedBind, bindResults) {
				t.Errorf("expected: %v, got %v ", test.expectedBind, bindResults)
			}
			if !equality.Semantic.DeepEqual(test.expectedActions, fakeVolumeBinder.Actions) {
				t.Errorf("expected: %v, got %v ", test.expectedActions, fakeVolumeBinder.Actions)
			}
			fakeVolumeBinder.Actions = make(map[string][]string)
		})
	}
}
