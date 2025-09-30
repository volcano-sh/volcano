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
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// InitVNPU init vnpu attr
func (tp *VirtualNPU) InitVNPU() {
	if tp == nil {
		klog.V(util.LogDebugLev).Infof("InitVNPU failed:%s", util.ArgumentError)
		return
	}
	tp.DynamicVNPU = DynamicVNPU{
		DowngradeCache: make(map[string][]string, util.MapInitNum),
	}
}

// PreStartAction pre start action for vnpu
func (tp *VirtualNPU) PreStartAction(env *plugin.ScheduleEnv, ssn *framework.Session) error {
	if ssn == nil || env == nil {
		return fmt.Errorf("prestart failed %v", util.ArgumentError)
	}
	tp.setVNPUTemplate(env)
	tp.setPresetVirtualDevices(env)
	tp.DowngradeCache = make(map[string][]string, util.MapInitNum)
	return tp.preStartDyVNPU(env, ssn)
}

// setPresetVirtualDevices get preset virtual devices
func (tp *VirtualNPU) setPresetVirtualDevices(env *plugin.ScheduleEnv) {
	if tp == nil {
		klog.V(util.LogDebugLev).Infof("GetPresetVirtualDevices failed:%s", util.ArgumentError)
		return
	}
	tp.StaticByConf = env.FrameAttr.PresetVirtualDevice
}

func (tp *VirtualNPU) setVNPUTemplate(env *plugin.ScheduleEnv) {
	temp := tp.getEnvTemplate(env)
	if temp == "" {
		return
	}
	tp.VT = VTemplate{
		Data: env.FrameAttr.VJobTemplate[temp],
		Temp: temp,
	}
}

func (tp *VirtualNPU) getEnvTemplate(env *plugin.ScheduleEnv) string {
	if tp == nil {
		klog.V(util.LogDebugLev).Infof("GetVNPUTemplate failed:%s", util.ArgumentError)
		return ""
	}
	for _, node := range env.Nodes {
		if node.VNode.ChipKind == "" {
			continue
		}
		if node.VNode.ChipKind == plugin.Ascend310P {
			return node.VNode.ChipKind
		}
		if node.VNode.ChipKind == plugin.Ascend910 {
			return node.VNode.ChipType
		}
	}
	return ""
}

func (tp *VirtualNPU) preStartDyVNPU(env *plugin.ScheduleEnv, ssn *framework.Session) error {
	var reErrors []error

	reErrors = append(reErrors, tp.initConCache(env, ssn))
	reErrors = append(reErrors, tp.deleteDyCutErrTasks(env, ssn))

	return util.ConvertErrSliceToError(reErrors)
}

// ConCache format nodeName: templateName:taskUID
func (tp *VirtualNPU) initConCache(env *plugin.ScheduleEnv, ssn *framework.Session) error {
	nodes := make(map[string]map[string]map[api.TaskID]struct{}, util.MapInitNum)
	for jobID, vJob := range env.Jobs {
		jobInf, jobOk := ssn.Jobs[jobID]
		if !jobOk {
			klog.V(util.LogErrorLev).Infof("initConCache %s not in ssn.", jobID)
			continue
		}
		if initErr := initDyCutConCacheByJobInfo(nodes, jobInf, vJob); initErr != nil {
			continue
		}
	}
	tp.DynamicVNPU.ConCache = nodes
	return nil
}

func (tp *VirtualNPU) deleteDyCutErrTasks(env *plugin.ScheduleEnv, ssn *framework.Session) error {
	nTasks := tp.getAllNeedRestartDyTasks(env, ssn)
	if len(nTasks) == 0 {
		return nil
	}
	for _, nT := range nTasks {
		if nT.VTask == nil {
			klog.V(util.LogErrorLev).Infof("deleteDyCutErrTasks vTask %s is nil.", nT.Name)
			continue
		}
		if delErr := nT.ForceDeletePodByTaskInf(ssn, DyCutFailedError, nT.VTask.Allocated.NodeName); delErr != nil {
			klog.V(util.LogErrorLev).Infof("ForceDeletePodByTaskInf %s: %s.", nT.Name, delErr)
		}
	}
	return nil
}

func (tp *VirtualNPU) getAllNeedRestartDyTasks(env *plugin.ScheduleEnv, ssn *framework.Session) []util.NPUTask {
	vJobs := tp.getAllDyJobs(env)
	if len(vJobs) == 0 {
		return nil
	}
	return tp.getRestartDyTasksFromJobs(vJobs, ssn)
}

func (tp *VirtualNPU) getAllDyJobs(env *plugin.ScheduleEnv) map[api.JobID]plugin.SchedulerJob {
	jobMap := make(map[api.JobID]plugin.SchedulerJob, util.MapInitNum)
	for jobID, vJob := range env.Jobs {
		if vJob.ReqNPUName == util.AscendNPUCore {
			jobMap[jobID] = vJob
		}
	}
	return jobMap
}

func (tp *VirtualNPU) getRestartDyTasksFromJobs(vJobs map[api.JobID]plugin.SchedulerJob,
	ssn *framework.Session) []util.NPUTask {
	vTasks := getFailedDyTasksFromJobs(vJobs)
	fTIDs := getDyFailedTasksFromFailed(ssn, vTasks)
	if len(fTIDs) == 0 {
		return nil
	}
	var nSlice []util.NPUTask
	for _, tID := range fTIDs {
		vT, ok := vTasks[tID]
		if !ok {
			klog.V(util.LogErrorLev).Infof("getRestartDyTasksFromJobs taskID(%s) not found.", tID)
			continue
		}
		nSlice = append(nSlice, vT)
	}
	return nSlice
}

func getFailedDyTasksFromJobs(vJobs map[api.JobID]plugin.SchedulerJob) map[api.TaskID]util.NPUTask {
	vTasks := make(map[api.TaskID]util.NPUTask, util.MapInitNum)
	for _, vJob := range vJobs {
		for tID, vTask := range vJob.Tasks {
			if vTask.Status == util.TaskStatusAllocate || vTask.Status == util.TaskStatusFailed {
				vTasks[tID] = vTask
			}
		}
	}
	return vTasks
}

func getDyFailedTasksFromFailed(ssn *framework.Session, vT map[api.TaskID]util.NPUTask) []api.TaskID {
	if len(vT) == 0 {
		return nil
	}
	nsMap := getDyFailedNamespaces(vT)

	allIDS := getAllDyFailedTasks(ssn, nsMap)

	return getDyFailedTaskIDsInFaileds(allIDS, vT)
}

func getAllDyFailedTasks(ssn *framework.Session, nsMap map[string]struct{}) []api.TaskID {
	var tIDs []api.TaskID
	for ns := range nsMap {
		tmp := GetSegmentFailureTaskIDs(ssn, ns)
		if len(tmp) == 0 {
			continue
		}
		tIDs = append(tIDs, tmp...)
	}
	return tIDs
}

func getDyFailedNamespaces(vT map[api.TaskID]util.NPUTask) map[string]struct{} {
	nsMap := make(map[string]struct{}, util.MapInitNum)
	for _, nT := range vT {
		nsMap[nT.NameSpace] = struct{}{}
	}
	return nsMap
}

func getDyFailedTaskIDsInFaileds(allIDS []api.TaskID, vT map[api.TaskID]util.NPUTask) []api.TaskID {
	var tIDs []api.TaskID
	for _, tID := range allIDS {
		if _, ok := vT[tID]; !ok {
			klog.V(util.LogErrorLev).Infof("getDyFailedTaskIDsInFaileds taskID(%s) not in tasks.", tID)
			continue
		}
		tIDs = append(tIDs, tID)
	}
	return tIDs
}

func initDyCutConCacheByJobInfo(nodes map[string]map[string]map[api.TaskID]struct{}, jobInf *api.JobInfo,
	vJob plugin.SchedulerJob) error {
	if jobInf == nil {
		return fmt.Errorf("initDyCutConCacheByJobInfo :%s", util.ArgumentError)
	}
	for taskID, vT := range vJob.Tasks {
		if !vT.IsNPUTask() {
			continue
		}
		if vT.Status == util.TaskStatusAllocate {
			taskInfo, taskOK := jobInf.Tasks[taskID]
			if !taskOK {
				klog.V(util.LogErrorLev).Infof("initConCache %s not in job.", vT.Name)
				continue
			}
			template, getErr := util.GetVTaskUseTemplate(taskInfo)
			if getErr != nil {
				klog.V(util.LogDebugLev).Infof("GetVTaskUseTemplate %s %s.", vT.Name, getErr)
				continue
			}
			initConcacheByTemplate(nodes, vT, template, taskID)
		}
	}
	return nil
}

func initConcacheByTemplate(nodes map[string]map[string]map[api.TaskID]struct{}, vT util.NPUTask,
	template string, taskID api.TaskID) {
	if nodes == nil {
		return
	}
	if vT.Allocated.NodeName != "" {
		templates, nodeOk := nodes[vT.Allocated.NodeName]
		if !nodeOk {
			templates = make(map[string]map[api.TaskID]struct{}, util.MapInitNum)
		}
		tasks, ok := templates[template]
		if !ok {
			tasks = make(map[api.TaskID]struct{}, util.MapInitNum)
		}
		tasks[taskID] = struct{}{}
		templates[template] = tasks
		nodes[vT.Allocated.NodeName] = templates
	}
}
