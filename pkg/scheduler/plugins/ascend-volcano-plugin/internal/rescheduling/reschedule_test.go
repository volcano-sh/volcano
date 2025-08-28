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
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

const (
	fakeTime        = 123455
	fakeTime2       = 11111
	createTime      = 10000
	graceDeleteTime = 900
	zero            = 0
	one             = 1
	two             = 2
	three           = 3
	mockJobName1    = "jobName1"
	mockJobName2    = "jobName2"
	minTestSliceLen = 0
	maxTestSliceLen = 1000
)

func fakeTestFaultCardUnhealthy(name string, nodeName string, faultType string) *FaultCard {
	return &FaultCard{
		IsFaultCard: true,
		NPUName:     name,
		FaultType:   faultType,
	}
}

func fakeTestFaultCardHealthy(name string, nodeName string) *FaultCard {
	return &FaultCard{
		IsFaultCard: false,
		NPUName:     name,
		FaultType:   CardHealthy,
	}
}

func fakeTestFaultCardsUnhealthy(nodeName string, isUnHealth bool) []FaultCard {
	var card0 *FaultCard
	if !isUnHealth {
		card0 = fakeTestFaultCardUnhealthy("Ascend910-0", nodeName, CardUnhealthy)
	} else {
		card0 = fakeTestFaultCardHealthy("Ascend910-0", nodeName)
	}
	cards := []FaultCard{
		*card0,
		*fakeTestFaultCardHealthy("Ascend910-1", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-2", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-3", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-4", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-5", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-6", nodeName),
		*fakeTestFaultCardHealthy("Ascend910-7", nodeName),
	}
	return cards
}

func fakeTestFaultNodeCardUnhealthy(nodeName string, allCard []string) *FaultNode {
	updateTime := int64(fakeTime2)
	faultCards := fakeTestFaultCardsUnhealthy(nodeName, true)
	return &FaultNode{
		NodeName:            nodeName,
		UpdateTime:          updateTime,
		NPUName:             util.NPU910CardName,
		UnhealthyNPU:        []string{"Ascend910-0"},
		NetworkUnhealthyNPU: nil,
		IsFaultNode:         true,
		NodeDEnable:         true,
		NodeHealthState:     NodeCardUnhealthy,
		FaultCards:          faultCards,
	}
}

func fakeTestFaultNodeNodeUnhealthy(nodeName string) *FaultNode {
	updateTime := int64(fakeTime2)
	faultCards := fakeTestFaultCardsUnhealthy(nodeName, false)
	return &FaultNode{
		NodeName:            nodeName,
		NPUName:             util.NPU910CardName,
		UpdateTime:          updateTime,
		UnhealthyNPU:        nil,
		NetworkUnhealthyNPU: nil,
		IsFaultNode:         true,
		NodeDEnable:         true,
		NodeHealthState:     NodeUnhealthy,
		FaultCards:          faultCards,
	}
}

func fakeTestFaultNodeNodeHealthy(nodeName string) *FaultNode {
	updateTime := int64(fakeTime2)
	faultCards := fakeTestFaultCardsUnhealthy(nodeName, false)
	return &FaultNode{
		NodeName:            nodeName,
		NPUName:             util.NPU910CardName,
		UpdateTime:          updateTime,
		UnhealthyNPU:        nil,
		NetworkUnhealthyNPU: nil,
		IsFaultNode:         false,
		NodeDEnable:         true,
		NodeHealthState:     NodeHealthy,
		FaultCards:          faultCards,
	}
}

func fakeTestFaultNodeNodeHealthyOneCard(nodeName string) *FaultNode {
	updateTime := int64(fakeTime2)
	faultCards := []FaultCard{*fakeTestFaultCardHealthy("Ascend910-0", nodeName)}
	return &FaultNode{
		NodeName:            nodeName,
		UpdateTime:          updateTime,
		NPUName:             util.NPU910CardName,
		UnhealthyNPU:        nil,
		NetworkUnhealthyNPU: nil,
		IsFaultNode:         false,
		NodeDEnable:         true,
		NodeHealthState:     NodeHealthy,
		FaultCards:          faultCards,
	}
}

func fakeTestFaultTaskFault(name string, namespace string,
	nodeName string, nodeRankIndex string, podUID types.UID) *FaultTask {
	return &FaultTask{
		IsFaultTask:   true,
		TaskName:      name,
		TaskNamespace: namespace,
		NodeName:      nodeName,
		NodeRankIndex: nodeRankIndex,
		UseCardName:   []string{"Ascend910-0", "Ascend910-1", "Ascend910-2", "Ascend910-3"},
		PodCreateTime: int64(createTime),
	}
}

func fakeTestFaultTaskHealth(name string, namespace string, nodeName string,
	nodeRankIndex string, podUID types.UID) *FaultTask {
	return &FaultTask{
		IsFaultTask:   false,
		TaskName:      name,
		TaskNamespace: namespace,
		NodeName:      nodeName,
		NodeRankIndex: nodeRankIndex,
		UseCardName:   []string{"Ascend910-0", "Ascend910-1", "Ascend910-2", "Ascend910-3"},
		PodCreateTime: int64(createTime),
	}
}

func fakeTestFaultJob(
	nodeNames []string, jobRankIds []string, faultTasks []FaultTask, jobName string, nameSpace string) *FaultJob {
	updateTime := int64(fakeTime2)
	return &FaultJob{
		ReScheduleKey: JobGraceRescheduleLabelValue,
		IsFaultJob:    true,
		JobName:       jobName,
		JobUID:        api.JobID(nameSpace + `/` + jobName),
		JobNamespace:  nameSpace,
		FaultTasks:    faultTasks,
		UpdateTime:    updateTime,
	}
}

func fakeReSchedulerCache() *DealReSchedulerCache {
	nodeNames := []string{"ubuntu1", "ubuntu2", "ubuntu3", "ubuntu4"}
	nodeRankIds := []string{"0", "1", "2", "3"}
	taskNames := []string{"task1", "task2", "task3", "task4"}
	jobRankIds := []string{"0", "10", "18", "27"}
	allCard := []string{"Ascend910-0", "Ascend910-1", "Ascend910-2", "Ascend910-3", "Ascend910-4",
		"Ascend910-5", "Ascend910-6", "Ascend910-7"}
	nameSpace := "vcjob"
	jobName := "job1"
	faultTasks := []FaultTask{
		*fakeTestFaultTaskHealth(taskNames[zero], nameSpace, nodeNames[zero], nodeRankIds[zero], "pod1"),
		*fakeTestFaultTaskFault(taskNames[one], nameSpace, nodeNames[one], nodeRankIds[one], "pod2"),
		*fakeTestFaultTaskHealth(taskNames[two], nameSpace, nodeNames[two], nodeRankIds[two], "pod3"),
		*fakeTestFaultTaskFault(taskNames[three], nameSpace, nodeNames[three], nodeRankIds[three], "pod4"),
	}
	return &DealReSchedulerCache{
		FaultNodes: map[string]*FaultNode{
			nodeNames[zero]:  fakeTestFaultNodeNodeHealthy(nodeNames[zero]),
			nodeNames[one]:   fakeTestFaultNodeCardUnhealthy(nodeNames[one], allCard),
			nodeNames[two]:   fakeTestFaultNodeNodeHealthy(nodeNames[two]),
			nodeNames[three]: fakeTestFaultNodeNodeUnhealthy(nodeNames[three]),
		},
		FaultJobs: map[api.JobID]*FaultJob{
			api.JobID(nameSpace + `/` + jobName): fakeTestFaultJob(nodeNames, jobRankIds, faultTasks, jobName, nameSpace),
		},
	}
}

func fakeNPUNodeNilDeviceInfo(name string) *plugin.NPUNode {
	nodeInfo := test.FakeNormalTestNode(name)
	return &plugin.NPUNode{
		CommonNode: plugin.CommonNode{
			Name:       name,
			Capability: nodeInfo.Capability.ScalarResources,
			Allocate:   nodeInfo.Allocatable.ScalarResources,
			Idle:       nodeInfo.Idle.ScalarResources,
			Annotation: nodeInfo.Node.Annotations,
			Label:      nodeInfo.Node.Labels,
		},
	}
}

func fakeNPUNodeWithDeviceInfo(name string) *plugin.NPUNode {
	anno := map[string]string{
		util.NPU910CardName:                              "Ascend910-0,Ascend910-1,Ascend910-2",
		util.NPU910CardName + "-" + CardUnhealthy:        "Ascend910-1",
		util.NPU910CardName + "-" + CardNetworkUnhealthy: "Ascend910-2",
	}
	nodeInfo := test.FakeNormalTestNode(name)
	npuNode := &plugin.NPUNode{
		CommonNode: plugin.CommonNode{
			Name:       name,
			Capability: nodeInfo.Capability.ScalarResources,
			Allocate:   nodeInfo.Allocatable.ScalarResources,
			Idle:       nodeInfo.Idle.ScalarResources,
			Annotation: anno,
			Label:      nodeInfo.Node.Labels,
		},
	}
	return npuNode
}

type ReSchedulerAddFaultJobWithSessionArgs struct {
	jobs        map[api.JobID]*api.JobInfo
	cardName    string
	cardPreName string
}

type ReSchedulerAddFaultJobWithSessionTests struct {
	fields  *ReScheduler
	name    string
	args    ReSchedulerAddFaultJobWithSessionArgs
	wantErr bool
}

func fakeCacheNoneFJobReSchedulerAddFaultJobWithSession() *DealReSchedulerCache {
	reCache := DealReSchedulerCache{
		FaultNodes: map[string]*FaultNode{
			"node0": {
				NodeName:            "node0",
				UpdateTime:          fakeTime,
				UnhealthyNPU:        []string{"Ascend910-0"},
				NetworkUnhealthyNPU: nil,
				IsFaultNode:         true,
				NodeDEnable:         true,
				NodeHealthState:     NodeCardUnhealthy,
				FaultCards: []FaultCard{
					*fakeTestFaultCardUnhealthy("Ascend910-0", "node0", NodeCardUnhealthy),
					*fakeTestFaultCardHealthy("Ascend910-1", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-2", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-3", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-4", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-5", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-6", "node0"),
					*fakeTestFaultCardHealthy("Ascend910-7", "node0"),
				},
			},
		},
		FaultJobs: map[api.JobID]*FaultJob{},
	}
	return &reCache
}

func fakeFaultTask2P(ns string, name string, node string, job string, index string) FaultTask {
	fTask := FaultTask{
		IsFaultTask:   true,
		TaskUID:       api.TaskID(`"` + ns + `"` + `"` + name + `"`),
		TaskName:      name,
		TaskNamespace: ns,
		NodeName:      node,
		NodeRankIndex: index,
		UseCardName:   []string{"Ascend910-0", "Ascend910-1"},
		PodCreateTime: fakeTime,
	}
	return fTask
}

func fakeFaultJob() FaultJob {
	return FaultJob{
		ReScheduleKey: "grace",
		IsFaultJob:    true,
		JobName:       "job0",
		JobUID:        "vcjob/job0",
		JobNamespace:  "test",
		FaultTasks: []FaultTask{
			fakeFaultTask2P("vcjob", "pod0", "node0", "job0", "0"),
			fakeFaultTask2P("vcjob", "pod1", "node1", "job0", "1"),
		},
		UpdateTime: test.FakeUpdateTime,
	}
}

func fakeCacheWithFJobReSchedulerAddFaultJobWithSession() *DealReSchedulerCache {
	reCache := fakeCacheNoneFJobReSchedulerAddFaultJobWithSession()
	fakeJob := fakeFaultJob()
	reCache.FaultJobs = map[api.JobID]*FaultJob{fakeJob.JobUID: &fakeJob}
	return reCache
}

func reAddFaultJobWithSessionModifyJobInfo(jobInfos map[api.JobID]*api.JobInfo) map[api.JobID]*api.JobInfo {
	jobInfos["vcjob/job0"].PodGroup.Labels = map[string]string{JobRescheduleLabelKey: "grace"}
	jobInfos["vcjob/job1"].PodGroup.Labels = map[string]string{JobRescheduleLabelKey: "grace"}
	jobInfos["vcjob/job0"].Tasks[test.FakeTaskName1].Pod.Annotations =
		map[string]string{podRankIndex: "0", util.NPU910CardName: "Ascend910-0,Ascend910-1"}
	jobInfos["vcjob/job0"].Tasks[test.FakeTaskName1].Pod.Annotations =
		map[string]string{podRankIndex: "1", util.NPU910CardName: "Ascend910-0,Ascend910-1"}
	jobInfos["vcjob/job1"].Tasks[test.FakeTaskName1].Pod.Annotations =
		map[string]string{podRankIndex: "2", util.NPU910CardName: "Ascend910-0,Ascend910-1"}
	jobInfos["vcjob/job1"].Tasks[test.FakeTaskName1].Pod.Annotations =
		map[string]string{podRankIndex: "3", util.NPU910CardName: "Ascend910-0,Ascend910-1"}
	jobInfos["vcjob/job1"].Tasks[test.FakeTaskName0].NodeName = "node3"
	jobInfos["vcjob/job1"].Tasks[test.FakeTaskName1].NodeName = "node4"
	return jobInfos
}

func reCreateNPUTask910(name, namespace string, reqResourceNum int) util.NPUTask {
	return util.NPUTask{
		Name:       namespace + "/" + name,
		ReqNPUName: "huawei.com/Ascend910",
		ReqNPUNum:  reqResourceNum,
	}
}

func addNPUTaskToNPUJob(npuJob plugin.SchedulerJob, taskName, taskNamespace string, reqNPUNum int) plugin.SchedulerJob {
	task := reCreateNPUTask910(taskName, taskNamespace, reqNPUNum)
	npuJob.Tasks[api.TaskID(taskName)] = task
	npuJob.ReqNPUNum += reqNPUNum
	return npuJob
}

func reCreateSchedulerJob910(namespace string, UID api.JobID) plugin.SchedulerJob {
	sJob := plugin.SchedulerJob{
		SchedulerJobAttr: util.SchedulerJobAttr{
			ComJob: util.ComJob{
				Name:      UID,
				NameSpace: namespace,
				Selector:  nil,
				Label:     nil,
			},
			NPUJob: &util.NPUJob{
				ReqNPUName: "huawei.com/Ascend910",
				ReqNPUNum:  zero,
				Tasks:      make(map[api.TaskID]util.NPUTask, util.MapInitNum),
			},
		},
	}
	return sJob
}

func reNewReScheduler(graceTime int64) *ReScheduler {
	fakeReScheduler := ReScheduler{
		GraceDeleteTime:      graceTime,
		Jobs:                 nil,
		Nodes:                nil,
		DealReSchedulerCache: nil,
	}
	return &fakeReScheduler
}

func addReCacheToReScheduler(reScheduler *ReScheduler, reCache *DealReSchedulerCache) {
	reScheduler.DealReSchedulerCache = reCache
}

func addSchedulerJobToReScheduler(reScheduler *ReScheduler, sJob map[api.JobID]plugin.SchedulerJob) {
	reScheduler.Jobs = sJob
}

func buildReSchedulerAddFaultJobWithSession() []ReSchedulerAddFaultJobWithSessionTests {
	jobInfos1 := map[api.JobID]*api.JobInfo{
		"vcjob/job0": test.FakeNormalTestJob("job0", util.NPUIndex2),
		"vcjob/job1": test.FakeNormalTestJob("job1", util.NPUIndex2),
	}
	jobInfos1 = reAddFaultJobWithSessionModifyJobInfo(jobInfos1)
	jobs11 := reCreateSchedulerJob910("vcjob", "job0")
	jobs11 = addNPUTaskToNPUJob(jobs11, "task0", "vcjob", util.NPUIndex4)
	jobs11 = addNPUTaskToNPUJob(jobs11, "task1", "vcjob", util.NPUIndex4)
	jobs12 := reCreateSchedulerJob910("vcjob", "job1")
	jobs12 = addNPUTaskToNPUJob(jobs12, "task3", "vcjob", util.NPUIndex4)
	jobs12 = addNPUTaskToNPUJob(jobs12, "task4", "vcjob", util.NPUIndex4)
	jobs1 := map[api.JobID]plugin.SchedulerJob{
		"vcjob/job0": jobs11,
		"vcjob/job1": jobs12,
	}

	reCache1 := fakeCacheNoneFJobReSchedulerAddFaultJobWithSession()
	reScheduler1 := reNewReScheduler(0)
	addSchedulerJobToReScheduler(reScheduler1, jobs1)
	addReCacheToReScheduler(reScheduler1, reCache1)

	reCache2 := fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
	reScheduler2 := reNewReScheduler(DefaultGraceOverTime)
	addSchedulerJobToReScheduler(reScheduler2, jobs1)
	addReCacheToReScheduler(reScheduler2, reCache2)
	test1 := ReSchedulerAddFaultJobWithSessionTests{
		name:   "01-AddFaultJobWithSession()-GraceDeleteTime 0",
		fields: reScheduler1,
		args: ReSchedulerAddFaultJobWithSessionArgs{
			jobs:        jobInfos1,
			cardName:    "huawei.com/Ascend910",
			cardPreName: "Ascend910-",
		},
		wantErr: false,
	}
	test2 := ReSchedulerAddFaultJobWithSessionTests{
		name:   "02-AddFaultJobWithSession()-GraceDeleteTime 900",
		fields: reScheduler2,
		args: ReSchedulerAddFaultJobWithSessionArgs{
			jobs:        jobInfos1,
			cardName:    "",
			cardPreName: "",
		},
		wantErr: false,
	}
	tests := []ReSchedulerAddFaultJobWithSessionTests{
		test1,
		test2,
	}
	return tests
}

// TestReSchedulerAddFaultJobWithSession test for add fault job
func TestReSchedulerAddFaultJobWithSession(t *testing.T) {
	env := plugin.ScheduleEnv{}
	env.SuperPodInfo = plugin.NewSuperPodInfo()
	tests := buildReSchedulerAddFaultJobWithSession()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reScheduler := tt.fields
			if err := reScheduler.AddFaultJobWithSession(tt.args.jobs, env); (err != nil) != tt.wantErr {
				t.Errorf("AddFaultJobWithSession() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type FaultNodeGetUnhealthyCardsFromDeviceInfoArgs struct {
	node     *plugin.NPUNode
	cardName string
}

type FaultNodeGetUnhealthyCardsFromDeviceInfoTests struct {
	fields  *FaultNode
	name    string
	args    FaultNodeGetUnhealthyCardsFromDeviceInfoArgs
	want    []string
	wantErr bool
}

func buildFaultNodeGetUnhealthyCardsFromDeviceInfoTests() []FaultNodeGetUnhealthyCardsFromDeviceInfoTests {
	node2 := fakeNPUNodeWithDeviceInfo("node0")
	test1 := FaultNodeGetUnhealthyCardsFromDeviceInfoTests{
		name:   "01-FaultNodeGetUnhealthyCardsFromDeviceInfoTests() nil device info",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetUnhealthyCardsFromDeviceInfoArgs{
			node:     fakeNPUNodeNilDeviceInfo("node0"),
			cardName: util.NPU910CardName,
		},
		want:    nil,
		wantErr: true,
	}
	test2 := FaultNodeGetUnhealthyCardsFromDeviceInfoTests{
		name:   "02-FaultNodeGetUnhealthyCardsFromDeviceInfoTests() succeed",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetUnhealthyCardsFromDeviceInfoArgs{
			node:     node2,
			cardName: util.NPU910CardName,
		},
		want:    []string{"Ascend910-1"},
		wantErr: false,
	}
	tests := []FaultNodeGetUnhealthyCardsFromDeviceInfoTests{
		test1,
		test2,
	}
	return tests
}

// TestFaultNodeGetUnhealthyCardsFromDeviceInfo test for get unhealthy card
func TestFaultNodeGetUnhealthyCardsFromDeviceInfo(t *testing.T) {
	tests := buildFaultNodeGetUnhealthyCardsFromDeviceInfoTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fNode := tt.fields
			got, err := fNode.getUnhealthyCardsFromDeviceInfo(tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("getUnhealthyCardsFromDeviceInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getUnhealthyCardsFromDeviceInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoArgs struct {
	node     *plugin.NPUNode
	cardName string
}

type GetNetworkUnhealthyCardsFromDeviceInfoTests struct {
	fields  *FaultNode
	name    string
	args    FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoArgs
	want    []string
	wantErr bool
}

func buildFaultNodeGetNetworkUnhealthyCardsFromDeviceInfoTests() []GetNetworkUnhealthyCardsFromDeviceInfoTests {
	node2 := fakeNPUNodeWithDeviceInfo("node0")
	test1 := GetNetworkUnhealthyCardsFromDeviceInfoTests{
		name:   "01-FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoTests() nil device info",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoArgs{
			node:     fakeNPUNodeNilDeviceInfo("node0"),
			cardName: util.NPU910CardName,
		},
		want:    nil,
		wantErr: true,
	}
	test2 := GetNetworkUnhealthyCardsFromDeviceInfoTests{
		name:   "02-FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoTests() succeed",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetNetworkUnhealthyCardsFromDeviceInfoArgs{
			node:     node2,
			cardName: util.NPU910CardName,
		},
		want:    []string{"Ascend910-2"},
		wantErr: false,
	}
	tests := []GetNetworkUnhealthyCardsFromDeviceInfoTests{
		test1,
		test2,
	}
	return tests
}

// TestFaultNodeGetNetworkUnhealthyCardsFromDeviceInfo test for get network unhealthy card
func TestFaultNodeGetNetworkUnhealthyCardsFromDeviceInfo(t *testing.T) {
	tests := buildFaultNodeGetNetworkUnhealthyCardsFromDeviceInfoTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fNode := tt.fields
			got, err := fNode.getNetworkUnhealthyCardsFromDeviceInfo(tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("getNetworkUnhealthyCardsFromDeviceInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNetworkUnhealthyCardsFromDeviceInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func fakeTestFaultNodeCardNetworkUnhealthyOneCard(nodeName string) *FaultNode {
	updateTime := test.FakeUpdateTime + 1
	faultCards := fakeTestFaultCardsUnhealthy(nodeName, true)
	return &FaultNode{
		NodeName:            nodeName,
		NPUName:             util.NPU910CardName,
		UpdateTime:          updateTime,
		UnhealthyNPU:        nil,
		NetworkUnhealthyNPU: []string{"Ascend910-0"},
		IsFaultNode:         true,
		NodeDEnable:         true,
		NodeHealthState:     NodeCardNetworkUnhealthy,
		FaultCards:          faultCards,
	}
}

type FaultNodeNewFaultCardHandlersArgs struct {
	node     *plugin.NPUNode
	cardName string
}

type FaultNodeNewFaultCardHandlersTests struct {
	fields *FaultNode
	name   string
	args   FaultNodeNewFaultCardHandlersArgs
	want   []FaultCard
}

func faultNodeNewFaultCardHandlersFaultCard(isFault bool, name, nodeName, faultType string) FaultCard {
	return FaultCard{
		IsFaultCard: isFault,
		NPUName:     name,
		FaultType:   faultType,
	}
}

func buildFaultNodeNewFaultCardHandlers() []FaultNodeNewFaultCardHandlersTests {
	test1 := FaultNodeNewFaultCardHandlersTests{
		name:   "01-newFaultCardHandlers()-NodeUnhealthy",
		fields: fakeTestFaultNodeNodeHealthyOneCard("node0"),
		args: FaultNodeNewFaultCardHandlersArgs{
			node: fakeNPUNodeWithDeviceInfo("node0"),
		},
		want: []FaultCard{
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-0", "node0", "Healthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-1", "node0", "Healthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-2", "node0", "Healthy"),
		},
	}
	test2 := FaultNodeNewFaultCardHandlersTests{
		name:   "02-newFaultCardHandlers()-CardUnhealthy",
		fields: fakeTestFaultNodeCardUnhealthy("node0", []string{"Ascend910-0"}),
		args: FaultNodeNewFaultCardHandlersArgs{
			node: fakeNPUNodeWithDeviceInfo("node0"),
		},
		want: []FaultCard{
			faultNodeNewFaultCardHandlersFaultCard(true, "Ascend910-0", "node0", "Unhealthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-1", "node0", "Healthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-2", "node0", "Healthy"),
		},
	}
	test3 := FaultNodeNewFaultCardHandlersTests{
		name:   "03-newFaultCardHandlers()-CardNetworkUnhealthy",
		fields: fakeTestFaultNodeCardNetworkUnhealthyOneCard("node0"),
		args: FaultNodeNewFaultCardHandlersArgs{
			node: fakeNPUNodeWithDeviceInfo("node0"),
		},
		want: []FaultCard{
			faultNodeNewFaultCardHandlersFaultCard(true, "Ascend910-0", "node0", "NetworkUnhealthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-1", "node0", "Healthy"),
			faultNodeNewFaultCardHandlersFaultCard(false, "Ascend910-2", "node0", "Healthy"),
		},
	}
	tests := []FaultNodeNewFaultCardHandlersTests{
		test1,
		test2,
		test3,
	}
	return tests
}

// TestFaultNodeNewFaultCardHandlers test for new fault card handlers
func TestFaultNodeNewFaultCardHandlers(t *testing.T) {
	tests := buildFaultNodeNewFaultCardHandlers()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fNode := tt.fields
			got := fNode.createFaultCardHandlers(tt.args.node)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newFaultCardHandlers() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type FaultNodeUpdateFaultNodesAttrArgs struct {
	node *plugin.NPUNode
}

type FaultNodeUpdateFaultNodesAttrTests struct {
	name   string
	fields FaultNode
	args   FaultNodeUpdateFaultNodesAttrArgs
	want   bool
}

func buildFaultNodeUpdateFaultNodesAttrTestCases() []FaultNodeUpdateFaultNodesAttrTests {
	allCard := []string{"Ascend910-0", "Ascend910-1", "Ascend910-2", "Ascend910-3", "Ascend910-4",
		"Ascend910-5", "Ascend910-6", "Ascend910-7"}
	test1 := FaultNodeUpdateFaultNodesAttrTests{
		name:   "node0",
		fields: *fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeUpdateFaultNodesAttrArgs{
			fakeNPUNodeNilDeviceInfo("node0"),
		},
	}
	test2 := FaultNodeUpdateFaultNodesAttrTests{
		name:   "node1",
		fields: *fakeTestFaultNodeNodeUnhealthy("node1"),
		args: FaultNodeUpdateFaultNodesAttrArgs{
			fakeNPUNodeNilDeviceInfo("node1"),
		},
		want: false,
	}
	test3 := FaultNodeUpdateFaultNodesAttrTests{
		name:   "node2",
		fields: *fakeTestFaultNodeCardUnhealthy("node2", allCard),
		args: FaultNodeUpdateFaultNodesAttrArgs{
			fakeNPUNodeNilDeviceInfo("node2"),
		},
		want: false,
	}
	test4 := FaultNodeUpdateFaultNodesAttrTests{
		name:   "node3",
		fields: *fakeTestFaultNodeNodeUnhealthy("node3"),
		args: FaultNodeUpdateFaultNodesAttrArgs{
			fakeNPUNodeNilDeviceInfo("node3"),
		},
		want: false,
	}
	tests := []FaultNodeUpdateFaultNodesAttrTests{
		test1,
		test2,
		test3,
		test4,
	}
	return tests
}

// TestFaultNodeUpdateFaultNodesAttr test fault node attribute
func TestFaultNodeUpdateFaultNodesAttr(t *testing.T) {
	tests := buildFaultNodeUpdateFaultNodesAttrTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fNode := *fakeTestFaultNodeNodeHealthy("node0")
			fNode.updateFaultNodesAttr(tt.args.node)
			if fNode.IsFaultNode != tt.want {
				t.Errorf("updateFaultNodesAttr() error = %v, wantErr %v", fNode.IsFaultNode, tt.want)
			}
		})
	}
}

type TestReScheduler struct {
	DealReSchedulerCache *DealReSchedulerCache
	GraceDeleteTime      int64
	Level                string
	Jobs                 map[api.JobID]plugin.SchedulerJob
	Nodes                map[string]plugin.NPUNode
	kubeClient           kubernetes.Interface
}

type ReSchedulerCheckNodeNPUByTaskArgs struct {
	task   *api.TaskInfo
	vcNode plugin.NPUNode
}

type ReSchedulerCheckNodeNPUByTaskTests struct {
	name    string
	npuName string
	fields  TestReScheduler
	args    ReSchedulerCheckNodeNPUByTaskArgs
	wantErr error
}

func buildReSchedulerCheckNodeNPUByTaskTests() []ReSchedulerCheckNodeNPUByTaskTests {
	faultNode := fakeTestFaultNodeNodeUnhealthy("node0")
	faultTask00 := fakeTestFaultTaskFault("pod0", "vcjob", "node0", "0", "pppp")
	faultTask01 := fakeTestFaultTaskHealth("pod1", "vcjob", "node1", "1", "oooo")
	faultJob0 := fakeTestFaultJob([]string{"node0", "node1"}, []string{"0", "1"}, []FaultTask{*faultTask00,
		*faultTask01}, "job0", "vcjob")
	field1 := TestReScheduler{
		DealReSchedulerCache: &DealReSchedulerCache{
			FaultNodes: map[string]*FaultNode{faultNode.NodeName: faultNode},
			FaultJobs:  map[api.JobID]*FaultJob{faultJob0.JobUID: faultJob0},
		},
		GraceDeleteTime: 0,
		Level:           "",
		Jobs:            nil,
		Nodes:           nil,
		kubeClient:      nil,
	}
	arg1 := ReSchedulerCheckNodeNPUByTaskArgs{
		task: test.FakeNormalTestTask("pod1", "node1", "job0"),
		vcNode: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name: "node2",
			},
		},
	}
	test1 := ReSchedulerCheckNodeNPUByTaskTests{
		name:    "01-CheckNodeNPUByTaskTests()-old task bind to new pod should be abandoned",
		npuName: util.NPU910CardName,
		fields:  field1,
		args:    arg1,
		wantErr: errors.New("task corresponding job not in session"),
	}
	tests := []ReSchedulerCheckNodeNPUByTaskTests{
		test1,
	}
	return tests
}

// TestReSchedulerCheckNodeNPUByTask test for re-scheduler check node NPU
func TestReSchedulerCheckNodeNPUByTask(t *testing.T) {
	tests := buildReSchedulerCheckNodeNPUByTaskTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reScheduler := fakeTestTTReScheduler(tt.fields)
			if err := reScheduler.CheckNodeNPUByTask(tt.args.task, &tt.args.vcNode); !reflect.DeepEqual(err,
				tt.wantErr) {
				t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type ReSchedulerCheckNodeCurNodeIsFaultArgs struct {
	curFJob *FaultJob
	task    *api.TaskInfo
	vcNode  plugin.NPUNode
}

type ReSchedulerCheckNodeCurNodeIsFaultTests struct {
	name    string
	fields  TestReScheduler
	args    ReSchedulerCheckNodeCurNodeIsFaultArgs
	wantErr bool
}

func buildReSchedulerCheckNodeCurNodeIsFaultTests() []ReSchedulerCheckNodeCurNodeIsFaultTests {
	test1 := ReSchedulerCheckNodeCurNodeIsFaultTests{
		name: "01-checkNodeCurNodeIsFault()-succeed",
		fields: TestReScheduler{
			DealReSchedulerCache: &DealReSchedulerCache{
				FaultNodes: map[string]*FaultNode{"node0": fakeTestFaultNodeNodeUnhealthy("node0")},
				FaultJobs:  nil,
			},
		},
		args: ReSchedulerCheckNodeCurNodeIsFaultArgs{
			curFJob: &FaultJob{
				FaultTasks: []FaultTask{
					{
						TaskName: "pod0",
					},
				},
			},
			vcNode: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Name: "node0",
				},
			},
			task: &api.TaskInfo{
				Name: "pod0",
			},
		},
		wantErr: true,
	}
	tests := []ReSchedulerCheckNodeCurNodeIsFaultTests{
		test1,
	}
	return tests
}

// TestReSchedulerCheckNodeCurNodeIsFault test for check current node is fault node
func TestReSchedulerCheckNodeCurNodeIsFault(t *testing.T) {
	tests := buildReSchedulerCheckNodeCurNodeIsFaultTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reScheduler := fakeTestTTReScheduler(tt.fields)
			if err := reScheduler.checkNodeCurNodeIsFault(&tt.args.vcNode, tt.args.task); (err != nil) != tt.wantErr {
				t.Errorf("checkNodeCurNodeIsFault() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func fakeTestTTReScheduler(fields TestReScheduler) *ReScheduler {
	return &ReScheduler{
		DealReSchedulerCache: fields.DealReSchedulerCache,
		GraceDeleteTime:      fields.GraceDeleteTime,
		Jobs:                 fields.Jobs,
		Nodes:                fields.Nodes,
	}
}

func mockJobInfo(jobName string, taskNum int) *api.JobInfo {
	jobInfo := test.FakeNormalTestJob(jobName, taskNum)
	if taskNum < minTestSliceLen || taskNum > maxTestSliceLen {
		return jobInfo
	}
	var minRes = make(v1.ResourceList, taskNum)
	for _, task := range jobInfo.Tasks {
		for k, v := range task.Resreq.ScalarResources {
			minRes[k] = resource.MustParse(fmt.Sprintf("%f", v))
		}
	}
	jobInfo.PodGroup.Spec.MinResources = &minRes
	return jobInfo
}

func TestGetRunningJobs(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	ssn := test.FakeNormalSSN(nil)
	jobInfo1 := mockJobInfo(mockJobName1, test.NPUIndex4)
	jobInfo1.PodGroup.Status.Phase = util.PodGroupPending
	ssn.Jobs[jobInfo1.UID] = jobInfo1
	jobInfo2 := mockJobInfo(mockJobName2, test.NPUIndex4)
	ssn.Jobs[jobInfo2.UID] = jobInfo2
	reScheduler.Jobs = map[api.JobID]plugin.SchedulerJob{
		jobInfo2.UID: {SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{}}},
	}
	t.Run("01-GetRunningJobs() return nil when no running jobs", func(t *testing.T) {
		if jobs := reScheduler.GetRunningJobs(ssn); len(jobs) != 0 {
			t.Errorf("GetRunningJobs() len error = %v， wantErr is 0", len(jobs))
		}
	})
}

func TestGetTaskRestartReason(t *testing.T) {
	t.Run("01-GetTaskRestartReason() return non-empty string when json marshal success",
		func(t *testing.T) {
			if res := GetTaskRestartReason([]FaultReasonList{}); res == "" {
				t.Errorf("GetTaskRestartReason() res = %v, wantRes is non-empty string", res)
			}
		})
	t.Run("02-GetTaskRestartReason() return empty string when json marshal failed",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Marshal, func(_ interface{}) ([]byte, error) {
				return nil, errors.New("json marshal failed")
			})
			defer patch.Reset()
			if res := GetTaskRestartReason(nil); res != "" {
				t.Errorf("GetTaskRestartReason() res = %v, wantRes is empty string", res)
			}
		})
}

func TestGetGraceDeleteFaultJobs(t *testing.T) {
	t.Run("01-getGraceDeleteFaultJobs() return not nil when fault jobs with grace label",
		func(t *testing.T) {
			reScheduler := fakeTestTTReScheduler(TestReScheduler{})
			reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
			if res := reScheduler.getGraceDeleteFaultJobs(); res == nil {
				t.Errorf("getGraceDeleteFaultJobs() res = %v, wantRes is not nil", res)
			}
		})
}

func TestGetNeedForceDeleteDelayingNPUJobs(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	t.Run("01-GetNeedForceDeleteDelayingNPUJobs() return error when schedulerJobs or ssn is nil",
		func(t *testing.T) {
			_, err := reScheduler.GetNeedForceDeleteDelayingNPUJobs(nil, nil)
			if err == nil {
				t.Errorf("GetNeedForceDeleteDelayingNPUJobs() err = %v, wantErr is not nil", err)
			}
		})
	t.Run("02-GetNeedForceDeleteDelayingNPUJobs() return error when get none jobs", func(t *testing.T) {
		reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
		ssn := test.FakeNormalSSN(nil)
		jobInfo := mockJobInfo("job0", test.NPUIndex4)
		ssn.Jobs[jobInfo.UID] = jobInfo
		schedulerJobs := map[api.JobID]plugin.SchedulerJob{
			jobInfo.UID: {SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{}}},
		}
		_, err := reScheduler.GetNeedForceDeleteDelayingNPUJobs(schedulerJobs, ssn)
		if err == nil {
			t.Errorf("GetNeedForceDeleteDelayingNPUJobs() err = %v, wantErr is not nil", err)
		}
	})
}

func TestIsDelayingJobTimeout(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	reScheduler.GraceDeleteTime = graceDeleteTime
	t.Run("01-isDelayingJobTimeout() return true when job is timeout", func(t *testing.T) {
		fJob := &FaultJob{UpdateTime: test.FakeUpdateTime}
		if res := reScheduler.isDelayingJobTimeout(fJob); !res {
			t.Errorf("isDelayingJobTimeout() res = %v, wantRes is true", res)
		}
	})
	t.Run("02-isDelayingJobTimeout() return true when job is not timeout", func(t *testing.T) {
		fJob := &FaultJob{UpdateTime: time.Now().Unix()}
		if res := reScheduler.isDelayingJobTimeout(fJob); res {
			t.Errorf("isDelayingJobTimeout() res = %v, wantRes is false", res)
		}
	})
}

func TestRestartNeedForceDeleteJobs(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	ssn := test.FakeNormalSSN(nil)
	t.Run("01-RestartNeedForceDeleteJobs() return error when ssn is nil",
		func(t *testing.T) {
			err := reScheduler.RestartNeedForceDeleteJobs(nil, plugin.ScheduleEnv{})
			if err == nil {
				t.Errorf("RestartNeedForceDeleteJobs() err = %v, wantErr is not nil", err)
			}
		})
	t.Run("02-RestartNeedForceDeleteJobs() return error when get none jobs", func(t *testing.T) {
		err := reScheduler.RestartNeedForceDeleteJobs(ssn, plugin.ScheduleEnv{})
		if err == nil {
			t.Errorf("RestartNeedForceDeleteJobs() err = %v, wantErr is not nil", err)
		}
	})
	t.Run("03-RestartNeedForceDeleteJobs() return nil when jobs restart success", func(t *testing.T) {
		patch1 := gomonkey.ApplyMethod(reflect.TypeOf(reScheduler), "GetNeedForceDeleteDelayingNPUJobs",
			func(_ *ReScheduler, _ map[api.JobID]plugin.SchedulerJob,
				_ *framework.Session) ([]plugin.SchedulerJob, error) {
				jobs := reCreateSchedulerJob910("", mockJobUID)
				return []plugin.SchedulerJob{jobs}, nil
			})
		defer patch1.Reset()
		patch2 := gomonkey.ApplyMethod(reflect.TypeOf(&FaultJob{}), "ForceDeleteJob",
			func(_ *FaultJob, _ *plugin.SchedulerJob, _ plugin.ScheduleEnv) error {
				return errors.New("force delete job failed")
			})
		defer patch2.Reset()
		reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
		reScheduler.FaultJobs = map[api.JobID]*FaultJob{"test-job": {FaultTasks: []FaultTask{}}}
		err := reScheduler.RestartNeedForceDeleteJobs(ssn, plugin.ScheduleEnv{})
		if err != nil {
			t.Errorf("RestartNeedForceDeleteJobs() err = %v, wantErr is not nil", err)
		}
	})
}

func TestRestartFaultJobs(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	ssn := test.FakeNormalSSN(nil)
	t.Run("01-RestartFaultJobs() return error when ssn is nil",
		func(t *testing.T) {
			err := reScheduler.RestartFaultJobs(nil, plugin.ScheduleEnv{})
			if err == nil {
				t.Errorf("RestartFaultJobs() err = %v, wantErr is not nil", err)
			}
		})
	t.Run("02-RestartFaultJobs() return nil when ssn is not nil and fault jobs restart success",
		func(t *testing.T) {
			reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
			err := reScheduler.RestartFaultJobs(ssn, plugin.ScheduleEnv{})
			if err != nil {
				t.Errorf("RestartFaultJobs() err = %v, wantErr is nil", err)
			}
		})
}

func TestUpdateRescheduleReason(t *testing.T) {
	t.Run("01-updateRescheduleReason() return nil when fJob is nil",
		func(t *testing.T) {
			res := updateRescheduleReason(nil, nil)
			if res != nil {
				t.Errorf("updateRescheduleReason() res = %v, wantRes is nil", res)
			}
		})
	t.Run("02-updateRescheduleReason() return not nil when fJob is not nil",
		func(t *testing.T) {
			fJob := &FaultJob{UpdateTime: test.FakeUpdateTime}
			res := updateRescheduleReason(nil, fJob)
			if res == nil {
				t.Errorf("updateRescheduleReason() res = %v, wantRes is not nil", res)
			}
		})
}

func createFaultTasks(count int) []FaultTask {
	if count < minTestSliceLen || count > maxTestSliceLen {
		return []FaultTask{}
	}
	faultTasks := make([]FaultTask, count)
	for i := range faultTasks {
		faultTasks[i].IsFaultTask = true
	}
	return faultTasks
}

func TestConvertFaultTaskToRecords(t *testing.T) {
	const numberOfFaultTasks = 10
	t.Run("01-convertFaultTaskToRecords() return empty slice when fJob is nil",
		func(t *testing.T) {
			res := convertFaultTaskToRecords(nil)
			if len(res) != 0 {
				t.Errorf("convertFaultTaskToRecords() res = %v, wantRes is empty slice", res)
			}
		})
	t.Run("02-convertFaultTaskToRecords() return slice when fJob is not nil",
		func(t *testing.T) {
			fJob := fakeFaultJob()
			fJob.FaultTasks = append(fJob.FaultTasks, FaultTask{})
			fJob.FaultTasks = append(fJob.FaultTasks, createFaultTasks(numberOfFaultTasks)...)
			res := convertFaultTaskToRecords(&fJob)
			if len(res) == 0 {
				t.Errorf("convertFaultTaskToRecords() res = %v, wantRes is non-empty slice", res)
			}
		})
}

func TestGetNewCacheJobs(t *testing.T) {
	t.Run("01-getNewCacheJobs() return slice when reScheduler.DealReSchedulerCache is not nil",
		func(t *testing.T) {
			reScheduler := fakeTestTTReScheduler(TestReScheduler{})
			reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
			if res := reScheduler.getNewCacheJobs(map[api.JobID]*FaultJob{}); res == nil {
				t.Errorf("getNewCacheJobs() res = %v, wantRes is non-empty slice", res)
			}
		})
}

func TestScoreBestNPUNodes(t *testing.T) {
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	reScheduler.DealReSchedulerCache = fakeCacheWithFJobReSchedulerAddFaultJobWithSession()
	taskInfo := &api.TaskInfo{}
	scoreMap := map[string]float64{"test": 1}
	wantScore := map[string]float64{"test": 1}
	t.Run("01-ScoreBestNPUNodes() scoreMap not change when task is nil",
		func(t *testing.T) {
			reScheduler.ScoreBestNPUNodes(nil, scoreMap)
			if !reflect.DeepEqual(scoreMap, wantScore) {
				t.Errorf("ScoreBestNPUNodes() scoreMap = %v, wantScore is %v", scoreMap, wantScore)
			}
		})
	t.Run("02-ScoreBestNPUNodes() scoreMap not change when fJob is nil",
		func(t *testing.T) {
			reScheduler.ScoreBestNPUNodes(taskInfo, scoreMap)
			if !reflect.DeepEqual(scoreMap, wantScore) {
				t.Errorf("ScoreBestNPUNodes() scoreMap = %v, wantScore is %v", scoreMap, wantScore)
			}
		})
	taskInfo.Job = mockJobUID
	t.Run("03-ScoreBestNPUNodes() scoreMap not change when add scores on scoreMap success",
		func(t *testing.T) {
			reScheduler.ScoreBestNPUNodes(taskInfo, scoreMap)
			if !reflect.DeepEqual(scoreMap, wantScore) {
				t.Errorf("ScoreBestNPUNodes() scoreMap = %v, wantScore is %v", scoreMap, wantScore)
			}

		})
	t.Run("04-ScoreBestNPUNodes() scoreMap not change when fJob.IsFaultJob is false",
		func(t *testing.T) {
			reScheduler.DealReSchedulerCache.FaultJobs = map[api.JobID]*FaultJob{
				mockJobUID: {JobUID: mockJobUID, IsFaultJob: false},
			}
			reScheduler.ScoreBestNPUNodes(taskInfo, scoreMap)
			if !reflect.DeepEqual(scoreMap, wantScore) {
				t.Errorf("ScoreBestNPUNodes() scoreMap = %v, wantScore is %v", scoreMap, wantScore)
			}
		})
}

func TestIsJobCanAssignToSubHealthNode(t *testing.T) {
	type args struct {
		nodeSubHealth        bool
		jobSubHealthStrategy string
	}
	testCases := []struct {
		name       string
		args       args
		wantResult bool
	}{
		{"01-isJobCanAssignToSubHealthNode return false when nodeSubHealth is true and" +
			" jobSubHealthStrategy is not ignore",
			args{true, util.SubHealthyGraceExit}, false},
		{"02-isJobCanAssignToSubHealthNode return true when nodeSubHealth is true and " +
			"jobSubHealthStrategy is ignore",
			args{true, util.SubHealthyIgnore}, true},
		{"03-isJobCanAssignToSubHealthNode return true when nodeSubHealth is false",
			args{false, ""}, true},
	}
	reScheduler := fakeTestTTReScheduler(TestReScheduler{})
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := reScheduler.isJobCanAssignToSubHealthNode(testCase.args.jobSubHealthStrategy, testCase.args.nodeSubHealth)
			if result != testCase.wantResult {
				t.Errorf("isJobCanAssignToSubHealthNode() result = %v, want %v", result, testCase.wantResult)
			}
		})
	}
}

func mockFaultDeviceList(index string, faultHandling string) FaultDeviceList {
	return FaultDeviceList{
		NPUName:       "npuName" + index,
		FaultHandling: faultHandling,
	}
}

func TestSetTaskFaultReasonByFaultNode(t *testing.T) {
	ftask := &FaultTask{UseCardName: []string{"npuName0"}}
	testCases := []struct {
		name       string
		fTask      *FaultTask
		fNode      *FaultNode
		wantResult bool
	}{
		{
			name: "01-setTaskFaultReasonByFaultNode return non-empty slice " +
				"when card name match and faulthandling is not NotHandleFault",
			fTask: ftask,
			fNode: &FaultNode{
				FaultDeviceList: []FaultDeviceList{
					mockFaultDeviceList("0", SubHealthFault),
				}},
			wantResult: true,
		},
		{
			name: "02-setTaskFaultReasonByFaultNode return empty slice " +
				"when card name not match or faulthandling is NotHandleFault",
			fTask: ftask,
			fNode: &FaultNode{
				FaultDeviceList: []FaultDeviceList{
					mockFaultDeviceList("0", NotHandleFault),
				}},
			wantResult: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			result := setTaskFaultReasonByFaultNode(testCase.fTask, testCase.fNode)
			if (len(result) != 0) != testCase.wantResult {
				t.Errorf("setTaskFaultReasonByFaultNode() result = %v, want is empty slice", result)
			}
		})
	}
}

func TestSetTaskCardHealthCode(t *testing.T) {
	testCases := []struct {
		name        string
		reScheduler *ReScheduler
		fTask       *FaultTask
		wantError   bool
	}{
		{
			name:        "01-setTaskCardHealthCode return error when task used node is nil",
			reScheduler: &ReScheduler{DealReSchedulerCache: &DealReSchedulerCache{}},
			fTask: &FaultTask{
				IsSoftwareFault: true,
				NodeName:        "",
			},
			wantError: true,
		},
		{
			name: "02-setTaskCardHealthCode return nil when get match node and node is unhealthy",
			reScheduler: &ReScheduler{DealReSchedulerCache: &DealReSchedulerCache{
				FaultNodes: map[string]*FaultNode{
					"nodeName0": {
						NodeName:        "nodeName0",
						NodeHealthState: NodeUnhealthy,
						FaultDeviceList: []FaultDeviceList{
							mockFaultDeviceList("0", NotHandleFault),
						}},
				},
			}},
			fTask: &FaultTask{
				IsSoftwareFault: true,
				NodeName:        "nodeName0",
			},
			wantError: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.reScheduler.setTaskCardHealthCode(testCase.fTask)
			if (err != nil) != testCase.wantError {
				t.Errorf("setTaskCardHealthCode() err = %v, want is nil", err)
			}
		})
	}
}
