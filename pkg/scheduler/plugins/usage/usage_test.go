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

package usage

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics/source"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	eps = 1e-8
)

type predicateResult struct {
	predicateStatus []*api.Status
	err             error
}

func buildNodeUsage(cpuAvgUsage map[string]float64, memAvgUsage map[string]float64, metricsTime time.Time) *api.NodeUsage {
	return &api.NodeUsage{
		MetricsTime: metricsTime,
		CPUUsageAvg: cpuAvgUsage,
		MEMUsageAvg: memAvgUsage,
	}
}

func updateNodeUsage(nodesInfo map[string]*api.NodeInfo, nodesUsage map[string]*api.NodeUsage) {
	for nodeName, nodeInfo := range nodesInfo {
		if nodeUsage, ok := nodesUsage[nodeName]; ok {
			nodeInfo.ResourceUsage = nodeUsage
		}
	}
}

func TestUsage_predicateFn(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n3 := util.BuildNode("n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n4 := util.BuildNode("n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n5 := util.BuildNode("n5", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))

	nodesUsage := make(map[string]*api.NodeUsage)
	timeNow := time.Now()
	// The CPU load of the node exceeds the upper limit.
	// The node cannot be scheduled.
	nodesUsage[n1.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 81}, map[string]float64{source.NODE_METRICS_PERIOD: 60}, timeNow)
	// The memory load of the node exceeds the upper limit.
	// The node cannot be scheduled.
	nodesUsage[n2.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 60}, map[string]float64{source.NODE_METRICS_PERIOD: 81}, timeNow)
	// The CPU usage and memory usage do not exceed the upper limit.
	// The node can be scheduled.
	nodesUsage[n3.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 80}, map[string]float64{source.NODE_METRICS_PERIOD: 79}, timeNow)
	// The memory and memory load of the node exceeds the upper limit.
	// However, the metrics are not updated in the latest 5 minutes, and the usage function is invalid.
	// The node can schedule pods.
	nodesUsage[n4.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 90}, map[string]float64{source.NODE_METRICS_PERIOD: 81}, timeNow.Add(-6*time.Minute))
	// The memory and memory load of the node exceeds the upper limit.
	// However, the metric time is in the initial state, and the usage function is invalid.
	// The node can schedule pods.
	nodesUsage[n5.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 90}, map[string]float64{source.NODE_METRICS_PERIOD: 81}, time.Time{})

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue: "q1",
		},
	}
	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
		},
	}

	tests := []struct {
		name          string
		podGroups     []*schedulingv1.PodGroup
		queues        []*schedulingv1.Queue
		pods          []*v1.Pod
		nodes         []*v1.Node
		nodesUsageMap map[string]*api.NodeUsage
		arguments     framework.Arguments
		expected      predicateResult
	}{
		{
			name: "The node cannot be scheduled, because of the CPU load of the node exceeds the upper limit.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2,
			},
			nodes: []*v1.Node{
				n1,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: predicateResult{
				predicateStatus: []*api.Status{
					{
						Code:   api.UnschedulableAndUnresolvable,
						Reason: NodeUsageCPUExtend,
					},
				},
				err: fmt.Errorf("plugin %s predicates failed, because of %s", PluginName, NodeUsageCPUExtend),
			},
		},
		{
			name: "The node cannot be scheduled, because of the memory load of the node exceeds the upper limit.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2,
			},
			nodes: []*v1.Node{
				n2,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: predicateResult{
				predicateStatus: []*api.Status{
					{
						Code:   api.UnschedulableAndUnresolvable,
						Reason: NodeUsageMemoryExtend,
					},
				},
				err: fmt.Errorf("plugin %s predicates failed, because of %s", PluginName, NodeUsageMemoryExtend),
			},
		},
		{
			name: "The node can be scheduled, because of the CPU usage and memory usage do not exceed the upper limit.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2,
			},
			nodes: []*v1.Node{
				n3,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: predicateResult{
				predicateStatus: []*api.Status{
					{
						Code:   api.Success,
						Reason: "",
					},
				},
				err: nil,
			},
		},
		{
			name: "The node can be scheduled, because of the metrics are not updated in the latest 5 minutes, and the usage function is invalid.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2,
			},
			nodes: []*v1.Node{
				n4,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: predicateResult{
				predicateStatus: []*api.Status{
					{
						Code:   api.Success,
						Reason: "",
					},
				},
				err: nil,
			},
		},
		{
			name: "The node can be scheduled, because of the metric time is in the initial state, and the usage function is invalid.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2,
			},
			nodes: []*v1.Node{
				n5,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: predicateResult{
				predicateStatus: []*api.Status{
					{
						Code:   api.Success,
						Reason: "",
					},
				},
				err: nil,
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  &util.FakeVolumeBinder{},

				Recorder: record.NewFakeRecorder(100),
			}

			for _, node := range test.nodes {
				schedulerCache.AddOrUpdateNode(node)
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
			updateNodeUsage(schedulerCache.Nodes, nodesUsage)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledPredicate: &trueValue,
							Arguments:        test.arguments,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
					for _, node := range ssn.Nodes {
						err := ssn.PredicateFn(task, node)
						if (test.expected.err == nil || err == nil) && test.expected.err != err {
							t.Errorf("case%d: task %s on node %s has error, expect: %v, actual: %v",
								i, taskID, node.Name, test.expected.err, err)
							continue
						}
						if test.expected.err != nil && test.expected.err.Error() != err.Error() {
							t.Errorf("case%d: task %s on node %s has error, expect: %v, actual: %v",
								i, taskID, node.Name, test.expected.err, err)
							continue
						}
						predicateStatus := err.(*api.FitError).Status
						for index := range predicateStatus {
							if predicateStatus[index].Code != test.expected.predicateStatus[index].Code ||
								predicateStatus[index].Reason != test.expected.predicateStatus[index].Reason {
								t.Errorf("case%d: task %s on node %s has error, expect: %v, actual: %v",
									i, taskID, node.Name, test.expected.predicateStatus[index], predicateStatus[index])
								continue
							}
						}
					}
				}
			}
		})
	}
}

func TestUsage_nodeOrderFn(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n3 := util.BuildNode("n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n4 := util.BuildNode("n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n5 := util.BuildNode("n5", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))

	nodesUsage := make(map[string]*api.NodeUsage)
	timeNow := time.Now()
	nodesUsage[n1.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 30}, map[string]float64{source.NODE_METRICS_PERIOD: 50}, timeNow)
	nodesUsage[n2.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 60}, map[string]float64{source.NODE_METRICS_PERIOD: 50}, timeNow)
	nodesUsage[n3.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 60}, map[string]float64{source.NODE_METRICS_PERIOD: 80}, timeNow)
	// The metrics are not updated in the latest 5 minutes, and the usage function is invalid.
	// The node score is 0.
	nodesUsage[n4.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 10}, map[string]float64{source.NODE_METRICS_PERIOD: 20}, timeNow.Add(-6*time.Minute))
	// The metric time is in the initial state, and the usage function is invalid.
	// The node score is 0.
	nodesUsage[n5.Name] = buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 0}, map[string]float64{source.NODE_METRICS_PERIOD: 0}, time.Time{})

	pg1 := &schedulingv1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv1.PodGroupSpec{
			Queue: "q1",
		},
	}
	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
		},
	}

	tests := []struct {
		name          string
		podGroups     []*schedulingv1.PodGroup
		queues        []*schedulingv1.Queue
		pods          []*v1.Pod
		nodes         []*v1.Node
		nodesUsageMap map[string]*api.NodeUsage
		arguments     framework.Arguments
		expected      map[string]map[string]float64
	}{
		{
			name: "Node scoring in the default weight configuration scenario.",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1,
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    1,
				"memory.weight": 1,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 300,
					"n2": 225,
					"n3": 150,
					"n4": 0,
					"n5": 0,
				},
			},
		},
		{
			name: "Node scoring gives priority to memory resources",
			podGroups: []*schedulingv1.PodGroup{
				pg1,
			},
			queues: []*schedulingv1.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1,
			},
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
			},
			nodesUsageMap: nodesUsage,
			arguments: framework.Arguments{
				"usage.weight":  5,
				"cpu.weight":    2,
				"memory.weight": 8,
				"thresholds": map[interface{}]interface{}{
					"cpu": 80,
					"mem": 80,
				},
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 270,
					"n2": 240,
					"n3": 120,
					"n4": 0,
					"n5": 0,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  &util.FakeVolumeBinder{},

				Recorder: record.NewFakeRecorder(100),
			}

			for _, node := range test.nodes {
				schedulerCache.AddOrUpdateNode(node)
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
			updateNodeUsage(schedulerCache.Nodes, nodesUsage)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							Arguments:        test.arguments,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
					for _, node := range ssn.Nodes {
						score, err := ssn.NodeOrderFn(task, node)
						if err != nil {
							t.Errorf("case%d: task %s on node %s has err %v", i, taskID, node.Name, err)
							continue
						}
						if expectScore := test.expected[taskID][node.Name]; math.Abs(expectScore-score) > eps {
							t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i, taskID, node.Name, expectScore, score)
						}
					}
				}
			}
		})
	}
}
