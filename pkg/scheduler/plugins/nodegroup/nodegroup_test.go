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

package nodegroup

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestNodeGroup(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", nil, nil)

	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg2", nil, nil)

	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg3", nil, nil)

	p4 := util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg4", nil, nil)

	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi"), map[string]string{
		NodeGroupNameKey: "group1",
	})
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group2",
	})
	n3 := util.BuildNode("n3", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group3",
	})
	n4 := util.BuildNode("n4", api.BuildResourceList("4", "16Gi"), map[string]string{
		NodeGroupNameKey: "group4",
	})
	n5 := util.BuildNode("n5", api.BuildResourceList("4", "16Gi"), make(map[string]string))

	pg1 := util.BuildPodGroup("pg1", "c1", "q1", 0, nil, "")
	pg2 := util.BuildPodGroup("pg2", "c1", "q2", 0, nil, "")
	pg3 := util.BuildPodGroup("pg3", "c1", "q1-child", 0, nil, "")
	pg4 := util.BuildPodGroup("pg4", "c1", "q-no-affinity-child", 0, nil, "")

	rootQ := util.MakeQueue("root").Affinity(&schedulingv1.Affinity{
		NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1", "group3"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
		},
		NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2", "group4"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
		},
	}).Obj()

	queue1 := util.MakeQueue("q1").Affinity(&schedulingv1.Affinity{
		NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1", "group3"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
		},
		NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2", "group4"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
		},
	}).Parent("root").Obj()

	queue2 := util.MakeQueue("q2").Affinity(&schedulingv1.Affinity{
		NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
		},
		NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2"},
			PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
		},
	}).Parent("root").Obj()

	//
	noAffinityRootQ := util.MakeQueue("root-no-affinity").Affinity(nil).Obj()

	noAffinityQ := util.MakeQueue("q-no-affinity").Affinity(nil).Parent("root").Obj()
	// q1-child's affinity is inherited from q1
	queue1Child := util.MakeQueue("q1-child").Affinity(nil).Parent("q1").Obj()
	// q-no-affinity-child's affinity is inherited from root
	noAffinityQChild := util.MakeQueue("q-no-affinity-child").Affinity(nil).Parent("q-no-affinity").Obj()

	tests := []struct {
		uthelper.TestCommonStruct
		arguments       framework.Arguments
		expected        map[string]map[string]float64
		expectedStatus  map[string]map[string]int
		enableHierarchy bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: soft constraints is subset of hard constraints",
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 100,
					"n2": 0.0,
					"n3": 150,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p1": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
		},
		{
			// test unnormal case
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: soft constraints is not subset of hard constraints",
				PodGroups: []*schedulingv1.PodGroup{pg2},
				Queues:    []*schedulingv1.Queue{queue2},
				Pods:      []*v1.Pod{p2},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p2": {
					"n1": 100,
					"n2": 0.0,
					"n3": 50,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p2": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
		},
		{
			// test unnormal case
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: soft constraints is not subset of hard constraints with strict=false",
				PodGroups: []*schedulingv1.PodGroup{pg2},
				Queues:    []*schedulingv1.Queue{noAffinityRootQ},
				Pods:      []*v1.Pod{p2},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{
				"strict": false,
			},
			expected: map[string]map[string]float64{
				"c1/p2": {
					"n1": 100,
					"n2": 0.0,
					"n3": 50,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p2": {
					"n1": api.UnschedulableAndUnresolvable,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.UnschedulableAndUnresolvable,
					"n4": api.UnschedulableAndUnresolvable,
					"n5": api.Success,
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: affinity inherits from parent queue",
				PodGroups: []*schedulingv1.PodGroup{pg3},
				Queues:    []*schedulingv1.Queue{rootQ, queue1, queue1Child},
				Pods:      []*v1.Pod{p3},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p3": {
					"n1": 100,
					"n2": 0.0,
					"n3": 150,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p3": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
			enableHierarchy: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: affinity inherits from ancestor queue if parent queue has no affinity",
				PodGroups: []*schedulingv1.PodGroup{pg4},
				Queues:    []*schedulingv1.Queue{rootQ, noAffinityQ, noAffinityQChild},
				Pods:      []*v1.Pod{p4},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p4": {
					"n1": 100,
					"n2": 0.0,
					"n3": 150,
					"n4": -1,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p4": {
					"n1": api.Success,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.Success,
					"n4": api.Success,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
			enableHierarchy: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "case: queue without affinity",
				PodGroups: []*schedulingv1.PodGroup{pg4},
				Queues:    []*schedulingv1.Queue{noAffinityQ},
				Pods:      []*v1.Pod{p4},
				Nodes:     []*v1.Node{n1, n2, n3, n4, n5},
				Plugins:   plugins,
			},
			arguments: framework.Arguments{},
			expected: map[string]map[string]float64{
				"c1/p4": {
					"n1": 0.0,
					"n2": 0.0,
					"n3": 0.0,
					"n4": 0.0,
					"n5": 0.0,
				},
			},
			expectedStatus: map[string]map[string]int{
				"c1/p4": {
					"n1": api.UnschedulableAndUnresolvable,
					"n2": api.UnschedulableAndUnresolvable,
					"n3": api.UnschedulableAndUnresolvable,
					"n4": api.UnschedulableAndUnresolvable,
					"n5": api.UnschedulableAndUnresolvable,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %v %v", i, test.Name), func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							EnabledPredicate: &trueValue,
							Arguments:        test.arguments,
							EnabledHierarchy: &test.enableHierarchy,
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)

					for _, node := range ssn.Nodes {
						score, err := ssn.NodeOrderFn(task, node)
						if err != nil {
							t.Errorf("case%d: task %s on node %s has err %v", i, taskID, node.Name, err)
							continue
						}
						if expectScore := test.expected[taskID][node.Name]; expectScore != score {
							t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i, taskID, node.Name, expectScore, score)
						}

						var code int
						err = ssn.PredicateFn(task, node)
						if err == nil {
							code = api.Success
						} else {
							code = err.(*api.FitError).Status[0].Code
						}
						if expectStatus := test.expectedStatus[taskID][node.Name]; expectStatus != code {
							t.Errorf("case%d: task %s on node %s expect have status code %v, but get %v", i, taskID, node.Name, expectStatus, code)
						}

					}
				}
			}
			t.Logf("nodegroup unit test finished ")
		})
	}

}
