/*
Copyright 2024 The Volcano Authors.

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

package deviceshare

import (
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PluginName indicates name of volcano scheduler plugin.
const (
	PluginName = "deviceshare"
	// GPUSharingPredicate is the key for enabling GPU Sharing Predicate in YAML
	GPUSharingPredicate = "deviceshare.GPUSharingEnable"
	NodeLockEnable      = "deviceshare.NodeLockEnable"
	GPUNumberPredicate  = "deviceshare.GPUNumberEnable"

	VGPUEnable = "deviceshare.VGPUEnable"
)

type deviceSharePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return priority plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &deviceSharePlugin{pluginArguments: arguments}
}

func (dp *deviceSharePlugin) Name() string {
	return PluginName
}

func enablePredicate(args framework.Arguments) {
	// Checks whether predicate.GPUSharingEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&gpushare.GpuSharingEnable, GPUSharingPredicate)
	args.GetBool(&gpushare.GpuNumberEnable, GPUNumberPredicate)
	args.GetBool(&gpushare.NodeLockEnable, NodeLockEnable)
	args.GetBool(&vgpu.VGPUEnable, VGPUEnable)

	if gpushare.GpuSharingEnable && gpushare.GpuNumberEnable {
		klog.Fatal("can not define true in both gpu sharing and gpu number")
	}
	if (gpushare.GpuSharingEnable || gpushare.GpuNumberEnable) && vgpu.VGPUEnable {
		klog.Fatal("gpu-share and vgpu can't be used together")
	}
}

func (dp *deviceSharePlugin) OnSessionOpen(ssn *framework.Session) {
	enablePredicate(dp.pluginArguments)
	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddPredicateFn(dp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) ([]*api.Status, error) {
		predicateStatus := make([]*api.Status, 0)
		// Check PredicateWithCache
		for _, val := range api.RegisteredDevices {
			if dev, ok := node.Others[val].(api.Devices); ok {
				if dev == nil {
					predicateStatus = append(predicateStatus, &api.Status{
						Code:   devices.Unschedulable,
						Reason: "node not initialized with device" + val,
					})
					return predicateStatus, fmt.Errorf("node not initialized with device %s", val)
				}
				code, msg, err := dev.FilterNode(task.Pod)
				filterNodeStatus := &api.Status{
					Code:   code,
					Reason: msg,
				}
				if err != nil {
					return predicateStatus, err
				}
				if filterNodeStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, filterNodeStatus)
					return predicateStatus, fmt.Errorf("plugin device filternode predicates failed %s", msg)
				}
			} else {
				klog.Warningf("Devices %s assertion conversion failed, skip", val)
			}
		}

		klog.V(4).Infof("checkDevices predicates Task <%s/%s> on Node <%s>: fit ",
			task.Namespace, task.Name, node.Name)

		return predicateStatus, nil
	})
}

func (dp *deviceSharePlugin) OnSessionClose(ssn *framework.Session) {}
