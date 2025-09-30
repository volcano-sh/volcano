/*
Copyright(C)2020-2023. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package module910bx8 is using for HuaWei Ascend pin affinity schedule.
*/
package module910x8

import (
	"errors"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	itest "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

// TestNew
func TestNew(t *testing.T) {
	t.Run("test New", func(t *testing.T) {
		npu := New(SchedulerName)
		if npu.GetPluginName() != SchedulerName {
			t.Errorf("New() npu Name: %s, wantName: %s.", npu.GetPluginName(), SchedulerName)
		}
		if npu.GetAnnoName() != util.NPU910CardName {
			t.Errorf("New() npu annoName: %s, wantAnnoName: %s.", npu.GetPluginName(), util.NPU910CardName)
		}
		if npu.GetAnnoPreVal() != util.NPU910CardNamePre {
			t.Errorf("New() npu annoNamePre: %s, wantAnnoNamePre: %s.",
				npu.GetPluginName(), util.NPU910CardNamePre)
		}
	})
}

// checkNodeNPUByTaskTestCase CheckNodeNPUByTask test case
type checkNodeNPUByTaskTestCase struct {
	Task    *api.TaskInfo
	Name    string
	Attr    util.SchedulerJobAttr
	Node    plugin.NPUNode
	WantErr error
}

func buildCheckNodeNPUByTaskTestCases01() []checkNodeNPUByTaskTestCase {
	return []checkNodeNPUByTaskTestCase{
		{
			Name: "01-CheckNodeNPUByTask return nil when node npu meet task req",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name: "node1",
					Annotation: map[string]string{
						util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3",
						networkUnhealthyNPU: "Ascend910-0"},
				},
			},
			WantErr: nil,
		},
		{
			Name: "02-CheckNodeNPUByTask return err when task is not npu task",
			Task: test.FakeTaskWithResReq("pod1", util.NPU910CardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name:       "node1",
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3"},
				},
			},
			WantErr: errors.New("task<pod1> is not npu task"),
		},
		{
			Name: "03-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name:       "node1",
					Annotation: map[string]string{util.NPU310CardName: "Ascend310-0,Ascend310-1,Ascend310-2"},
				},
			},
			WantErr: errors.New("getUsableTopFromNode don't have huawei.com/Ascend910"),
		},
	}

}

func buildCheckNodeNPUByTaskTestCases02() []checkNodeNPUByTaskTestCase {
	return []checkNodeNPUByTaskTestCase{
		{
			Name: "04-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name:       "node1",
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0, Ascend910-1"},
				},
			},
			WantErr: errors.New("npu topology not meet job require,network unhealthy card is [  ]"),
		},
		{
			Name: "05-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name: "node1",
					Annotation: map[string]string{
						util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-4",
					},
				},
			},
			WantErr: errors.New("npu topology not meet job require,network unhealthy card is [  ]"),
		},
		{
			Name: "06-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, util.NPUIndex4),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name: "node1",
					Annotation: map[string]string{
						util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-4,Ascend910-5",
						networkUnhealthyNPU: "Ascend910-5",
					},
				},
			},
			WantErr: errors.New("npu topology not meet job require,network unhealthy card is [ Ascend910-5 ]"),
		},
	}
}

func buildCheckNodeNPUByTaskTestCases03() []checkNodeNPUByTaskTestCase {
	return []checkNodeNPUByTaskTestCase{
		{
			Name: "01-CheckNodeNPUByTask return err when task is nil",
			Task: nil,
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name:       "node1",
					Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0,Ascend310P-1"},
				},
			},
			WantErr: errors.New(util.ArgumentError),
		},
		{
			Name: "02-CheckNodeNPUByTask return err when node annotation is nil",
			Task: test.FakeTaskWithResReq("pod1", util.NPU310PCardName, util.NPUIndex2),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name:       "node1",
					Annotation: nil,
				},
			},
			WantErr: errors.New(util.ArgumentError),
		},
	}
}

// TestCheckNodeNPUByTask
func TestCheckNodeNPUByTask(t *testing.T) {
	npu := New(SchedulerName)
	job := test.FakeNormalTestJob("job", 1)
	test.SetFakeJobResRequest(job, util.NPU910CardName, "4")
	attr := itest.FakeSchedulerJobAttrByJob(job)
	sJob := plugin.SchedulerJob{}
	sJob.SchedulerJobAttr = attr
	env := plugin.ScheduleEnv{
		ClusterCache: plugin.ClusterCache{
			Jobs: map[api.JobID]plugin.SchedulerJob{job.UID: sJob}},
	}
	npu.SetSchedulerAttr(attr)
	npu.SetSchedulerEnv(env)
	testCases := buildCheckNodeNPUByTaskTestCases01()
	testCases = append(testCases, buildCheckNodeNPUByTaskTestCases02()...)
	testCases = append(testCases, buildCheckNodeNPUByTaskTestCases03()...)
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if err := npu.CheckNodeNPUByTask(tt.Task, tt.Node); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("ValidNPUJob() error = %v, wantErr %v", err, tt.WantErr)
			}
		})
	}
}

type scoreBestNPUNodesTestCase struct {
	Task     *api.TaskInfo
	Nodes    []*api.NodeInfo
	ScoreMap map[string]float64
	WantSMap map[string]float64
	Name     string
	WantErr  error
	Attr     util.SchedulerJobAttr
}

func buildScoreBestNPUNodesTestCases01() []scoreBestNPUNodesTestCase {
	return []scoreBestNPUNodesTestCase{
		{
			Name:     "01-ScoreBestNPUNodes return err when task is not this job npu task ",
			Task:     test.FakeTaskWithResReq("pod1", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0},
			WantSMap: map[string]float64{"node1": 0, "node2": 0},
			WantErr:  errors.New("task<pod1> is not npu task"),
		},
		{
			Name:     "02-ScoreBestNPUNodes scoreMap no refresh when node is not this job npu node",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node13"}},
			ScoreMap: map[string]float64{"node13": 0},
			WantSMap: map[string]float64{"node13": 0},
			WantErr:  nil,
		},
		{
			Name:     "03-ScoreBestNPUNodes scoreMap no refresh when node netUnhealthyNPU not define",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node14"}},
			ScoreMap: map[string]float64{"node14": 0},
			WantSMap: map[string]float64{"node14": 0},
			WantErr:  nil,
		},
	}
}

func buildScoreBestNPUNodesTestCases02() []scoreBestNPUNodesTestCase {
	const (
		score263 = 263
		score246 = 246
		score236 = 236
		score253 = 253
	)
	return []scoreBestNPUNodesTestCase{
		{
			Name:     "04-ScoreBestNPUNodes scoreMap no refresh when node has no npu",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node15"}},
			ScoreMap: map[string]float64{"node15": 0},
			WantSMap: map[string]float64{"node15": 0},
			WantErr:  nil,
		},
		{
			Name:     "05-ScoreBestNPUNodes return nil when node npu not meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node3"}, {Name: "node11"}, {Name: "node12"}},
			ScoreMap: map[string]float64{"node1": 0, "node3": 0, "node11": 0, "node12": 0},
			WantSMap: map[string]float64{"node1": score263, "node3": score253, "node11": score246, "node12": score236},
			WantErr:  nil,
		},
		{
			Name:     "06-ScoreBestNPUNodes return nil when node npu not meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node12"}, {Name: "node11"}},
			ScoreMap: map[string]float64{"node12": 0, "node11": 0},
			WantSMap: map[string]float64{"node12": score236, "node11": score246},
			WantErr:  nil,
		},
	}
}

func buildFakeScheduleEnv() plugin.ScheduleEnv {
	const allocateNPUNum8 = 8
	return plugin.ScheduleEnv{
		ClusterCache: plugin.ClusterCache{
			Nodes: map[string]plugin.NPUNode{
				"node1": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "Ascend910-0",
					networkUnhealthyNPU: ""},
					Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum8 * util.NPUHexKilo}}},
				"node2": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "Ascend910-0," +
					"Ascend910-1"}}},
				"node3": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "Ascend910-0," +
					"Ascend910-1,Ascend910-2", networkUnhealthyNPU: ""},
					Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum8 * util.NPUHexKilo}}},
				"node11": {CommonNode: plugin.CommonNode{Annotation: map[string]string{
					util.NPU910CardName: "Ascend910-4,Ascend910-5", networkUnhealthyNPU: ""},
					Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum8 * util.NPUHexKilo}}},
				"node12": {CommonNode: plugin.CommonNode{Annotation: map[string]string{
					util.NPU910CardName: "Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7", networkUnhealthyNPU: ""},
					Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum8 * util.NPUHexKilo}}},
				"node13": {CommonNode: plugin.CommonNode{Annotation: map[string]string{}}},
				"node14": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "Ascend910-0"}}},
				"node15": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "",
					networkUnhealthyNPU: ""}}}},
		},
	}
}

// TestCheckNodeNPUByTask
func TestScoreBestNPUNodes(t *testing.T) {
	npu := New(SchedulerName)
	job := test.FakeNormalTestJob("job", 1)
	test.SetFakeJobResRequest(job, util.NPU910CardName, "1")
	attr := itest.FakeSchedulerJobAttrByJob(job)
	npu.SetSchedulerAttr(attr)
	env := buildFakeScheduleEnv()
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	npu.SetSchedulerEnv(env)
	testCases := buildScoreBestNPUNodesTestCases01()
	testCases = append(testCases, buildScoreBestNPUNodesTestCases02()...)
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			err := npu.ScoreBestNPUNodes(tt.Task, tt.Nodes, tt.ScoreMap)
			if !reflect.DeepEqual(err, tt.WantErr) || !reflect.DeepEqual(tt.ScoreMap, tt.WantSMap) {
				t.Errorf("ScoreBestNPUNodes() scoreMap: %v, wantSMap: %v, error = %v, wantErr %v",
					tt.ScoreMap, tt.WantSMap, err, tt.WantErr)
			}
		})
	}
}

type useAnnotationTestCase struct {
	Task     *api.TaskInfo
	WantNode *plugin.NPUNode
	Name     string
	Node     plugin.NPUNode
	PodAnno  string
	Attr     util.SchedulerJobAttr
}

func buildUseAnnotationTestCases01() []useAnnotationTestCase {
	return []useAnnotationTestCase{
		{
			Name: "01-UseAnnotation task will select the npu which is the only one on the ring",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-4,Ascend910-5",
						networkUnhealthyNPU: ""},
				},
			},
			PodAnno: "Ascend910-0",
			WantNode: &plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-4,Ascend910-5", networkUnhealthyNPU: ""},
				},
			},
		},
		{
			Name: "02-UseAnnotation task will select the npu which is on the card that has 3 npu other than 2",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-4," +
						"Ascend910-5", networkUnhealthyNPU: "Ascend910-0"},
				},
			},
			PodAnno: "Ascend910-0",
			WantNode: &plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-1,Ascend910-2,Ascend910-4,Ascend910-5",
						networkUnhealthyNPU: "Ascend910-0"},
				},
			},
		},
	}
}

func buildUseAnnotationTestCases02() []useAnnotationTestCase {
	return []useAnnotationTestCase{
		{
			Name: "03-UseAnnotation task will select the npu which is on the card that has 3 npu other than 2",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-4,Ascend910-5," +
						"Ascend910-6", networkUnhealthyNPU: ""},
				},
			},
			PodAnno: "Ascend910-4",
			WantNode: &plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-5,Ascend910-6",
						networkUnhealthyNPU: ""},
				},
			},
		},
		{
			Name: "04-UseAnnotation task will select the npu which is on the card that has 2 npu other than 4",
			Task: test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-4,Ascend910-5," +
						"Ascend910-6,Ascend910-7", networkUnhealthyNPU: ""}},
			},
			PodAnno: "Ascend910-0",
			WantNode: &plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-1,Ascend910-4,Ascend910-5,Ascend910-6," +
						"Ascend910-7", networkUnhealthyNPU: ""}},
			},
		},
	}
}

func TestUseAnnotation(t *testing.T) {
	npu := New(SchedulerName)
	job := test.FakeNormalTestJob("job", 1)
	test.SetFakeJobResRequest(job, util.NPU910CardName, "1")
	attr := itest.FakeSchedulerJobAttrByJob(job)
	npu.SetSchedulerAttr(attr)
	env := plugin.ScheduleEnv{
		ClusterCache: plugin.ClusterCache{
			Jobs: map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}},
	}
	npu.SetSchedulerEnv(env)
	testCases := buildUseAnnotationTestCases01()
	testCases = append(testCases, buildUseAnnotationTestCases02()...)
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			node := npu.UseAnnotation(tt.Task, tt.Node)
			if tt.Task != nil && tt.Node.Annotation != nil && (!reflect.DeepEqual(node.Annotation,
				tt.Node.Annotation)) || !reflect.DeepEqual(tt.Task.Pod.Annotations[util.NPU910CardName], tt.PodAnno) {
				t.Errorf("UseAnnotation() node: %v, wantNode: %v, anno %v, wantAnno %v",
					node, tt.WantNode, tt.Task.Pod.Annotations, tt.PodAnno)
			}
			if (tt.Task == nil || tt.Node.Annotation == nil) || !reflect.DeepEqual(node, tt.WantNode) {
				t.Errorf("UseAnnotation() node: %v, wantNode: %v", node, tt.WantNode)
			}
		})
	}
}

func TestNilFunc(t *testing.T) {
	t.Run("tp nil test", func(t *testing.T) {
		var tp *module910x8
		wantErr := errors.New(util.ArgumentError)
		if err := tp.CheckNodeNPUByTask(nil, plugin.NPUNode{}); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, wantErr)
		}
		if err := tp.ScoreBestNPUNodes(nil, nil, nil); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, wantErr)
		}
		if node := tp.UseAnnotation(nil, plugin.NPUNode{}); node != nil {
			t.Errorf("UseAnnotation() error = %v, want %v", node, nil)
		}
	})
}
