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
Package vnpu is using for Ascend vnpu affinity schedule.
*/
package vnpu

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/vnpu"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

type virtual310NPU struct {
	base.NPUHandler
	vHandle *vnpu.VirtualNPU
}

// New return npu plugin.
func New(npuName string) base.AscendHandler {
	npuPlugin := &virtual310NPU{}
	npuPlugin.SetPluginName(npuName)
	npuPlugin.SetAnnoName(util.NPU310PCardName)
	npuPlugin.SetAnnoPreVal(util.NPU310PCardNamePre)
	npuPlugin.vHandle = &vnpu.VirtualNPU{}
	npuPlugin.vHandle.InitVNPU()

	return npuPlugin
}

// PreStartAction pre-processing actions for rescheduling
func (tp *virtual310NPU) PreStartAction(ssn *framework.Session) error {
	klog.V(util.LogDebugLev).Infof("Entering PreStartAction of %s", util.NPU310PCardName)
	defer klog.V(util.LogDebugLev).Infof("Leaving PreStartAction of %s", util.NPU310PCardName)
	if tp == nil || ssn == nil || tp.FrameAttr.KubeClient == nil {
		return fmt.Errorf("%s handler not enabled or ssn is nil: %s", util.NPU310PCardName, util.ArgumentError)
	}
	return tp.vHandle.PreStartAction(&tp.ScheduleEnv, ssn)
}

// ValidNPUJob check job req npu num and mode
func (tp *virtual310NPU) ValidNPUJob() *api.ValidateResult {
	if tp == nil {
		err := errors.New(util.ArgumentError)
		return &api.ValidateResult{Pass: false, Reason: err.Error(), Message: err.Error()}
	}
	klog.V(util.LogDebugLev).Infof("%s ValidNPUJob job(%s).", tp.GetPluginName(), tp.Name)
	return tp.validDyVNPUJob()
}

// CheckNodeNPUByTask check nod npu meet task req
func (tp *virtual310NPU) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	klog.V(util.LogDebugLev).Infof("%s CheckNodeNPUByTask job(%s).", tp.GetPluginName(), tp.Name)
	if task == nil || len(node.Annotation) == 0 {
		return errors.New(util.ArgumentError)
	}
	taskRes, err := tp.vHandle.GetTaskResource(task, node)
	if err != nil {
		return err
	}
	return tp.vHandle.CheckNodeNPUByDyTask(task, node, taskRes)
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (tp *virtual310NPU) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, sMap map[string]float64) error {
	klog.V(util.LogDebugLev).Infof("%s ScoreBestNPUNodes job(%s).", tp.GetPluginName(), tp.Name)
	return tp.vHandle.DynamicVNPU.ScoreBestNPUNodes(task, nodes, sMap)
}

// UseAnnotation select npu for task from node
func (tp *virtual310NPU) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	klog.V(util.LogDebugLev).Infof("%s UseAnnotation job(%s).", tp.GetPluginName(), tp.Name)
	taskRes, err := tp.vHandle.GetTaskResource(task, node)
	klog.V(util.LogDebugLev).Infof("task<%s> require resource<%#v>", task.Name, taskRes)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation job(%s) get require task resource failed: %s",
			tp.GetPluginName(), tp.Name, err)
	}
	return tp.vHandle.DynamicVNPU.UseAnnotation(task, node, taskRes, tp.vHandle.VT)
}

// ReleaseAnnotation release select npu for task to node
func (tp *virtual310NPU) ReleaseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	klog.V(util.LogDebugLev).Infof("%s ReleaseAnnotation job(%s).", tp.GetPluginName(), tp.Name)
	return tp.vHandle.DynamicVNPU.ReleaseAnnotation(task, node)
}
