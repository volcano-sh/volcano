/*
Copyright 2021 The Volcano Authors.

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

package tdm

import (
	"fmt"
	"math"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"

	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	eps = 1e-8
)

func Test_parseRevocableZone(t *testing.T) {
	tests := []struct {
		rz    string
		delta int64
		err   bool
	}{
		{
			rz:    "00:00_01:00",
			delta: 0,
			err:   true,
		},
		{
			rz:    "00:00-01:00",
			delta: 60 * 60,
			err:   false,
		},
		{
			rz:    "0:00-23:59",
			delta: 23*60*60 + 59*60,
			err:   false,
		},
		{
			rz:    "0:00",
			delta: 0,
			err:   true,
		},
		{
			rz:    "1:00-0:00",
			delta: 23 * 60 * 60,
			err:   false,
		},
		{
			rz:    "1:0-0:0",
			delta: 0,
			err:   true,
		},
		{
			rz:    "   1:00-0:00    ",
			delta: 23 * 60 * 60,
			err:   false,
		},
		{
			rz:    "23:59-23:59",
			delta: 24 * 60 * 60,
			err:   false,
		},
		{
			rz:    "63:59-23:59",
			delta: 0,
			err:   true,
		},
	}

	for i, c := range tests {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			start, end, err := parseRevocableZone(c.rz)
			if (err != nil) != c.err {
				t.Errorf("want %v ,got %v, err: %v", c.err, err != nil, err)
			}

			if end.Unix()-start.Unix() != c.delta {
				t.Errorf("want %v, got %v", c.delta, end.Unix()-start.Unix())
			}

		})
	}
}

func Test_TDM(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))

	p1.Annotations[schedulingv2.RevocableZone] = "*"
	p3.Annotations[schedulingv2.RevocableZone] = "*"

	n1 := util.BuildNode("n1", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n2 := util.BuildNode("n2", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n3 := util.BuildNode("n3", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	n4 := util.BuildNode("n4", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})

	n5 := util.BuildNode("n5", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{
		schedulingv2.RevocableZone: "rz2",
	})

	pg1 := util.BuildPodGroup("pg1", "c1", "c1", 0, nil, "")
	queue1 := util.BuildQueue("c1", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		predicatedExpected map[string]bool
		scoreExpected      map[string]map[string]float64
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "preemptable task rz available",
				PodGroups: []*schedulingv2.PodGroup{
					pg1,
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{p1},
				Nodes: []*v1.Node{
					n1, n2, n3, n4, n5,
				},
			},
			predicatedExpected: map[string]bool{"n1": true, "n2": true, "n3": true, "n4": true},
			scoreExpected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 100,
					"n2": 100,
					"n3": 0,
					"n4": 0,
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "not preemptable task",
				PodGroups: []*schedulingv2.PodGroup{
					pg1,
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{p2},
				Nodes: []*v1.Node{
					n1, n2, n3, n4, n5,
				},
			},
			predicatedExpected: map[string]bool{"n3": true, "n4": true},
			scoreExpected: map[string]map[string]float64{
				"c1/p2": {
					"n3": 0,
					"n4": 0,
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "preemptable task and multiple rz",
				PodGroups: []*schedulingv2.PodGroup{
					pg1,
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{p3},
				Nodes: []*v1.Node{
					n1, n2, n3, n4, n5,
				},
			},
			predicatedExpected: map[string]bool{"n1": true, "n2": true, "n3": true, "n4": true},
			scoreExpected: map[string]map[string]float64{
				"c1/p3": {
					"n1": 100,
					"n2": 100,
					"n3": 0,
					"n4": 0,
				},
			},
		},
	}

	for i, test := range tests {
		test.Plugins = plugins
		t.Run(fmt.Sprintf("case %v %v", i, test.Name), func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							EnabledPredicate: &trueValue,
							Arguments: map[string]interface{}{
								"tdm.revocable-zone.rz1": "0:00-0:00",
								"tdm.revocable-zone.rz2": "0:00-0:01",
							},
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)

					predicatedNode := make([]*api.NodeInfo, 0)
					for _, node := range ssn.Nodes {
						err := ssn.PredicateFn(task, node)
						if err != nil {
							continue
						}
						predicatedNode = append(predicatedNode, node)
					}

					if len(predicatedNode) != len(test.predicatedExpected) {
						t.Errorf("want %v nodes,but got %v", len(test.predicatedExpected), len(predicatedNode))
					}

					for _, node := range predicatedNode {
						if !test.predicatedExpected[node.Name] {
							t.Errorf("want node: %v,but not found", node.Name)
						}
					}

					for _, node := range predicatedNode {
						score, err := ssn.NodeOrderFn(task, node)
						if err != nil {
							t.Errorf("task %s on node %s has err %v", taskID, node.Name, err)
							continue
						}
						if expectScore := test.scoreExpected[taskID][node.Name]; math.Abs(expectScore-score) > eps {
							t.Errorf("task %s on node %s expect have score %v, but get %v", taskID, node.Name, expectScore, score)
						}
					}
				}
			}
		})
	}
}
func Test_TDM_victimsFn(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	p1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("c1", "p3", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p4 := util.BuildPod("c1", "p4", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p5 := util.BuildPod("c1", "p5", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p6 := util.BuildPod("c2", "p6", "n2", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p7 := util.BuildPod("c2", "p7", "n2", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p8 := util.BuildPod("c2", "p8", "n2", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p9 := util.BuildPod("c2", "p9", "n2", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p10 := util.BuildPod("c2", "p10", "n2", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))

	p1.Annotations[schedulingv2.PodPreemptable] = "true"
	p2.Annotations[schedulingv2.PodPreemptable] = "true"
	p3.Annotations[schedulingv2.PodPreemptable] = "true"

	p4.Annotations[schedulingv2.PodPreemptable] = "false"
	p5.Annotations[schedulingv2.PodPreemptable] = "false"

	p6.Annotations[schedulingv2.PodPreemptable] = "true"
	p7.Annotations[schedulingv2.PodPreemptable] = "true"
	p8.Annotations[schedulingv2.PodPreemptable] = "true"
	p9.Annotations[schedulingv2.PodPreemptable] = "true"
	p10.Annotations[schedulingv2.PodPreemptable] = "true"

	n1 := util.BuildNode("n1", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n2 := util.BuildNode("n2", api.BuildResourceList("16", "64Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	queue1 := util.BuildQueue("c1", 1, nil)
	queue2 := util.BuildQueue("c2", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		args framework.Arguments
		want int
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg1", "c1", "c1", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "30%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{
					p1, p2, p3, p4, p5,
				},
				Nodes: []*v1.Node{
					n1,
				},
				ExpectEvictNum: 2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "100ms",
			},
			want: 2,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg1", "c1", "c1", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "30%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{
					p1, p2, p3, p4, p5,
				},
				Nodes: []*v1.Node{
					n1,
				},
				ExpectEvictNum: 0,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:00",
				"tdm.evict.period":       "100ms",
			},
			want: 0,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg1", "c1", "c1", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "99%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue1,
				},
				Pods: []*v1.Pod{
					p1, p2, p3, p4, p5,
				},
				Nodes: []*v1.Node{
					n1,
				},
				ExpectEvictNum: 3,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "50%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue2,
				},
				Pods: []*v1.Pod{
					p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n2,
				},
				ExpectEvictNum: 3,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "50%"}),
					util.BuildPodGroupWithAnno("pg1", "c1", "c1", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "90%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue1,
					queue2,
				},
				Pods: []*v1.Pod{
					p1, p2, p3, p4, p5, p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n1, n2,
				},
				ExpectEvictNum: 6,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 6,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "3"}),
				},
				Queues: []*schedulingv2.Queue{
					queue2,
				},
				Pods: []*v1.Pod{
					p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n2,
				},
				ExpectEvictNum: 3,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMinAvailable: "3"}),
				},
				Queues: []*schedulingv2.Queue{
					queue2,
				},
				Pods: []*v1.Pod{
					p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n2,
				},
				ExpectEvictNum: 2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 2,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMinAvailable: "30%"}),
				},
				Queues: []*schedulingv2.Queue{
					queue2,
				},
				Pods: []*v1.Pod{
					p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n2,
				},
				ExpectEvictNum: 3,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv2.PodGroup{
					util.BuildPodGroupWithAnno("pg2", "c2", "c2", 0, nil, "", map[string]string{schedulingv2.JDBMinAvailable: "2"}),
					util.BuildPodGroupWithAnno("pg1", "c1", "c1", 0, nil, "", map[string]string{schedulingv2.JDBMaxUnavailable: "3"}),
				},
				Queues: []*schedulingv2.Queue{
					queue1,
					queue2,
				},
				Pods: []*v1.Pod{
					p1, p2, p3, p4, p5, p6, p7, p8, p9, p10,
				},
				Nodes: []*v1.Node{
					n1, n2,
				},
				ExpectEvictNum: 6,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 6,
		},
	}

	for i, test := range tests {
		test.Plugins = plugins
		t.Run(fmt.Sprintf("case %v", i), func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:          PluginName,
							EnabledVictim: &trueValue,
							Arguments:     test.args,
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			d, _ := time.ParseDuration(test.args[evictPeriodLabel].(string))
			time.Sleep(d)
			tasks := make([]*api.TaskInfo, 0)
			res := ssn.VictimTasks(tasks)
			if len(res) != test.want {
				t.Errorf("case %d: want %v, got %v", i, test.want, len(res))
				return
			}
		})
	}
}
