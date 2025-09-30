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
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

// PolicyBuilder PolicyBuilder plugin management
type PolicyBuilder = func() SchedulerPluginNeed

// SchedulerPluginBase the frame plugin need implement.
type SchedulerPluginBase interface {
	GetPluginName() string
	SetPluginName(string)
	GetAnnoPreVal() string
	SetAnnoPreVal(string)
	GetAnnoName() string
	SetAnnoName(string)
}

// SchedulerPluginNeed The interface that the specific plug-in needs to implement.
type SchedulerPluginNeed interface {
	// ValidNPUJob Valid the job part of npu scheduler policy, if not, disallowed.
	ValidNPUJob() *api.ValidateResult
	CheckNodeNPUByTask(*api.TaskInfo, NPUNode) error
	ScoreBestNPUNodes(*api.TaskInfo, []*api.NodeInfo, map[string]float64) error
	UseAnnotation(*api.TaskInfo, NPUNode) *NPUNode
	ReleaseAnnotation(*api.TaskInfo, NPUNode) *NPUNode
	PreStartAction(ssn *framework.Session) error
	InitMyJobPlugin(util.SchedulerJobAttr, ScheduleEnv) error
}

// SchedulerPlugin for volcano-npu plugin has function.
type SchedulerPlugin interface {
	SchedulerPluginBase
	SchedulerPluginNeed
}

// FaultHandler fault handler for job
type FaultHandler interface {
	Execute(*ScheduleEnv, *framework.Session) error
	CheckNodeNPUByTask(*api.TaskInfo, *NPUNode) error
	ScoreBestNPUNodes(*api.TaskInfo, map[string]float64)
	PreStopAction(*ScheduleEnv) error
}

// SchedulerBaseAttr for all volcano-npu plugin.
type SchedulerBaseAttr struct {
	// the new func add name
	pluginName string
	// in k8s annotation huawei.com/Ascend310,huawei.com/Ascend910
	annoName string
	// huawei.com/
	annoPreVal string
}

// GetPluginName get PluginName.
func (sp SchedulerBaseAttr) GetPluginName() string {
	return sp.pluginName
}

// SetPluginName set PluginName.
func (sp *SchedulerBaseAttr) SetPluginName(name string) {
	if sp == nil {
		klog.V(util.LogInfoLev).Infof("SetPluginName failed: %s.", util.ArgumentError)
		return
	}
	sp.pluginName = name
}

// GetAnnoPreVal get AnnoPreVal.
func (sp SchedulerBaseAttr) GetAnnoPreVal() string {
	return sp.annoPreVal
}

// SetAnnoPreVal set AnnoPreVal.
func (sp *SchedulerBaseAttr) SetAnnoPreVal(value string) {
	if sp == nil {
		klog.V(util.LogInfoLev).Infof("SetAnnoPreVal failed: %s.", util.ArgumentError)
		return
	}
	sp.annoPreVal = value
}

// GetAnnoName get AnnoName.
func (sp SchedulerBaseAttr) GetAnnoName() string {
	return sp.annoName
}

// SetAnnoName set AnnoName.
func (sp *SchedulerBaseAttr) SetAnnoName(annoName string) {
	if sp == nil {
		klog.V(util.LogInfoLev).Infof("SetAnnoName failed: %s.", util.ArgumentError)
		return
	}
	sp.annoName = annoName
}
