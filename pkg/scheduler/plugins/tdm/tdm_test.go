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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
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
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))

	p1.Annotations[schedulingv2.RevocableZone] = "*"
	p3.Annotations[schedulingv2.RevocableZone] = "*"

	n1 := util.BuildNode("n1", util.BuildResourceList("16", "64Gi"), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n2 := util.BuildNode("n2", util.BuildResourceList("16", "64Gi"), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n3 := util.BuildNode("n3", util.BuildResourceList("16", "64Gi"), map[string]string{})
	n4 := util.BuildNode("n4", util.BuildResourceList("16", "64Gi"), map[string]string{})

	n5 := util.BuildNode("n5", util.BuildResourceList("16", "64Gi"), map[string]string{
		schedulingv2.RevocableZone: "rz2",
	})

	pg1 := &schedulingv2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: schedulingv2.PodGroupSpec{
			Queue: "c1",
		},
	}

	queue1 := &schedulingv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c1",
		},
		Spec: schedulingv2.QueueSpec{
			Weight: 1,
		},
	}

	tests := []struct {
		name               string
		podGroups          []*schedulingv2.PodGroup
		pod                *v1.Pod
		nodes              []*v1.Node
		queues             []*schedulingv2.Queue
		predicatedExpected map[string]bool
		scoreExpected      map[string]map[string]float64
	}{
		{
			name: "preemptable task rz available",
			podGroups: []*schedulingv2.PodGroup{
				pg1,
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pod: p1,
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
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
			name: "not preemptable task",
			podGroups: []*schedulingv2.PodGroup{
				pg1,
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pod: p2,
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
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
			name: "preemptable task and multiple rz",
			podGroups: []*schedulingv2.PodGroup{
				pg1,
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pod: p3,
			nodes: []*v1.Node{
				n1, n2, n3, n4, n5,
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
		t.Run(fmt.Sprintf("case %v %v", i, test.name), func(t *testing.T) {
			binder := &util.FakeBinder{
				Binds:   map[string]string{},
				Channel: make(chan string),
			}
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				Binder:        binder,
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  &util.FakeVolumeBinder{},

				Recorder: record.NewFakeRecorder(100),
			}
			for _, node := range test.nodes {
				schedulerCache.AddNode(node)
			}

			schedulerCache.AddPod(test.pod)

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
			}, nil)

			defer framework.CloseSession(ssn)

			for _, job := range ssn.Jobs {
				for _, task := range job.Tasks {
					taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)

					predicatedNode := make([]*api.NodeInfo, 0)
					for _, node := range ssn.Nodes {
						if err := ssn.PredicateFn(task, node); err != nil {
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
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	p1 := util.BuildPod("c1", "p1", "n1", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("c1", "p2", "n1", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("c1", "p3", "n1", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p4 := util.BuildPod("c1", "p4", "n1", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p5 := util.BuildPod("c1", "p5", "n1", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	p6 := util.BuildPod("c2", "p6", "n2", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p7 := util.BuildPod("c2", "p7", "n2", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p8 := util.BuildPod("c2", "p8", "n2", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p9 := util.BuildPod("c2", "p9", "n2", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))
	p10 := util.BuildPod("c2", "p10", "n2", v1.PodRunning, util.BuildResourceList("1", "1Gi"), "pg2", make(map[string]string), make(map[string]string))

	p1.Annotations[schedulingv2.PodPreemptable] = "true"
	p2.Annotations[schedulingv2.PodPreemptable] = "true"
	p3.Annotations[schedulingv2.PodPreemptable] = "true"

	p6.Annotations[schedulingv2.PodPreemptable] = "true"
	p7.Annotations[schedulingv2.PodPreemptable] = "true"
	p8.Annotations[schedulingv2.PodPreemptable] = "true"
	p9.Annotations[schedulingv2.PodPreemptable] = "true"
	p10.Annotations[schedulingv2.PodPreemptable] = "true"

	n1 := util.BuildNode("n1", util.BuildResourceList("16", "64Gi"), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	n2 := util.BuildNode("n2", util.BuildResourceList("16", "64Gi"), map[string]string{
		schedulingv2.RevocableZone: "rz1",
	})

	queue1 := &schedulingv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c1",
		},
		Spec: schedulingv2.QueueSpec{
			Weight: 1,
		},
	}

	queue2 := &schedulingv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "c2",
		},
		Spec: schedulingv2.QueueSpec{
			Weight: 1,
		},
	}

	tests := []struct {
		podGroups []*schedulingv2.PodGroup
		pods      []*v1.Pod
		nodes     []*v1.Node
		queues    []*schedulingv2.Queue
		args      framework.Arguments
		want      int
	}{
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "30%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2, p3, p4, p5,
			},
			nodes: []*v1.Node{
				n1,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "100ms",
			},
			want: 2,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "30%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2, p3, p4, p5,
			},
			nodes: []*v1.Node{
				n1,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:00",
				"tdm.evict.period":       "100ms",
			},
			want: 0,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "99%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue1,
			},
			pods: []*v1.Pod{
				p1, p2, p3, p4, p5,
			},
			nodes: []*v1.Node{
				n1,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "50%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue2,
			},
			pods: []*v1.Pod{
				p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "50%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "90%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue1,
				queue2,
			},
			pods: []*v1.Pod{
				p1, p2, p3, p4, p5, p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n1, n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 6,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "3",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue2,
			},
			pods: []*v1.Pod{
				p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMinAvailable: "3",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue2,
			},
			pods: []*v1.Pod{
				p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 2,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMinAvailable: "30%",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue2,
			},
			pods: []*v1.Pod{
				p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 3,
		},
		{
			podGroups: []*schedulingv2.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c2",
						Annotations: map[string]string{
							schedulingv2.JDBMinAvailable: "2",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c2",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
						Annotations: map[string]string{
							schedulingv2.JDBMaxUnavailable: "3",
						},
					},
					Spec: schedulingv2.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			queues: []*schedulingv2.Queue{
				queue1,
				queue2,
			},
			pods: []*v1.Pod{
				p1, p2, p3, p4, p5, p6, p7, p8, p9, p10,
			},
			nodes: []*v1.Node{
				n1, n2,
			},
			args: framework.Arguments{
				"tdm.revocable-zone.rz1": "0:00-0:01",
				"tdm.evict.period":       "1s",
			},
			want: 6,
		},
	}

	for i, test := range tests {
		t.Run(fmt.Sprintf("case %v", i), func(t *testing.T) {
			binder := &util.FakeBinder{
				Binds:   map[string]string{},
				Channel: make(chan string),
			}
			schedulerCache := &cache.SchedulerCache{
				Nodes:         make(map[string]*api.NodeInfo),
				Jobs:          make(map[api.JobID]*api.JobInfo),
				Queues:        make(map[api.QueueID]*api.QueueInfo),
				Binder:        binder,
				StatusUpdater: &util.FakeStatusUpdater{},
				VolumeBinder:  &util.FakeVolumeBinder{},

				Recorder: record.NewFakeRecorder(100),
			}
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
							Name:          PluginName,
							EnabledVictim: &trueValue,
							Arguments:     test.args,
						},
					},
				},
			}, nil)

			defer framework.CloseSession(ssn)

			d, _ := time.ParseDuration(test.args[evictPeriodLabel].(string))
			time.Sleep(d)
			tasks := make([]*api.TaskInfo, 0)
			res := ssn.VictimTasks(tasks)
			if len(res) != test.want {
				t.Errorf("want %v, got %v", test.want, len(res))
				return
			}

		})
	}
}
