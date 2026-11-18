/*
Copyright 2026 The Volcano Authors.

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

package numaaware

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/cpuset"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestNuma_OnSessionClose(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi"), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi"), make(map[string]string))

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue: "c1",
		},
	}
	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
		},
	}

	tests := []struct {
		name         string
		mockflag     string
		expectLenMap map[string]int
	}{
		{
			name:         "taskBindNodeMap is empty, no unassigned numa pods",
			mockflag:     "empty taskBindNodeMap",
			expectLenMap: map[string]int{"n1": 0, "n2": 0},
		},
		{
			name:         "assignRes missing taskID, skip adding unassigned numa pods",
			mockflag:     "assignRes missing taskID",
			expectLenMap: map[string]int{"n1": 0, "n2": 0},
		},
		{
			name:         "assignRes has taskID but missing nodeName, skip adding unassigned numa pods",
			mockflag:     "assignRes missing nodeName",
			expectLenMap: map[string]int{"n1": 0, "n2": 0},
		},
		{
			name:         "taskPodMetaMap missing taskID, skip adding unassigned numa pods",
			mockflag:     "taskPodMetaMap missing taskID",
			expectLenMap: map[string]int{"n1": 0, "n2": 0},
		},
		{
			name:         "normal case, successfully add unassigned numa pods",
			mockflag:     "normal",
			expectLenMap: map[string]int{"n1": 0, "n2": 1},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedulerCache := &cache.SchedulerCache{
				Nodes:          make(map[string]*api.NodeInfo),
				Jobs:           make(map[api.JobID]*api.JobInfo),
				Queues:         make(map[api.QueueID]*api.QueueInfo),
				StatusUpdater:  &util.FakeStatusUpdater{},
				HyperNodesInfo: api.NewHyperNodesInfo(nil),
				Recorder:       record.NewFakeRecorder(100),
			}

			schedulerCache.AddOrUpdateNode(n1)
			schedulerCache.AddOrUpdateNode(n2)
			schedulerCache.AddPod(p1)
			schedulerCache.AddPodGroupV1beta1(pg1)
			schedulerCache.AddQueueV1beta1(queue1)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledPredicate: &trueValue,
						},
					},
				},
			}, nil)

			plugin := New(nil).(*numaPlugin)
			plugin.nodeResSets = api.GenerateNodeResNumaSets(ssn.Nodes)
			taskID := api.TaskID(p1.UID)
			boundNodeName := "n2"
			podMeta := api.PodMeta{
				UID:       p1.UID,
				Name:      p1.Name,
				Namespace: p1.Namespace,
			}

			switch test.mockflag {
			case "empty taskBindNodeMap":
			case "assignRes missing taskID":
				plugin.taskBindNodeMap[taskID] = boundNodeName
			case "assignRes missing nodeName":
				plugin.taskBindNodeMap[taskID] = "nonexistent node"
				plugin.assignRes[taskID] = map[string]api.ResNumaSets{
					"n1": {
						string(v1.ResourceCPU): cpuset.New(0, 1),
					},
					"n2": {
						string(v1.ResourceCPU): cpuset.New(2, 3),
					},
				}
			case "taskPodMetaMap missing taskID":
				plugin.taskBindNodeMap[taskID] = boundNodeName
				plugin.assignRes[taskID] = map[string]api.ResNumaSets{
					"n1": {
						string(v1.ResourceCPU): cpuset.New(0, 1),
					},
					"n2": {
						string(v1.ResourceCPU): cpuset.New(2, 3),
					},
				}
			case "normal":
				plugin.taskBindNodeMap[taskID] = boundNodeName
				plugin.assignRes[taskID] = map[string]api.ResNumaSets{
					"n1": {
						string(v1.ResourceCPU): cpuset.New(0, 1),
					},
					"n2": {
						string(v1.ResourceCPU): cpuset.New(2, 3),
					},
				}
				plugin.taskPodMetaMap[taskID] = podMeta
			default:
				return
			}

			plugin.OnSessionClose(ssn)
			for _, node := range schedulerCache.Nodes {
				expectedLen := test.expectLenMap[node.Name]
				if len(node.UnassignedNumaPods) != expectedLen {
					t.Errorf("case %d: node %s unassigned numa pods = %d, but want %d", i, node.Name, len(node.UnassignedNumaPods), expectedLen)
				}
			}
			framework.CloseSession(ssn)
			return
		})
	}
}

// TestNuma_OnSessionClose_MultipleTasksAccumulate verifies that when multiple tasks
// are bound in taskBindNodeMap, OnSessionClose accumulates all valid ones onto the
// correct nodes, even when some tasks are skipped due to missing assignRes or
// missing podMeta. The mixed path is the easiest to regress during refactoring.
func TestNuma_OnSessionClose_MultipleTasksAccumulate(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	n1 := util.BuildNode("n1", api.BuildResourceList("4", "8Gi"), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi"), make(map[string]string))

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "c1"},
		Spec:       schedulingv1.PodGroupSpec{Queue: "c1"},
	}
	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "c1"},
		Spec:       schedulingv1.QueueSpec{Weight: 1},
	}

	schedulerCache := &cache.SchedulerCache{
		Nodes:          make(map[string]*api.NodeInfo),
		Jobs:           make(map[api.JobID]*api.JobInfo),
		Queues:         make(map[api.QueueID]*api.QueueInfo),
		StatusUpdater:  &util.FakeStatusUpdater{},
		HyperNodesInfo: api.NewHyperNodesInfo(nil),
		Recorder:       record.NewFakeRecorder(100),
	}
	schedulerCache.AddOrUpdateNode(n1)
	schedulerCache.AddOrUpdateNode(n2)
	schedulerCache.AddPod(p1)
	schedulerCache.AddPod(p2)
	schedulerCache.AddPod(p3)
	schedulerCache.AddPodGroupV1beta1(pg1)
	schedulerCache.AddQueueV1beta1(queue1)

	trueValue := true
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{Name: PluginName, EnabledPredicate: &trueValue},
			},
		},
	}, nil)

	plugin := New(nil).(*numaPlugin)
	plugin.nodeResSets = api.GenerateNodeResNumaSets(ssn.Nodes)

	t1 := api.TaskID(p1.UID)
	t2 := api.TaskID(p2.UID)
	t3 := api.TaskID(p3.UID)

	// p1: valid, bound to n2
	plugin.taskBindNodeMap[t1] = "n2"
	plugin.assignRes[t1] = map[string]api.ResNumaSets{
		"n2": {string(v1.ResourceCPU): cpuset.New(0, 1)},
	}
	plugin.taskPodMetaMap[t1] = api.PodMeta{UID: p1.UID, Name: p1.Name, Namespace: p1.Namespace}

	// p2: valid, bound to n1 (different node) to verify the loop writes to the right node
	plugin.taskBindNodeMap[t2] = "n1"
	plugin.assignRes[t2] = map[string]api.ResNumaSets{
		"n1": {string(v1.ResourceCPU): cpuset.New(2, 3)},
	}
	plugin.taskPodMetaMap[t2] = api.PodMeta{UID: p2.UID, Name: p2.Name, Namespace: p2.Namespace}

	// p3: skipped — present in taskBindNodeMap but missing podMeta (mixed skip path)
	plugin.taskBindNodeMap[t3] = "n1"
	plugin.assignRes[t3] = map[string]api.ResNumaSets{
		"n1": {string(v1.ResourceCPU): cpuset.New(4, 5)},
	}

	plugin.OnSessionClose(ssn)

	if got := len(schedulerCache.Nodes["n2"].UnassignedNumaPods); got != 1 {
		t.Errorf("n2 unassigned numa pods = %d, want 1 (only p1)", got)
	}
	if got := len(schedulerCache.Nodes["n1"].UnassignedNumaPods); got != 1 {
		t.Errorf("n1 unassigned numa pods = %d, want 1 (only p2; p3 skipped for missing podMeta)", got)
	}
	framework.CloseSession(ssn)
}

// TestNuma_OnSessionClose_VerifiesContent asserts the actual PodMeta key and CPUSet
// written to the cache match what was recorded in assignRes/taskPodMetaMap. The
// length-only assertion in the table test cannot catch a wrong-CPuset or wrong-key write.
func TestNuma_OnSessionClose_VerifiesContent(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi"), make(map[string]string))

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg1", Namespace: "c1"},
		Spec:       schedulingv1.PodGroupSpec{Queue: "c1"},
	}
	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "c1"},
		Spec:       schedulingv1.QueueSpec{Weight: 1},
	}

	schedulerCache := &cache.SchedulerCache{
		Nodes:          make(map[string]*api.NodeInfo),
		Jobs:           make(map[api.JobID]*api.JobInfo),
		Queues:         make(map[api.QueueID]*api.QueueInfo),
		StatusUpdater:  &util.FakeStatusUpdater{},
		HyperNodesInfo: api.NewHyperNodesInfo(nil),
		Recorder:       record.NewFakeRecorder(100),
	}
	schedulerCache.AddOrUpdateNode(n2)
	schedulerCache.AddPod(p1)
	schedulerCache.AddPodGroupV1beta1(pg1)
	schedulerCache.AddQueueV1beta1(queue1)

	trueValue := true
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{Name: PluginName, EnabledPredicate: &trueValue},
			},
		},
	}, nil)

	plugin := New(nil).(*numaPlugin)
	plugin.nodeResSets = api.GenerateNodeResNumaSets(ssn.Nodes)

	wantPodMeta := api.PodMeta{UID: p1.UID, Name: p1.Name, Namespace: p1.Namespace}
	wantCPUSet := cpuset.New(7, 8)

	taskID := api.TaskID(p1.UID)
	plugin.taskBindNodeMap[taskID] = "n2"
	plugin.assignRes[taskID] = map[string]api.ResNumaSets{
		"n2": {string(v1.ResourceCPU): wantCPUSet},
	}
	plugin.taskPodMetaMap[taskID] = wantPodMeta

	plugin.OnSessionClose(ssn)

	node2 := schedulerCache.Nodes["n2"]
	resSets, ok := node2.UnassignedNumaPods[wantPodMeta]
	if !ok {
		t.Fatalf("expected entry keyed by podMeta %v on n2, got %v", wantPodMeta, node2.UnassignedNumaPods)
	}
	got, ok := resSets[string(v1.ResourceCPU)]
	if !ok {
		t.Fatalf("expected CPUSet under %q in ResNumaSets, got %v", v1.ResourceCPU, resSets)
	}
	if !got.Equals(wantCPUSet) {
		t.Errorf("CPUSet mismatch: got %v, want %v", got, wantCPUSet)
	}
	framework.CloseSession(ssn)
}

// TestNuma_PredicateFn runs the numa-aware predicate through a scheduling session
// and checks how the task and node NUMA policies decide whether the node is
// rejected, skipped by the plugin, or gets NUMA resources reserved for the task.
func TestNuma_PredicateFn(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	var plugin *numaPlugin
	framework.RegisterPluginBuilder(PluginName, func(arguments framework.Arguments) framework.Plugin {
		plugin = New(arguments).(*numaPlugin)
		return plugin
	})
	defer framework.CleanupPluginBuilders()

	const (
		nodeName        = "n1"
		staticCPUMgr    = "static"
		noneCPUMgr      = "none"
		allNodeCPUs     = "0-7"
		noNodeCPUs      = ""
		nodeCPUCount    = 8
		cpusPerNUMANode = 4
	)

	// buildNumatopo describes a node with two NUMA nodes of four CPUs each.
	buildNumatopo := func(cpuManagerPolicy string, topologyPolicy batch.NumaPolicy, allocatableCPUs string) *nodeinfov1alpha1.Numatopology {
		cpuDetail := make(map[string]nodeinfov1alpha1.CPUInfo, nodeCPUCount)
		for cpuID := 0; cpuID < nodeCPUCount; cpuID++ {
			cpuDetail[strconv.Itoa(cpuID)] = nodeinfov1alpha1.CPUInfo{
				NUMANodeID: cpuID / cpusPerNUMANode,
				SocketID:   cpuID / cpusPerNUMANode,
				CoreID:     cpuID,
			}
		}
		return &nodeinfov1alpha1.Numatopology{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			Spec: nodeinfov1alpha1.NumatopoSpec{
				Policies: map[nodeinfov1alpha1.PolicyName]string{
					nodeinfov1alpha1.CPUManagerPolicy:      cpuManagerPolicy,
					nodeinfov1alpha1.TopologyManagerPolicy: string(topologyPolicy),
				},
				NumaResMap: map[string]nodeinfov1alpha1.ResourceInfo{
					string(v1.ResourceCPU): {Allocatable: allocatableCPUs, Capacity: nodeCPUCount},
				},
				CPUDetail: cpuDetail,
			},
		}
	}

	tests := []struct {
		name           string
		taskPolicy     batch.NumaPolicy
		cpuRequest     string
		guaranteed     bool
		numatopo       *nodeinfov1alpha1.Numatopology
		wantErr        string
		wantNumaAssign bool
	}{
		{
			name:       "non-guaranteed task is not checked by the plugin",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "2",
			guaranteed: false,
		},
		{
			name:       "task with policy is rejected on node without numa topology",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "2",
			guaranteed: true,
			wantErr:    "numa info is empty",
		},
		{
			name:       "task with policy is rejected when cpu manager policy is not static",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "2",
			guaranteed: true,
			numatopo:   buildNumatopo(noneCPUMgr, batch.SingleNumaNode, allNodeCPUs),
			wantErr:    "cpu manager policy isn't static",
		},
		{
			name:       "task with policy is rejected when node topology policy differs",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "2",
			guaranteed: true,
			numatopo:   buildNumatopo(staticCPUMgr, batch.BestEffort, allNodeCPUs),
			wantErr:    "task topology policy[single-numa-node] is different with node[best-effort]",
		},
		{
			name:       "task with policy is rejected when node has no allocatable cpus",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "2",
			guaranteed: true,
			numatopo:   buildNumatopo(staticCPUMgr, batch.SingleNumaNode, noNodeCPUs),
			wantErr:    "cpu allocatable map is empty",
		},
		{
			name:           "task with policy gets numa resources on matching node",
			taskPolicy:     batch.SingleNumaNode,
			cpuRequest:     "2",
			guaranteed:     true,
			numatopo:       buildNumatopo(staticCPUMgr, batch.SingleNumaNode, allNodeCPUs),
			wantNumaAssign: true,
		},
		{
			name:       "single-numa-node task is rejected when request spans numa nodes",
			taskPolicy: batch.SingleNumaNode,
			cpuRequest: "6",
			guaranteed: true,
			numatopo:   buildNumatopo(staticCPUMgr, batch.SingleNumaNode, allNodeCPUs),
			wantErr:    "cannot be assigned by numa",
		},
		{
			name:       "task without policy skips node without numa topology",
			cpuRequest: "2",
			guaranteed: true,
		},
		{
			name:       "task without policy skips node whose cpu manager policy is not static",
			cpuRequest: "2",
			guaranteed: true,
			numatopo:   buildNumatopo(noneCPUMgr, batch.SingleNumaNode, allNodeCPUs),
		},
		{
			name:       "task without policy skips node whose topology policy is none",
			cpuRequest: "2",
			guaranteed: true,
			numatopo:   buildNumatopo(staticCPUMgr, batch.None, allNodeCPUs),
		},
		{
			name:           "task without policy gets numa resources on node with topology policy",
			cpuRequest:     "2",
			guaranteed:     true,
			numatopo:       buildNumatopo(staticCPUMgr, batch.BestEffort, allNodeCPUs),
			wantNumaAssign: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resources := api.BuildResourceList(test.cpuRequest, "1Gi")
			pod := util.BuildPod("c1", "p1", "", v1.PodPending, resources, "pg1", make(map[string]string), make(map[string]string))
			if test.guaranteed {
				pod.Spec.Containers[0].Resources.Limits = resources
			}
			if test.taskPolicy != "" {
				pod.Annotations[schedulingv1.NumaPolicyKey] = string(test.taskPolicy)
			}

			schedulerCache := &cache.SchedulerCache{
				Nodes:          make(map[string]*api.NodeInfo),
				Jobs:           make(map[api.JobID]*api.JobInfo),
				Queues:         make(map[api.QueueID]*api.QueueInfo),
				StatusUpdater:  &util.FakeStatusUpdater{},
				HyperNodesInfo: api.NewHyperNodesInfo(nil),
				Recorder:       record.NewFakeRecorder(100),
			}
			schedulerCache.AddOrUpdateNode(util.BuildNode(nodeName, api.BuildResourceList("8", "16Gi"), make(map[string]string)))
			if test.numatopo != nil {
				schedulerCache.AddNumaInfoV1alpha1(test.numatopo)
			}
			schedulerCache.AddPod(pod)
			schedulerCache.AddPodGroupV1beta1(util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue))
			schedulerCache.AddQueueV1beta1(util.BuildQueue("c1", 1, nil))

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{Name: PluginName, EnabledPredicate: &trueValue},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			var task *api.TaskInfo
			for _, job := range ssn.Jobs {
				for _, jobTask := range job.Tasks {
					task = jobTask
				}
			}
			if task == nil {
				t.Fatalf("task for pod %s/%s not found in session", pod.Namespace, pod.Name)
			}

			err := ssn.PredicateFn(task, ssn.Nodes[nodeName])
			if test.wantErr == "" && err != nil {
				t.Errorf("unexpected predicate error: %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Errorf("predicate error = %v, want error containing %q", err, test.wantErr)
			}

			_, assigned := plugin.assignRes[task.UID][nodeName]
			if assigned != test.wantNumaAssign {
				t.Errorf("numa resources reserved on %s = %v, want %v", nodeName, assigned, test.wantNumaAssign)
			}
		})
	}
}
