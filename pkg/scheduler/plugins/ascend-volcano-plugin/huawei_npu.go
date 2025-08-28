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
Package main is using for HuaWei Ascend pin affinity schedule.
*/
package main

import (
	"fmt"
	"strings"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/rescheduling"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

var sHandler *plugin.ScheduleHandler

func init() {
	sHandler = HandlerStart()
}

// HandlerStart HuaWei NPU plugin start by frame.
func HandlerStart() *plugin.ScheduleHandler {
	scheduleHandler := &plugin.ScheduleHandler{
		NPUPlugins:  sets.String{util.NPU910CardName: {}, util.NPU310CardName: {}, util.NPU310PCardName: {}},
		FaultHandle: rescheduling.NewHandler(),
		ScheduleEnv: plugin.ScheduleEnv{
			FrameAttr:               plugin.NewVolcanoFrame(),
			JobScheduleInfoRecorder: plugin.NewJobScheduleInfoRecorder(),
			ClusterCache:            plugin.NewClusterCache(),
		},
	}
	scheduleHandler.PolicyBuilder = internal.New
	return scheduleHandler
}

// New return npu plugin.
func New(arguments framework.Arguments) framework.Plugin {
	return &huaweiNPUPlugin{Scheduler: sHandler, Arguments: arguments}
}

// Name This need by volcano frame init plugin.
func (tp *huaweiNPUPlugin) Name() string {
	return PluginName
}

// OnSessionOpen HuaWei NPU Action's init session for frame.
func (tp *huaweiNPUPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(util.LogInfoLev).Infof("enter %s OnSessionOpen.", PluginName)
	defer klog.V(util.LogInfoLev).Infof("leave %s OnSessionOpen.", PluginName)
	if tp == nil || ssn == nil {
		klog.V(util.LogInfoLev).Infof("OnSessionOpen : %s.", util.ArgumentError)
		return
	}
	// Init npu plugin and nodes.
	if err := tp.Scheduler.InitNPUSession(ssn); err != nil {
		klog.V(util.LogErrorLev).Infof("InitNPUSession : %s, npu plugin will not be initialized.", err)
		return
	}
	// check job npu resource, if illegal return failed
	addJobValidFn(ssn, tp)
	// if node not meet the task require, the task will be failed. so need to intercept in advance
	addPredicateFn(ssn, tp)

	addJobPipelinedFn(ssn, tp)

	addBatchNodeOrderFn(ssn, tp)

	addJobReadyFn(ssn, tp)

	addJobEnqueueableFn(ssn, tp)
	// Register event handlers to update task info in PodLister & nodeMap
	// for support Concurrency
	addEventHandler(ssn, tp)
}

// OnSessionClose Close session by volcano frame.
func (tp *huaweiNPUPlugin) OnSessionClose(ssn *framework.Session) {
	klog.V(util.LogInfoLev).Infof("enter %s OnSessionClose.", PluginName)
	defer klog.V(util.LogInfoLev).Infof("leave %s OnSessionClose.", PluginName)
	if tp == nil || ssn == nil {
		klog.V(util.LogInfoLev).Infof("OnSessionClose failed: %s.", util.ArgumentError)
		return
	}
	if *tp.Scheduler.FrameAttr.IsFirstSession {
		*tp.Scheduler.FrameAttr.IsFirstSession = false
	}
	// 1、Record job's unscheduled reason;
	// 2、Update job statue;
	// 3、Handle other post-dispatch issues.
	for _, job := range ssn.Jobs {
		// deal pending job
		if job.PodGroup.Status.Phase == util.PodGroupInqueue ||
			job.PodGroup.Status.Phase == util.PodGroupPending {
			// if all nodes not meet job require failed
			tp.Scheduler.SetJobPendReasonByNodesCase(job)
		}
	}
	tp.Scheduler.BeforeCloseHandler()
}

func addJobValidFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	// check job npu resource, if illegal return failed
	ssn.AddJobValidFn(tp.Name(), func(obj interface{}) *api.ValidateResult {
		return tp.Scheduler.JobValid(obj)
	})
}

func addPredicateFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	// check job npu resource, if illegal return failed
	ssn.AddPredicateFn(tp.Name(), func(taskInfo *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		predicateErr := tp.Scheduler.NodePredicate(taskInfo, nodeInfo)
		if predicateErr != nil {
			tp.Scheduler.Jobs[taskInfo.Job].Lock()
			vcJob := tp.Scheduler.Jobs[taskInfo.Job]
			vcJob.UpdateJobPendingMessage(predicateErr.Error(), nodeInfo.Name)
			tp.Scheduler.Jobs[taskInfo.Job].Unlock()
			klog.V(util.LogDebugLev).Infof("NodePredicate failed for task %s err:%s", taskInfo.Name, predicateErr)
			predicateErr = fmt.Errorf("node check failed. for details,log by search keywords <%s> in volcano's log",
				predicateErr.Error())
		}
		return predicateErr
	})
}

func addJobPipelinedFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	// check job npu resource, if illegal return failed
	ssn.AddJobPipelinedFn(tp.Name(), func(obj interface{}) int {
		ji, ok := obj.(*api.JobInfo)
		if !ok {
			klog.V(util.LogErrorLev).Info("obj assertion failed.")
			return util.Reject
		}

		job, ok := tp.Scheduler.Jobs[ji.UID]
		if !ok {
			return util.Abstain
		}
		if *job.JobReadyTag {
			return util.Abstain
		}
		return util.Reject
	})
}

func addBatchNodeOrderFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	ssn.AddBatchNodeOrderFn(tp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		score, err := tp.Scheduler.BatchNodeOrderFn(task, nodes)
		if err != nil {
			if setErr := tp.Scheduler.SetJobPendingReason(ssn.Jobs[task.Job], err.Error()); setErr != nil {
				klog.V(util.LogDebugLev).Infof("%s setJobFailed err:%s.", PluginName, util.SafePrint(setErr))
			}
		}
		return score, nil
	})
}

func addJobReadyFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	ssn.AddJobReadyFn(tp.Name(), func(obj interface{}) bool {
		ji, ok := obj.(*api.JobInfo)
		if !ok {
			klog.V(util.LogErrorLev).Info("obj assertion failed.")
			return false
		}
		job, ok := tp.Scheduler.Jobs[ji.UID]
		if !ok {
			return true
		}
		return *job.JobReadyTag
	})
}

func addEventHandler(ssn *framework.Session, tp *huaweiNPUPlugin) {
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			if event == nil {
				klog.V(util.LogErrorLev).Infof("AllocateFunc event nil.")
				return
			}
			tp.Scheduler.NPUAllocateFunc(event.Task)
		},
		DeallocateFunc: func(event *framework.Event) {
			if event == nil {
				klog.V(util.LogErrorLev).Infof("DeallocateFunc event nil.")
				return
			}
			tp.Scheduler.NPUDeallocateFunc(event.Task)
		},
	})
}

func addJobEnqueueableFn(ssn *framework.Session, tp *huaweiNPUPlugin) {
	ssn.AddJobEnqueueableFn(tp.Name(), func(job interface{}) int {
		if tp.Scheduler.NPUPlugins == nil {
			klog.V(util.LogErrorLev).Infof("AddJobEnqueueableFn : %s", util.ArgumentError)
			return util.JobEnqueueSkip
		}
		vcjob, ok := job.(*api.JobInfo)
		if !ok {
			return util.JobEnqueueSkip
		}
		npuName, rNpuNum, _ := plugin.GetVCJobReqNPUTypeFromJobInfo(vcjob)
		if !tp.Scheduler.NPUPlugins.Has(npuName) {
			return util.JobEnqueueSkip
		}
		tNpuNum := getNpuNum(ssn, tp, npuName)
		if tNpuNum < rNpuNum {
			klog.V(util.LogWarningLev).Infof("job <%s> Add enqueue failed, require npu num is %v "+
				"but cluster npu num is %v", vcjob.Name, rNpuNum, tNpuNum)
			return util.JobNotEnqueue
		}
		klog.V(util.LogWarningLev).Infof("job <%s> Add enqueue success will start schedule, require npu num is <%v> "+
			"and cluster npu num is <%v>.", vcjob.Name, rNpuNum, tNpuNum)
		return util.JobEnqueue
	})
}

func getNpuNum(ssn *framework.Session, tp *huaweiNPUPlugin, npuName string) int {
	var tNpuNum int
	for _, node := range ssn.Nodes {
		vcNode, ok := tp.Scheduler.Nodes[node.Name]
		if !ok {
			klog.V(util.LogErrorLev).Infof("AddJobEnqueueableFn add node failed,%s is not in cache", node.Name)
			continue
		}
		deviceInfo, ok := vcNode.Annotation[npuName]
		if !ok || len(deviceInfo) == 0 {
			klog.V(util.LogDebugLev).Infof("AddJobEnqueueableFn add node failed,"+
				"%s deviceList is empty", node.Name)
			continue
		}
		deviceList := strings.Split(deviceInfo, ",")
		klog.V(util.LogInfoLev).Infof("Add enqueue node %s deviceList is: %#v", vcNode.Name, deviceList)
		npuNum, ok := vcNode.Idle[v1.ResourceName(npuName)]
		if !ok || len(deviceList) != int(npuNum/util.NPUHexKilo) {
			klog.V(util.LogErrorLev).Infof("Add enqueue node %s device info is %v and k8s is %v", vcNode.Name,
				len(deviceList), int(npuNum/util.NPUHexKilo))
			continue
		}
		tNpuNum += len(deviceList)
	}
	return tNpuNum
}
