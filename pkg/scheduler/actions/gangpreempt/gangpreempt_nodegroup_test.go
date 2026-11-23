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

package gangpreempt

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodegroup"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	nodeGroupGuaranteed = "guaranteed"
	nodeGroupElastic    = "elastic"
)

func nodeGroupTiers() []conf.Tier {
	trueValue := true
	return []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
					EnabledJobReady:     &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:             nodegroup.PluginName,
					EnabledPredicate: &trueValue,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}
}

func nodeGroupPlugins() map[string]framework.PluginBuilder {
	return map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		nodegroup.PluginName:   nodegroup.New,
	}
}

func nodeGroupPriorityClasses() []*schedulingv1.PriorityClass {
	return []*schedulingv1.PriorityClass{
		util.BuildPriorityClass("high-priority", 1000),
		util.BuildPriorityClass("low-priority", 10),
	}
}

func buildNodeGroupNode(name, group, cpu string) *v1.Node {
	return util.BuildNode(name, api.BuildResourceList(cpu, "64Gi", []api.ScalarResource{{Name: "pods", Value: "110"}}...),
		map[string]string{schedulingv1beta1.NodeGroupNameKey: group})
}

func buildNodeGroupQueue(name string, required, preferred []string, limits string) *schedulingv1beta1.Queue {
	q := util.BuildQueueWithAnnos(name, 1, nil, map[string]string{
		schedulingv1beta1.NodeGroupResourceLimitsAnnotationKey: limits,
	})
	q.Spec.Affinity = &schedulingv1beta1.Affinity{
		NodeGroupAffinity: &schedulingv1beta1.NodeGroupAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  required,
			PreferredDuringSchedulingIgnoredDuringExecution: preferred,
		},
	}
	return q
}

func buildNodeGroupPod(name, nodeName string, phase v1.PodPhase, cpu, pg string) *v1.Pod {
	return util.BuildPod("c1", name, nodeName, phase, api.BuildResourceList(cpu, "1Gi"), pg, map[string]string{}, map[string]string{})
}

func runAllocateThenGangPreempt(t *testing.T, test *uthelper.TestCommonStruct) *framework.Session {
	t.Helper()
	test.Plugins = nodeGroupPlugins()
	test.PriClass = nodeGroupPriorityClasses()
	ssn := test.RegisterSession(nodeGroupTiers(), nil)
	t.Cleanup(test.Close)
	test.Run([]framework.Action{allocate.New(), New()})
	return ssn
}

// The queue has used up its nodegroup resource limit with low-priority tasks spread over
// nodes that still have enough physical room for the preemptor. Allocate records those nodes
// as UnschedulableAndUnresolvable ("node group resource limit exceeded"); evicting the
// low-priority gang frees the limit, so gangpreempt must not prune the nodes using that
// pre-eviction verdict.
//
// e4 is full with another queue's task, so allocate records it as merely Unschedulable. It keeps
// the pruned candidate list non-empty; otherwise intersectNodesByName falls back to all nodes
// and hides the problem.
func TestGangPreemptWithinNodeGroupResourceLimit(t *testing.T) {
	test := uthelper.TestCommonStruct{
		Name: "preempt same-queue gang when nodegroup limit is exhausted but nodes have room",
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-low", "c1", "q1", 3, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-high", "c1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			util.BuildPodGroupWithPrio("pg-other", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
		},
		Pods: []*v1.Pod{
			buildNodeGroupPod("other-8", "e4", v1.PodRunning, "8", "pg-other"),
			buildNodeGroupPod("low-6a", "e1", v1.PodRunning, "6", "pg-low"),
			buildNodeGroupPod("low-6b", "e2", v1.PodRunning, "6", "pg-low"),
			buildNodeGroupPod("low-4", "e3", v1.PodRunning, "4", "pg-low"),
			buildNodeGroupPod("high-0", "", v1.PodPending, "1", "pg-high"),
			buildNodeGroupPod("high-1", "", v1.PodPending, "1", "pg-high"),
		},
		Nodes: []*v1.Node{
			buildNodeGroupNode("e1", nodeGroupElastic, "8"),
			buildNodeGroupNode("e2", nodeGroupElastic, "8"),
			buildNodeGroupNode("e3", nodeGroupElastic, "8"),
			buildNodeGroupNode("e4", nodeGroupElastic, "8"),
		},
		Queues: []*schedulingv1beta1.Queue{
			buildNodeGroupQueue("q1", []string{nodeGroupElastic}, nil, `{"elastic":{"cpu":"16"}}`),
			buildNodeGroupQueue("q2", []string{nodeGroupElastic}, nil, `{}`),
		},
		ExpectEvicted:  []string{"c1/low-6a", "c1/low-6b", "c1/low-4"},
		ExpectEvictNum: 3,
	}
	runAllocateThenGangPreempt(t, &test)
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}

// Evicting a victim on a node group without a limit must not let the preemptor be placed on an
// idle node of a node group whose limit is still exhausted.
func TestGangPreemptDoesNotExceedNodeGroupResourceLimit(t *testing.T) {
	test := uthelper.TestCommonStruct{
		Name: "preemptor must not exceed the elastic limit after evicting a guaranteed-pool victim",
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-low-g", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-low-e", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
			util.BuildPodGroupWithPrio("pg-high", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
		},
		Pods: []*v1.Pod{
			buildNodeGroupPod("low-g", "g1", v1.PodRunning, "8", "pg-low-g"),
			buildNodeGroupPod("low-e", "e1", v1.PodRunning, "8", "pg-low-e"),
			buildNodeGroupPod("high-0", "", v1.PodPending, "8", "pg-high"),
		},
		Nodes: []*v1.Node{
			buildNodeGroupNode("g1", nodeGroupGuaranteed, "8"),
			buildNodeGroupNode("e1", nodeGroupElastic, "8"),
			buildNodeGroupNode("e2", nodeGroupElastic, "8"),
		},
		Queues: []*schedulingv1beta1.Queue{
			buildNodeGroupQueue("q1", []string{nodeGroupGuaranteed}, []string{nodeGroupGuaranteed, nodeGroupElastic}, `{"elastic":{"cpu":"8"}}`),
		},
	}
	ssn := runAllocateThenGangPreempt(t, &test)

	var preemptor *api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			if task.Name == "high-0" {
				preemptor = task
			}
		}
	}
	if preemptor == nil || preemptor.NodeName == "" {
		t.Fatalf("expected preemptor to be pipelined, got %+v", preemptor)
	}

	elasticUsed := 0.0
	for _, name := range []string{"e1", "e2"} {
		for _, task := range ssn.Nodes[name].Tasks {
			if api.AllocatedStatus(task.Status) || task.Status == api.Pipelined {
				elasticUsed += task.Resreq.MilliCPU
			}
		}
	}
	if elasticUsed > 8000 {
		t.Fatalf("elastic node group limit exceeded: preemptor on %s, elastic allocated %vm > 8000m", preemptor.NodeName, elasticUsed)
	}
}
