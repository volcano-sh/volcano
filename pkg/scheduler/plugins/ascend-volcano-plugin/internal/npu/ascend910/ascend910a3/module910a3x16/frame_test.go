/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package module910a3x16 is using for A3 x16 affinity schedule.
*/
package module910a3x16

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910a3"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const (
	unhealthyKey = ascend910a3.NetworkUnhealthyNPU
	cardName     = util.NPU910CardName

	card0  = "Ascend910-0"
	card1  = "Ascend910-1"
	card2  = "Ascend910-2"
	card3  = "Ascend910-3"
	card4  = "Ascend910-4"
	card5  = "Ascend910-5"
	card6  = "Ascend910-6"
	card7  = "Ascend910-7"
	card8  = "Ascend910-8"
	card9  = "Ascend910-9"
	card10 = "Ascend910-10"
	card11 = "Ascend910-11"
	card12 = "Ascend910-12"
	card13 = "Ascend910-13"
	card14 = "Ascend910-14"
	card15 = "Ascend910-15"

	npuNum0  = 0
	npuNum1  = 1
	npuNum2  = 2
	npuNum3  = 3
	npuNum4  = 4
	npuNum5  = 5
	npuNum6  = 6
	npuNum7  = 7
	npuNum8  = 8
	npuNum9  = 9
	npuNum10 = 10
	npuNum11 = 11
	npuNum12 = 12
	npuNum13 = 13
	npuNum14 = 14
	npuNum15 = 15
	npuNum16 = 16
	npuNum32 = 32
	npuNum33 = 33

	node1 = "node1"
	node2 = "node2"
	node3 = "node3"
	task1 = "task1"

	score176 = 176
	score208 = 208
	score240 = 240
	score416 = 416
	score432 = 432
	score448 = 448
	score464 = 464
	score480 = 480
	score496 = 496
	score512 = 512
)

func TestValidNPUJob(t *testing.T) {
	tests := []struct {
		name       string
		NPUTaskNum int
		ReqNPUNum  int
		Labels     map[string]string
		Task       map[api.TaskID]util.NPUTask
		pass       bool
	}{
		{name: "01-want  pass when job label is nil", NPUTaskNum: 1, ReqNPUNum: 0,
			Labels: map[string]string{}, pass: true},
		{name: "02-want pass when job is invalid", NPUTaskNum: 1, ReqNPUNum: 1,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "03-want pass when request 1 npu", NPUTaskNum: 1, ReqNPUNum: 1,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "04-want pass when request 2 npu", NPUTaskNum: 1, ReqNPUNum: npuNum2,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "05-want pass when request 4 npu", NPUTaskNum: 1, ReqNPUNum: npuNum4,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "06-want not pass when request 5 npu", NPUTaskNum: 1, ReqNPUNum: npuNum5,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: false},
		{name: "07-want pass when request 16 npu", NPUTaskNum: 1, ReqNPUNum: npuNum16,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "08-want pass when request 32 npu", NPUTaskNum: npuNum2, ReqNPUNum: npuNum32,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: true},
		{name: "09-want not pass when request 33 npu", NPUTaskNum: npuNum2, ReqNPUNum: npuNum33,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, pass: false},
		{name: "10-want not pass when request 32 npu", NPUTaskNum: npuNum3, ReqNPUNum: npuNum32,
			Labels: map[string]string{util.JobKindKey: util.JobKind910BValue}, Task: map[api.TaskID]util.NPUTask{
				"task-1": {ReqNPUNum: npuNum16}, "task-2": {ReqNPUNum: npuNum8}, "task-3": {ReqNPUNum: npuNum8}}, pass: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, _ := New(SchedulerName).(*module910a3x16)
			tp.Label = tt.Labels
			tp.NPUJob = &util.NPUJob{Tasks: tt.Task}
			tp.NPUTaskNum = tt.NPUTaskNum
			tp.ReqNPUNum = tt.ReqNPUNum
			got := tp.ValidNPUJob()
			if tt.pass && got != nil {
				t.Errorf("ValidNPUJob() = %v, want pass", got)
			}
			if !tt.pass && got == nil {
				t.Errorf("ValidNPUJob() = %v, want not pass", got)
			}
		})
	}
}

type checkNodeNPUByTaskData struct {
	name           string
	task           *api.TaskInfo
	idleCards      []string
	unhealthyCards []string
	taskNPUNum     int
	npuTaskNum     int
	wantErr        bool
}

func genCheckNodeNPUByTaskData() []checkNodeNPUByTaskData {
	return []checkNodeNPUByTaskData{
		{name: "01-want err when task is nil", task: nil, wantErr: true},
		{name: "02-want success when task can be schedule", taskNPUNum: 1, npuTaskNum: 1,
			idleCards: []string{card0, card1}, unhealthyCards: []string{card1}, wantErr: false},
		{name: "03-want err when task can not be schedule", taskNPUNum: npuNum2, npuTaskNum: 1,
			idleCards: []string{card0}, unhealthyCards: []string{}, wantErr: true},
		{name: "04-want success when task can be schedule", taskNPUNum: npuNum4, npuTaskNum: 1,
			idleCards:      []string{card0, card1, card2, card3, card4, card5, card6},
			unhealthyCards: []string{card2, card3, card6}, wantErr: false},
		{name: "05-want err when unhealthy annot not exist", taskNPUNum: npuNum16, npuTaskNum: npuNum2,
			idleCards: []string{card0}, wantErr: true},
		{name: "06-want err when card npu > node max card", taskNPUNum: 1, npuTaskNum: 1,
			idleCards: []string{card0, card1, card2, card3, card4, card5, card6, card7, card1, card2, card3, card4,
				card5, card6, card0, card1, card2}, wantErr: true},
		{name: "07-want err when unhealthy card npu > node max card", taskNPUNum: npuNum16, npuTaskNum: npuNum2,
			idleCards: []string{card0}, unhealthyCards: []string{card0, card1, card2, card3, card4, card5, card6,
				card0, card1, card2, card3, card4, card5, card6, card0, card1, card2}, wantErr: true},
		{name: "08-want err when task can not be schedule", taskNPUNum: npuNum4, npuTaskNum: 1,
			idleCards: []string{card0, card1, card3, card5, card7}, unhealthyCards: []string{card7}, wantErr: true},
	}
}

func TestCheckNodeNPUByTask(t *testing.T) {
	tests := genCheckNodeNPUByTaskData()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, _ := New(SchedulerName).(*module910a3x16)
			patch := gomonkey.ApplyMethod(reflect.TypeOf(new(base.NPUHandler)), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, task *api.TaskInfo) (int, error) { return tt.taskNPUNum, nil })
			defer patch.Reset()
			tp.NPUJob = &util.NPUJob{NPUTaskNum: tt.npuTaskNum}
			node := plugin.NPUNode{}
			node.Annotation = map[string]string{}
			if len(tt.idleCards) != 0 {
				node.Annotation[cardName] = strings.Join(tt.idleCards, ",")
			}
			if len(tt.unhealthyCards) != 0 {
				node.Annotation[unhealthyKey] = strings.Join(tt.unhealthyCards, ",")
			}
			got := tp.CheckNodeNPUByTask(&api.TaskInfo{}, node)
			if tt.wantErr != (got != nil) {
				t.Errorf("CheckNodeNPUByTask() = %v, wanterr = %v", got, tt.wantErr)
			}
		})
	}
}

type scoreBestNPUNodesData struct {
	name       string
	task       *api.TaskInfo
	apiNodes   []*api.NodeInfo
	plgNodes   map[string]plugin.NPUNode
	taskNPUNum int
	score      map[string]float64
	wantScore  map[string]float64
	wantErr    bool
}

func genScoreBestNPUNodes01() []scoreBestNPUNodesData {
	return []scoreBestNPUNodesData{
		{name: "01-want err when task is nil", task: nil, wantErr: true},
		{name: "02-want success when node is nil", task: &api.TaskInfo{Name: task1},
			apiNodes: []*api.NodeInfo{nil}, plgNodes: map[string]plugin.NPUNode{},
			score: map[string]float64{node1: 0}, wantScore: map[string]float64{node1: 0}, wantErr: false},
		{name: "03-want success when not found node", task: &api.TaskInfo{Name: task1},
			apiNodes: []*api.NodeInfo{{Name: node1}}, plgNodes: map[string]plugin.NPUNode{node2: {}},
			score: map[string]float64{node1: 0}, wantScore: map[string]float64{node1: 0}, wantErr: false},
		{name: "04-want success when allocate not found card", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}}, plgNodes: map[string]plugin.NPUNode{node1: {
				CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0}, ",")}, Allocate: map[v1.ResourceName]float64{}}}},
			score: map[string]float64{node1: 0}, wantScore: map[string]float64{node1: score512}, wantErr: false},
		{name: "05-want success when node cards > 16", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}}, plgNodes: map[string]plugin.NPUNode{node1: {
				CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4, card5, card6, card7, card1,
						card2, card3, card4, card5, card6, card0, card1, card2, card3, card4, card5, card6}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum1 * util.NPUHexKilo}}},
			}, score: map[string]float64{node1: 0}, wantScore: map[string]float64{node1: 0}, wantErr: false},
		{name: "06-want success when schedule task", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}}, plgNodes: map[string]plugin.NPUNode{node1: {
				CommonNode: plugin.CommonNode{Annotation: map[string]string{cardName: strings.Join(
					[]string{card0}, ","), unhealthyKey: ""},
					Allocate: map[v1.ResourceName]float64{cardName: npuNum1 * util.NPUHexKilo}}}},
			score: map[string]float64{node1: 0}, wantScore: map[string]float64{node1: score512}, wantErr: false},
		{name: "07-want success when schedule single die task", task: &api.TaskInfo{Name: task1},
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum2 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum3 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0},
			wantScore: map[string]float64{node1: score240, node2: score480},
			wantErr:   false, taskNPUNum: npuNum1},
	}
}

func genScoreBestNPUNodes02() []scoreBestNPUNodesData {
	return []scoreBestNPUNodesData{
		{name: "08-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card4, card5}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum4 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum2 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum6 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score208, node2: score240, node3: score176}, wantErr: false},
		{name: "09-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card4, card5}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum4 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum2 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card5, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum7 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score208, node2: score240, node3: score416}, wantErr: false},
		{name: "10-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card4, card5}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum4 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card3}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum3 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card5, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum7 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score208, node2: score480, node3: score416}, wantErr: false},
	}
}

func genScoreBestNPUNodes03() []scoreBestNPUNodesData {
	return []scoreBestNPUNodesData{
		{name: "11-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum1,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card4, card5}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum5 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card3}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum3 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card5, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum7 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score448, node2: score480, node3: score416}, wantErr: false},
		{name: "12-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum2,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card4, card5}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum5 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card3}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum3 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card5, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum7 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score464, node2: score496, node3: score432}, wantErr: false},
		{name: "13-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum2,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card4}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum4 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum2 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card6, card7}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum6 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score480, node2: score512, node3: 0}, wantErr: false},
	}
}

func genScoreBestNPUNodes04() []scoreBestNPUNodesData {
	return []scoreBestNPUNodesData{
		{name: "14-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum10,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum10 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum16 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9, card10}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum11 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score512, node2: 0, node3: 0}, wantErr: false},
		{name: "15-want success when scheduler", task: &api.TaskInfo{Name: task1}, taskNPUNum: npuNum16,
			apiNodes: []*api.NodeInfo{{Name: node1}, {Name: node2}, {Name: node3}}, plgNodes: map[string]plugin.NPUNode{
				node1: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum16 * util.NPUHexKilo}}},
				node2: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum16 * util.NPUHexKilo}}},
				node3: {CommonNode: plugin.CommonNode{Annotation: map[string]string{unhealthyKey: "",
					cardName: strings.Join([]string{card0, card1, card2, card3, card4,
						card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15}, ","),
				}, Allocate: map[v1.ResourceName]float64{cardName: npuNum16 * util.NPUHexKilo}}}},
			score:     map[string]float64{node1: 0, node2: 0, node3: 0},
			wantScore: map[string]float64{node1: score512, node2: 0, node3: 0}, wantErr: false},
	}
}

func TestScoreBestNPUNodes(t *testing.T) {
	var tests []scoreBestNPUNodesData
	tests = append(tests, genScoreBestNPUNodes01()...)
	tests = append(tests, genScoreBestNPUNodes02()...)
	tests = append(tests, genScoreBestNPUNodes03()...)
	tests = append(tests, genScoreBestNPUNodes04()...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, _ := New(SchedulerName).(*module910a3x16)
			reqNPUNumPatch := gomonkey.ApplyMethod(reflect.TypeOf(new(base.NPUHandler)), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, task *api.TaskInfo) (int, error) { return tt.taskNPUNum, nil })
			defer reqNPUNumPatch.Reset()
			tp.Nodes = tt.plgNodes
			tp.NPUJob = &util.NPUJob{NPUTaskNum: 2}
			if err := tp.ScoreBestNPUNodes(tt.task, tt.apiNodes, tt.score); (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (reflect.DeepEqual(tt.score, tt.wantScore) == false) {
				t.Errorf("ScoreBestNPUNodes() score = %v, want %v", tt.score, tt.wantScore)
			}
		})
	}
}

type selectNPUFromNodeParam struct {
	name           string
	taskNPUNum     int
	npuTaskNum     int
	idleCards      []string
	unhealthyCards []string
	want           []int
	wantErr        bool
}

func genSelectNPUFromNodeData() []selectNPUFromNodeParam {
	tests := []selectNPUFromNodeParam{
		{name: "01-want err when task is nil", wantErr: true},
		{name: "02-want err when task npu num is 0", wantErr: true},
		{name: "03-want err when node npu num is 0", taskNPUNum: 1, wantErr: true},
		{name: "04-want success when select npu", taskNPUNum: 1, npuTaskNum: 1,
			idleCards: []string{card0}, want: []int{0}, wantErr: false},
		{name: "05-want err when distribute job not request 16 cards", taskNPUNum: npuNum2, npuTaskNum: npuNum2,
			idleCards: []string{card0, card1}, wantErr: true},
		{name: "06-want success when select npu", taskNPUNum: npuNum2, npuTaskNum: 1,
			idleCards: []string{card0, card1, card5, card3}, want: []int{0, 1}, wantErr: false},
		{name: "07-want success when select npu", taskNPUNum: npuNum16, npuTaskNum: npuNum2,
			idleCards: []string{card0, card1, card2, card3, card4,
				card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15},
			want: []int{npuNum0, npuNum1, npuNum2, npuNum3, npuNum4, npuNum5, npuNum6, npuNum7, npuNum8, npuNum9,
				npuNum10, npuNum11, npuNum12, npuNum13, npuNum14, npuNum15}, wantErr: false},
		{name: "08-want err when select npu", taskNPUNum: npuNum16, npuTaskNum: npuNum2,
			idleCards: []string{card0, card1, card2, card3, card4,
				card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15},
			unhealthyCards: []string{card0, card1, card2, card3, card4,
				card5, card6, card7, card8, card9, card10, card11, card12, card13, card14, card15}, wantErr: true},
		{name: "09-want success when select npu", taskNPUNum: npuNum6, npuTaskNum: 1,
			idleCards: []string{card0, card1, card3, card6, card7, card8, card9},
			want:      []int{npuNum0, npuNum1, npuNum6, npuNum7, npuNum8, npuNum9}, wantErr: false},
		{name: "10-want success when select npu", taskNPUNum: npuNum1, npuTaskNum: 1,
			idleCards: []string{card0, card1, card2, card3, card6, card7, card11},
			want:      []int{npuNum11}, wantErr: false},
		{name: "11-want success when select npu", taskNPUNum: npuNum2, npuTaskNum: 1,
			idleCards: []string{card0, card2, card6, card11, card12, card13},
			want:      []int{npuNum12, npuNum13}, wantErr: false},
	}
	return tests
}

func TestSelectNPUFromNode(t *testing.T) {
	tests := genSelectNPUFromNodeData()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp, _ := New(SchedulerName).(*module910a3x16)
			reqNPUNumPatch := gomonkey.ApplyMethod(reflect.TypeOf(new(base.NPUHandler)), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, task *api.TaskInfo) (int, error) { return tt.taskNPUNum, nil })
			defer reqNPUNumPatch.Reset()
			tp.NPUJob = &util.NPUJob{NPUTaskNum: tt.npuTaskNum}
			node := plugin.NPUNode{}
			node.Annotation = map[string]string{}
			if len(tt.idleCards) != 0 {
				node.Annotation[cardName] = strings.Join(tt.idleCards, ",")
			}
			node.Annotation[unhealthyKey] = ""
			if len(tt.unhealthyCards) != 0 {
				node.Annotation[unhealthyKey] = strings.Join(tt.unhealthyCards, ",")
			}
			got, err := tp.SelectNPUFromNode(&api.TaskInfo{}, node, tp.NPUTaskNum > 1)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectNPUFromNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			sort.Ints(got)
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("selectNPUFromNode() got = %v, want %v", got, tt.want)
			}
		})
	}
}
