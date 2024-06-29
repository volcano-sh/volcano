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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestNodeGroup(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{
		batch.QueueNameKey: "q1",
	}, make(map[string]string))

	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg2", map[string]string{
		batch.QueueNameKey: "q2",
	}, make(map[string]string))

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

	queue1 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q1",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
			Affinity: &schedulingv1.Affinity{
				NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1", "group3"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
				},
				NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2", "group4"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
				},
			},
		},
	}

	queue2 := &schedulingv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "q2",
		},
		Spec: schedulingv1.QueueSpec{
			Weight: 1,
			Affinity: &schedulingv1.Affinity{
				NodeGroupAffinity: &schedulingv1.NodeGroupAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group1"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group3"},
				},
				NodeGroupAntiAffinity: &schedulingv1.NodeGroupAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution:  []string{"group2"},
					PreferredDuringSchedulingIgnoredDuringExecution: []string{"group4"},
				},
			},
		},
	}

	tests := []struct {
		uthelper.TestCommonStruct
		arguments      framework.Arguments
		expected       map[string]map[string]float64
		expectedStatus map[string]map[string]int
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
