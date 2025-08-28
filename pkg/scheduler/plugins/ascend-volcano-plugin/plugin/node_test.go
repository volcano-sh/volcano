/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package plugin is using for HuaWei Ascend pin affinity schedule frame.
*/
package plugin

import (
	"reflect"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type nodeFields struct {
	Name       string
	Capability map[v1.ResourceName]float64
	Allocate   map[v1.ResourceName]float64
	Idle       map[v1.ResourceName]float64
	Annotation map[string]string
	Label      map[string]string
}

type checkNPUResourceStableArgs struct {
	vcJob SchedulerJob
}

type checkNPUResourceStableTest struct {
	name    string
	fields  nodeFields
	args    checkNPUResourceStableArgs
	wantErr bool
}

func buildVCheckNPUResourceStableTest() []checkNPUResourceStableTest {
	tJob := SchedulerJob{policyHandler: New(testPluginName),
		SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{ReqNPUName: util.NPU310PCardName}}}
	vJob := SchedulerJob{policyHandler: New(testPluginName),
		SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{ReqNPUName: util.AscendNPUCore}}}
	tests := []checkNPUResourceStableTest{
		{
			name:    "01-checkNPUResourceStable no annotation test",
			fields:  nodeFields{Name: "haha", Idle: map[v1.ResourceName]float64{testCardName: 1}, Annotation: nil},
			args:    checkNPUResourceStableArgs{vcJob: tJob},
			wantErr: true,
		},
		{
			name: "02-checkNPUResourceStable ok test.",
			fields: nodeFields{Name: "haha", Idle: map[v1.ResourceName]float64{util.NPU310PCardName: util.NPUHexKilo},
				Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0"}},
			args:    checkNPUResourceStableArgs{vcJob: tJob},
			wantErr: false,
		},
		{
			name: "03-checkNPUResourceStable vNPU ok test.",
			fields: nodeFields{Name: "haha", Idle: map[v1.ResourceName]float64{testCardName: 1},
				Annotation: map[string]string{testCardName: "haha"}},
			args:    checkNPUResourceStableArgs{vcJob: vJob},
			wantErr: false,
		},
	}
	return tests
}

func TestCheckNPUResourceStable(t *testing.T) {
	tests := buildVCheckNPUResourceStableTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NPUNode{
				CommonNode: CommonNode{
					Name:       tt.fields.Name,
					Capability: tt.fields.Capability,
					Allocate:   tt.fields.Allocate,
					Idle:       tt.fields.Idle,
					Annotation: tt.fields.Annotation,
					Label:      tt.fields.Label,
				},
			}
			if err := n.checkNPUResourceStable(tt.args.vcJob); (err != nil) != tt.wantErr {
				t.Errorf("checkNPUResourceStable() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type nodePredicateArgs struct {
	taskInfo *api.TaskInfo
	nodeInfo *api.NodeInfo
}

type nodePredicateTest struct {
	name    string
	fields  fields
	args    nodePredicateArgs
	wantErr bool
}

func buildNodePredicateTest() []nodePredicateTest {
	tTasks := test.FakeNormalTestTasks(1)
	tNode := test.FakeNormalTestNode("haha")
	tests := []nodePredicateTest{
		{
			name:    "01-NodePredicate nil test.",
			fields:  fields{},
			args:    nodePredicateArgs{taskInfo: &api.TaskInfo{}, nodeInfo: nil},
			wantErr: true,
		},
		{
			name: "02-NodePredicate job not in test.",
			fields: fields{ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{"haha": {}},
				}}},
			args:    nodePredicateArgs{taskInfo: tTasks[0], nodeInfo: tNode},
			wantErr: false,
		},
		{
			name: "03-NodePredicate node not in test.",
			fields: fields{ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs:  map[api.JobID]SchedulerJob{tTasks[0].Job: {policyHandler: New(PluginName)}},
					Nodes: map[string]NPUNode{"lala": {}}}}},
			args:    nodePredicateArgs{taskInfo: tTasks[0], nodeInfo: tNode},
			wantErr: false,
		},
		{
			name: "04-NodePredicate node not in test.",
			fields: fields{ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs:  map[api.JobID]SchedulerJob{tTasks[0].Job: {policyHandler: New(PluginName)}},
					Nodes: map[string]NPUNode{"haha": {}}}}},
			args:    nodePredicateArgs{taskInfo: tTasks[0], nodeInfo: tNode},
			wantErr: true,
		},
		{
			name: "05-NodePredicate ok test.",
			fields: fields{ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs:  map[api.JobID]SchedulerJob{tTasks[0].Job: {policyHandler: New(PluginName)}},
					Nodes: map[string]NPUNode{"haha": {}}}}},
			args:    nodePredicateArgs{taskInfo: tTasks[0], nodeInfo: tNode},
			wantErr: true,
		},
		{
			name: "06-NodePredicate UnHealthy Node test.",
			fields: fields{ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{tTasks[0].Job: {policyHandler: New(PluginName)}},
					Nodes: map[string]NPUNode{"haha": {
						CommonNode: CommonNode{
							Label:      map[string]string{util.NodeDEnableKey: util.NodeDEnableOnValue},
							Annotation: map[string]string{util.NodedNodeHealtyStatuskey: util.PreSeparateFaultCode},
						}}}}}},
			args:    nodePredicateArgs{taskInfo: tTasks[0], nodeInfo: tNode},
			wantErr: true,
		},
	}
	return tests
}

func TestSNodePredicate(t *testing.T) {
	tests := buildNodePredicateTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			tmpJob := sHandle.ScheduleEnv.Jobs["vcjob/pg0"]
			tmpJob.NPUJob = &util.NPUJob{}
			tmpJob.ReqNPUName = util.NPU910CardName
			if len(sHandle.ScheduleEnv.Jobs) != 0 {
				sHandle.ScheduleEnv.Jobs["vcjob/pg0"] = tmpJob
			}
			tt.args.taskInfo.Resreq = &api.Resource{}
			tt.args.taskInfo.Resreq.ScalarResources = make(map[v1.ResourceName]float64)
			tt.args.taskInfo.Resreq.ScalarResources[util.Ascend910bName] = util.NPUIndex10
			if err := sHandle.NodePredicate(tt.args.taskInfo, tt.args.nodeInfo); (err != nil) != tt.wantErr {
				t.Errorf("NodePredicate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type nPUNodeGetNewNPUNodeAnnotationTest struct {
	name            string
	usedTop         []int
	resourceName    string
	resourceNamePre string
	npuNode         *NPUNode
	want            string
	wantErr         bool
}

func buildNPUNodeGetNewNPUNodeAnnotationTest() []nPUNodeGetNewNPUNodeAnnotationTest {
	return []nPUNodeGetNewNPUNodeAnnotationTest{
		{
			name:    "01-GetNewNPUNodeAnnotation return error when npuNode is nil",
			npuNode: nil,
			wantErr: true,
		},
		{
			name:            "02-GetNewNPUNodeAnnotation return error when npuNode annotation is empty",
			npuNode:         &NPUNode{},
			usedTop:         []int{0},
			resourceName:    Ascend910,
			resourceNamePre: util.NPU910CardNamePre,
			wantErr:         true,
		},
		{
			name: "03-GetNewNPUNodeAnnotation return empty when npuNode annotation is empty",
			npuNode: &NPUNode{CommonNode: CommonNode{
				Annotation: map[string]string{Ascend910: ""}}},
			usedTop:         []int{0},
			resourceName:    Ascend910,
			resourceNamePre: util.NPU910CardNamePre,
			want:            "",
			wantErr:         false,
		},
		{
			name: "04-GetNewNPUNodeAnnotation return error when string to int error",
			npuNode: &NPUNode{CommonNode: CommonNode{
				Annotation: map[string]string{Ascend910: "Ascend910-s"}}},
			usedTop:         []int{0},
			resourceName:    Ascend910,
			resourceNamePre: util.NPU910CardNamePre,
			want:            "",
			wantErr:         true,
		},
		{
			name: "05-GetNewNPUNodeAnnotation return Ascend910-1 when get npu node annotation",
			npuNode: &NPUNode{CommonNode: CommonNode{
				Annotation: map[string]string{Ascend910: "Ascend910-0,Ascend910-1"}}},
			usedTop:         []int{0},
			resourceName:    Ascend910,
			resourceNamePre: util.NPU910CardNamePre,
			want:            "Ascend910-1",
			wantErr:         false,
		},
	}
}

func TestNPUNodeGetNewNPUNodeAnnotation(t *testing.T) {
	tests := buildNPUNodeGetNewNPUNodeAnnotationTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.npuNode.GetNewNPUNodeAnnotation(tt.usedTop, tt.resourceName, tt.resourceNamePre)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNewNPUNodeAnnotation() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetNewNPUNodeAnnotation() got = %v, want = %v", got, tt.want)
			}
		})
	}
}

type updateNPUNodeDeviceInfosTest struct {
	name string
	node NPUNode
	data k8s.NodeDeviceInfoWithID
}

func buildUpdateNPUNodeDeviceInfosTest() []updateNPUNodeDeviceInfosTest {
	return []updateNPUNodeDeviceInfosTest{
		{
			name: "01 force update by device info cm",
			node: NPUNode{CommonNode: CommonNode{devInfoUpdateTime: 0, Annotation: map[string]string{}}},
			data: k8s.NodeDeviceInfoWithID{NodeDeviceInfo: k8s.NodeDeviceInfo{UpdateTime: time.Now().Unix(),
				DeviceList: k8s.FakeDeviceList(),
			}},
		},
		{
			name: "02 update by device info cm and volcano cache",
			node: NPUNode{CommonNode: CommonNode{
				Idle:              map[v1.ResourceName]float64{util.NPU910CardName: util.NPUHexKilo},
				devInfoUpdateTime: time.Now().Unix(),
				Annotation:        map[string]string{util.NPU910CardName: "Ascend910-0"}}},
			data: k8s.NodeDeviceInfoWithID{NodeDeviceInfo: k8s.NodeDeviceInfo{UpdateTime: time.Now().Unix() +
				util.NPUIndex3, DeviceList: k8s.FakeDeviceList(),
			}},
		},
	}
}

func TestUpdateNPUNodeDeviceInfos(t *testing.T) {
	for _, tt := range buildUpdateNPUNodeDeviceInfosTest() {
		t.Run(tt.name, func(t *testing.T) {
			tt.node.updateNPUNodeDeviceInfos(tt.data)
		})
	}
}

type getNeedInitNodeListTest struct {
	name    string
	ssn     *framework.Session
	sHandle *ScheduleHandler
	want    []*api.NodeInfo
}

func buildGetNeedInitNodeListTest() []getNeedInitNodeListTest {
	sHandler := &ScheduleHandler{}
	sHandler.FrameAttr.KubeClient = fake.NewSimpleClientset()
	sHandler.FrameAttr.informerFactory = fakeInformerFactory()
	sHandler.Nodes = map[string]NPUNode{"node01": {}}
	return []getNeedInitNodeListTest{
		{
			name:    "01 will return nil when node list is nil and kubeClient is nil",
			ssn:     &framework.Session{},
			sHandle: &ScheduleHandler{},
			want:    nil,
		},
		{
			name:    "02 will return empty when node list is nil ",
			ssn:     &framework.Session{},
			sHandle: sHandler,
			want:    []*api.NodeInfo{},
		},
	}
}

func TestGetNeedInitNodeList(t *testing.T) {
	for _, tt := range buildGetNeedInitNodeListTest() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sHandle.getNeedInitNodeList(tt.ssn); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNeedInitNodeList() = %v, want %v", got, tt.want)
			}
		})
	}
}
