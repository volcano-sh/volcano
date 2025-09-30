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
Package nslb is using for HuaWei Ascend pin tor affinity.
*/
package nslb

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// InitPolicyHandler init nslb policy handler
func InitPolicyHandler(attr util.SchedulerJobAttr, env plugin.ScheduleEnv) (plugin.SchedulerPluginNeed, bool) {
	if !attr.IsJobHasTorAffinityLabel() {
		return nil, false
	}
	defaultHandler := TorHandler{
		pluginName:   pluginName,
		globalTorEnv: env.Tors,
	}
	if tmpJob, ok := env.Jobs[attr.Name]; ok {
		defaultHandler.Job = &tmpJob
	}
	if env.Tors == nil {
		return &defaultHandler, true
	}
	if env.Tors.TorLevel == SingleLayer {
		return &TorSingleLevelHandler{TorHandler: defaultHandler}, true
	}
	if env.Tors.GetNSLBVersion() == defaultNSLBVersion {
		return &TorHandlerV1{TorHandler: defaultHandler}, true
	}
	if env.Tors.GetNSLBVersion() == nslbv2Version {
		return &TorHandlerV2{TorHandler: defaultHandler}, true
	}
	return &defaultHandler, true
}

// PreStartAction pre-processing actions for rescheduling
func (th *TorHandler) PreStartAction(_ *framework.Session) error {
	return nil
}

// InitMyJobPlugin set attr and env for plugin
func (th *TorHandler) InitMyJobPlugin(_ util.SchedulerJobAttr, _ plugin.ScheduleEnv) error {
	return nil
}

// ValidNPUJob check job req npu num
func (th *TorHandler) ValidNPUJob() *api.ValidateResult {
	if th == nil || th.Job == nil {
		err := errors.New(util.ArgumentError)
		return &api.ValidateResult{Pass: false, Reason: err.Error(), Message: err.Error()}
	}
	klog.V(util.LogDebugLev).Infof("%s ValidNPUJob job(%s).", th.GetPluginName(), th.Job.Name)

	if th.globalTorEnv == nil {
		reason := "job tor affinity check failed, cluster basic-tor-node-cm is not imported"
		klog.V(util.LogWarningLev).Info(reason)
		return &api.ValidateResult{Pass: false, Reason: reason,
			Message: fmt.Sprintf("validJobF failed:%s", reason)}
	}

	if k, ok := th.Job.Label[TorAffinityKey]; ok && k != LargeModelTag && k != NormalSchema && k != NullTag {
		reason := fmt.Sprintf("job tor affinity label check failed,tor-affinity label value is %s", k)
		klog.V(util.LogWarningLev).Infof(reason)
		return &api.ValidateResult{Pass: false, Reason: reason,
			Message: fmt.Sprintf("validJobFn failed:%s label is %s ", reason, k)}
	}
	return nil
}

// CheckNodeNPUByTask check nod npu meet task req
func (th *TorHandler) CheckNodeNPUByTask(_ *api.TaskInfo, _ plugin.NPUNode) error {
	return nil
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (th *TorHandler) ScoreBestNPUNodes(_ *api.TaskInfo, _ []*api.NodeInfo, _ map[string]float64) error {
	return nil
}

// UseAnnotation select npu for task from node
func (th *TorHandler) UseAnnotation(_ *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	return &node
}

// ReleaseAnnotation release annotation
func (th *TorHandler) ReleaseAnnotation(_ *api.TaskInfo, _ plugin.NPUNode) *plugin.NPUNode {
	return &plugin.NPUNode{}
}
