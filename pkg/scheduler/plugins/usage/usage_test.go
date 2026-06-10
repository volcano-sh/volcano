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
	"strings"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics/source"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
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

func TestUsageEstimatorConfig(t *testing.T) {
	defaultPlugin := New(framework.Arguments{}).(*usagePlugin)
	if math.Abs(defaultPlugin.requestWeight-1.0) > eps {
		t.Errorf("default requestWeight = %v, expected 1.0", defaultPlugin.requestWeight)
	}
	if math.Abs(defaultPlugin.burstWeight-0.0) > eps {
		t.Errorf("default burstWeight = %v, expected 0.0", defaultPlugin.burstWeight)
	}
	if math.Abs(defaultPlugin.riskThreshold-0.6) > eps {
		t.Errorf("default riskThreshold = %v, expected 0.6", defaultPlugin.riskThreshold)
	}
	if math.Abs(defaultPlugin.riskFactor-1.2) > eps {
		t.Errorf("default riskFactor = %v, expected 1.2", defaultPlugin.riskFactor)
	}
	if math.Abs(defaultPlugin.beCPU-250) > eps {
		t.Errorf("default beCPU = %v, expected 250", defaultPlugin.beCPU)
	}
	if math.Abs(defaultPlugin.beMemory-float64(200*1024*1024)) > eps {
		t.Errorf("default beMemory = %v, expected 200Mi", defaultPlugin.beMemory)
	}

	configuredPlugin := New(framework.Arguments{
		"request.weight": 0.7,
		"burst.weight":   0.3,
		"risk_threshold": 0.75,
		"risk_factor":    1.5,
		"be.cpu":         "500m",
		"be.memory":      "300mi",
	}).(*usagePlugin)
	if math.Abs(configuredPlugin.requestWeight-0.7) > eps {
		t.Errorf("configured requestWeight = %v, expected 0.7", configuredPlugin.requestWeight)
	}
	if math.Abs(configuredPlugin.burstWeight-0.3) > eps {
		t.Errorf("configured burstWeight = %v, expected 0.3", configuredPlugin.burstWeight)
	}
	if math.Abs(configuredPlugin.riskThreshold-0.75) > eps {
		t.Errorf("configured riskThreshold = %v, expected 0.75", configuredPlugin.riskThreshold)
	}
	if math.Abs(configuredPlugin.riskFactor-1.5) > eps {
		t.Errorf("configured riskFactor = %v, expected 1.5", configuredPlugin.riskFactor)
	}
	if math.Abs(configuredPlugin.beCPU-500) > eps {
		t.Errorf("configured beCPU = %v, expected 500", configuredPlugin.beCPU)
	}
	if math.Abs(configuredPlugin.beMemory-float64(300*1024*1024)) > eps {
		t.Errorf("configured beMemory = %v, expected 300Mi", configuredPlugin.beMemory)
	}

	invalidPlugin := New(framework.Arguments{
		"request.weight": 1.5,
		"burst.weight":   -0.1,
		"risk_threshold": 2.0,
		"risk_factor":    0.5,
		"be.cpu":         "-1",
		"be.memory":      "bad",
	}).(*usagePlugin)
	if math.Abs(invalidPlugin.requestWeight-1.0) > eps {
		t.Errorf("invalid requestWeight should keep default, got %v", invalidPlugin.requestWeight)
	}
	if math.Abs(invalidPlugin.burstWeight-0.0) > eps {
		t.Errorf("invalid burstWeight should keep default, got %v", invalidPlugin.burstWeight)
	}
	if math.Abs(invalidPlugin.riskThreshold-0.6) > eps {
		t.Errorf("invalid riskThreshold should keep default, got %v", invalidPlugin.riskThreshold)
	}

	legacyPlugin := New(framework.Arguments{
		"threshold": 0.75,
	}).(*usagePlugin)
	if math.Abs(legacyPlugin.riskThreshold-0.6) > eps {
		t.Errorf("legacy threshold should not configure riskThreshold, got %v", legacyPlugin.riskThreshold)
	}
	if math.Abs(invalidPlugin.riskFactor-1.2) > eps {
		t.Errorf("invalid riskFactor should keep default, got %v", invalidPlugin.riskFactor)
	}
	if math.Abs(invalidPlugin.beCPU-250) > eps {
		t.Errorf("invalid beCPU should keep default, got %v", invalidPlugin.beCPU)
	}
	if math.Abs(invalidPlugin.beMemory-float64(200*1024*1024)) > eps {
		t.Errorf("invalid beMemory should keep default, got %v", invalidPlugin.beMemory)
	}
}

func TestUsage_predicateFn(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

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

	pg1 := util.BuildPodGroup("pg1", "c1", "q1", 0, nil, "")

	queue1 := util.BuildQueue("q1", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		nodesUsageMap map[string]*api.NodeUsage
		arguments     framework.Arguments
		expected      predicateResult
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "The node cannot be scheduled, because of the CPU load of the node exceeds the upper limit.",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2},
				Nodes:     []*v1.Node{n1},
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
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "The node cannot be scheduled, because of the memory load of the node exceeds the upper limit.",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2},
				Nodes:     []*v1.Node{n2},
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
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "The node can be scheduled, because of the CPU usage and memory usage do not exceed the upper limit.",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2},
				Nodes:     []*v1.Node{n3},
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
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "The node can be scheduled, because of the metrics are not updated in the latest 5 minutes, and the usage function is invalid.",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2},
				Nodes:     []*v1.Node{n4},
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
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "The node can be scheduled, because of the metric time is in the initial state, and the usage function is invalid.",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2},
				Nodes:     []*v1.Node{n5},
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
		t.Run(test.Name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledPredicate: &trueValue,
							Arguments:        test.arguments,
						},
					},
				},
			}
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			updateNodeUsage(ssn.Nodes, nodesUsage)

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
						if err == nil {
							continue
						}

						fitErr := err.(*api.FitError)
						predicateStatus := fitErr.Status

						errString := func(fe *api.FitError) string {
							if len(fe.Status) > 0 {
								return fmt.Sprintf("plugin %s predicates failed, because of %s", fe.Status[0].Plugin, strings.Join(fe.Reasons(), ", "))
							}
							return strings.Join(fe.Reasons(), ", ")
						}

						errStr := errString(fitErr)
						if test.expected.err != nil && test.expected.err.Error() != errStr {
							t.Errorf("case%d: task %s on node %s has error:\nexpect: %v\nactual: %v",
								i, taskID, node.Name, test.expected.err, errStr)
							continue
						}

						for index := range predicateStatus {
							if predicateStatus[index].Code != test.expected.predicateStatus[index].Code ||
								predicateStatus[index].Reason != test.expected.predicateStatus[index].Reason {
								t.Errorf("case%d: task %s on node %s has error:\nexpect: %v\nactual: %v",
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
	plugins := map[string]framework.PluginBuilder{PluginName: New}

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

	pg1 := util.BuildPodGroup("pg1", "c1", "q1", 0, nil, "")

	queue1 := util.BuildQueue("q1", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		nodesUsageMap map[string]*api.NodeUsage
		arguments     framework.Arguments
		expected      map[string]map[string]float64
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "Node scoring in the default weight configuration scenario.",
				PodGroups: []*schedulingv1.PodGroup{
					pg1,
				},
				Queues: []*schedulingv1.Queue{
					queue1,
				},
				Pods: []*v1.Pod{
					p1,
				},
				Nodes: []*v1.Node{
					n1, n2, n3, n4, n5,
				},
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
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "Node scoring gives priority to memory resources",
				PodGroups: []*schedulingv1.PodGroup{
					pg1,
				},
				Queues: []*schedulingv1.Queue{
					queue1,
				},
				Pods: []*v1.Pod{
					p1,
				},
				Nodes: []*v1.Node{
					n1, n2, n3, n4, n5,
				},
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
		t.Run(test.Name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							Arguments:        test.arguments,
						},
					},
				},
			}
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			updateNodeUsage(ssn.Nodes, nodesUsage)

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

func TestUsage_prioritizeNodesDoesNotDoubleCountNodeOrder(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	n1 := util.BuildNode("n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	pg1 := util.BuildPodGroup("pg1", "c1", "q1", 0, nil, "")
	queue1 := util.BuildQueue("q1", 1, nil)

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: framework.Arguments{
						"usage.weight":  5,
						"cpu.weight":    1,
						"memory.weight": 1,
					},
				},
			},
		},
	}

	test := uthelper.TestCommonStruct{
		Name:      "Usage score should be counted once in PrioritizeNodes.",
		Plugins:   plugins,
		PodGroups: []*schedulingv1.PodGroup{pg1},
		Queues:    []*schedulingv1.Queue{queue1},
		Pods:      []*v1.Pod{p1},
		Nodes:     []*v1.Node{n1, n2},
	}
	ssn := test.RegisterSession(tiers, nil)
	defer test.Close()

	updateNodeUsage(ssn.Nodes, map[string]*api.NodeUsage{
		n1.Name: buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 30}, map[string]float64{source.NODE_METRICS_PERIOD: 50}, time.Now()),
		n2.Name: buildNodeUsage(map[string]float64{source.NODE_METRICS_PERIOD: 60}, map[string]float64{source.NODE_METRICS_PERIOD: 50}, time.Now()),
	})

	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			nodeScores := util.PrioritizeNodes(task, []*api.NodeInfo{ssn.Nodes[n1.Name], ssn.Nodes[n2.Name]}, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
			scoreByNode := map[string]float64{}
			for score, nodes := range nodeScores {
				for _, node := range nodes {
					scoreByNode[node.Name] = score
				}
			}

			expected := map[string]float64{
				n1.Name: 300,
				n2.Name: 225,
			}
			for nodeName, expectedScore := range expected {
				if math.Abs(scoreByNode[nodeName]-expectedScore) > eps {
					t.Errorf("PrioritizeNodes score for %s = %v, expected %v", nodeName, scoreByNode[nodeName], expectedScore)
				}
			}
		}
	}
}

func TestShouldAddToShadowCache(t *testing.T) {
	recentStart := metav1.NewTime(time.Now().Add(-time.Minute))
	oldStart := metav1.NewTime(time.Now().Add(-10 * time.Minute))

	tests := []struct {
		name     string
		task     *api.TaskInfo
		expected bool
	}{
		{
			name: "pending without node is not tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status: api.Pending,
				},
			},
			expected: false,
		},
		{
			name: "pending with node is not treated as allocated",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Pending,
					NodeName: "node1",
				},
			},
			expected: false,
		},
		{
			name: "allocated task is tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Allocated,
					NodeName: "node1",
				},
			},
			expected: true,
		},
		{
			name: "binding task is tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Binding,
					NodeName: "node1",
				},
			},
			expected: true,
		},
		{
			name: "bound task is tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Bound,
					NodeName: "node1",
				},
			},
			expected: true,
		},
		{
			name: "recent running task is tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Running,
					NodeName: "node1",
				},
				Pod: &v1.Pod{
					Status: v1.PodStatus{StartTime: &recentStart},
				},
			},
			expected: true,
		},
		{
			name: "old running task is not tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Running,
					NodeName: "node1",
				},
				Pod: &v1.Pod{
					Status: v1.PodStatus{StartTime: &oldStart},
				},
			},
			expected: false,
		},
		{
			name: "completed task is not tracked",
			task: &api.TaskInfo{
				TransactionContext: api.TransactionContext{
					Status:   api.Succeeded,
					NodeName: "node1",
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldAddToShadowCache(tt.task, 5*time.Minute)
			if got != tt.expected {
				t.Errorf("shouldAddToShadowCache() = %v, expected %v", got, tt.expected)
			}
		})
	}
}
