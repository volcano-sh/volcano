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
Package internal is using for HuaWei Ascend pin scheduling policy schedule.
*/
package internal

import (
	"errors"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/nslb"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// Controller controller to driver the scheduling process
type Controller struct {
	PolicyHandler []plugin.SchedulerPluginNeed
}

// New new scheduler driver controller
func New() plugin.SchedulerPluginNeed {
	return &Controller{}
}

// SetPolicyHandler set attr and env for plugin
func (c *Controller) SetPolicyHandler(attr util.SchedulerJobAttr, env plugin.ScheduleEnv) {
	if c == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("InitMyJobPlugin %s.", err.Error())
		return
	}
	if handler, ok := npu.InitPolicyHandler(attr, env); ok {
		c.PolicyHandler = append(c.PolicyHandler, handler)
	}
	if handler, ok := nslb.InitPolicyHandler(attr, env); ok {
		c.PolicyHandler = append(c.PolicyHandler, handler)
	}
}

// PreStartAction pre-processing actions for all policy handler
func (c *Controller) PreStartAction(ssn *framework.Session) error {
	if c == nil {
		return errors.New(util.ArgumentError)
	}
	for _, handler := range c.PolicyHandler {
		if err := handler.PreStartAction(ssn); err != nil {
			return err
		}
	}
	return nil
}

// InitMyJobPlugin set attr and env for plugin
func (c *Controller) InitMyJobPlugin(attr util.SchedulerJobAttr, env plugin.ScheduleEnv) error {
	if c == nil {
		return errors.New(util.ArgumentError)
	}
	c.SetPolicyHandler(attr, env)
	for _, handler := range c.PolicyHandler {
		if err := handler.InitMyJobPlugin(attr, env); err != nil {
			return err
		}
	}
	return nil
}

// ValidNPUJob check job req npu num
func (c *Controller) ValidNPUJob() *api.ValidateResult {
	if c == nil {
		err := errors.New(util.ArgumentError)
		return &api.ValidateResult{Pass: false, Reason: err.Error(), Message: err.Error()}
	}
	for _, handler := range c.PolicyHandler {
		if result := handler.ValidNPUJob(); result != nil && !result.Pass {
			return result
		}
	}
	return nil
}

// CheckNodeNPUByTask check nod npu meet task req
func (c *Controller) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if c == nil {
		err := errors.New(util.ArgumentError)
		return err
	}
	for _, handler := range c.PolicyHandler {
		if err := handler.CheckNodeNPUByTask(task, node); err != nil {
			return err
		}
	}
	return nil
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (c *Controller) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, sMap map[string]float64) error {
	if c == nil || task == nil || len(nodes) == 0 || sMap == nil {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes %v.", err.Error())
		return err
	}
	for _, handler := range c.PolicyHandler {
		if err := handler.ScoreBestNPUNodes(task, nodes, sMap); err != nil {
			return err
		}
	}
	klog.V(util.LogInfoLev).Infof("ScoreBestNPUNodes task<%s> scoreMap<%v>", task.Name, sMap)
	return nil
}

// UseAnnotation select npu for task from node
func (c *Controller) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if c == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("UseAnnotation %s.", err.Error())
		return nil
	}
	for _, handler := range c.PolicyHandler {
		node = *handler.UseAnnotation(task, node)
	}
	return &node
}

// ReleaseAnnotation release annotation
func (c *Controller) ReleaseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if c == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ReleaseAnnotation %s.", err.Error())
		return nil
	}
	for _, handler := range c.PolicyHandler {
		node = *handler.ReleaseAnnotation(task, node)
	}
	return &node
}
