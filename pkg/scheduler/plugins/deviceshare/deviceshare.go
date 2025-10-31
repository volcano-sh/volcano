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
	"context"
	"fmt"
	"math"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
	vnpu310p "volcano.sh/volcano/pkg/scheduler/plugins/deviceshare/devices/ascend/310p/vnpu"
)

// PluginName indicates name of volcano scheduler plugin.
const (
	PluginName = "deviceshare"
	// GPUSharingPredicate is the key for enabling GPU Sharing Predicate in YAML
	GPUSharingPredicate = "deviceshare.GPUSharingEnable"
	NodeLockEnable      = "deviceshare.NodeLockEnable"
	GPUNumberPredicate  = "deviceshare.GPUNumberEnable"

	VGPUEnable = "deviceshare.VGPUEnable"

	ASCEND310PvGPU = "deviceshare.ASCEND310PVNPUEnable"

	SchedulePolicyArgument = "deviceshare.SchedulePolicy"
	ScheduleWeight         = "deviceshare.ScheduleWeight"

	KnownGeometriesCMName      = "deviceshare.KnownGeometriesCMName"
	KnownGeometriesCMNamespace = "deviceshare.KnownGeometriesCMNamespace"
)

type deviceSharePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	schedulePolicy  string
	scheduleWeight  int
}

// New return priority plugin
func New(arguments framework.Arguments) framework.Plugin {
	dsp := &deviceSharePlugin{pluginArguments: arguments, schedulePolicy: "", scheduleWeight: 0}
	enablePredicate(dsp)
	return dsp
}

func (dp *deviceSharePlugin) Name() string {
	return PluginName
}

func enablePredicate(dsp *deviceSharePlugin) {
	// Checks whether predicate.GPUSharingEnable is provided or not, if given, modifies the value in predicateEnable struct.
	nodeLockEnable := false
	args := dsp.pluginArguments
	args.GetBool(&gpushare.GpuSharingEnable, GPUSharingPredicate)
	args.GetBool(&gpushare.GpuNumberEnable, GPUNumberPredicate)
	args.GetBool(&nodeLockEnable, NodeLockEnable)
	args.GetBool(&vgpu.VGPUEnable, VGPUEnable)
	args.GetBool(&vnpu.Ascend310pvNPUEnable, ASCEND310PvGPU)

	gpushare.NodeLockEnable = nodeLockEnable
	vgpu.NodeLockEnable = nodeLockEnable

	args.GetString(&dsp.schedulePolicy, SchedulePolicyArgument)
	args.GetInt(&dsp.scheduleWeight, ScheduleWeight)

	if gpushare.GpuSharingEnable && gpushare.GpuNumberEnable {
		klog.Fatal("can not define true in both gpu sharing and gpu number")
	}
	if (gpushare.GpuSharingEnable || gpushare.GpuNumberEnable) && vgpu.VGPUEnable {
		klog.Fatal("gpu-share and vgpu can't be used together")
	}

	if !vgpu.VGPUEnable {
		return
	}
	knownGeometriesCMName := "volcano-vgpu-device-config"
	args.GetString(&knownGeometriesCMName, KnownGeometriesCMName)
	knownGeometriesCMNamespace := "kube-system"
	args.GetString(&knownGeometriesCMNamespace, KnownGeometriesCMNamespace)
	config.InitDevicesConfig(knownGeometriesCMName, knownGeometriesCMNamespace)
}

func createStatus(code int, reason string) *api.Status {
	status := api.Status{
		Code:   code,
		Reason: reason,
	}
	return &status
}

func getDeviceScore(ctx context.Context, pod *v1.Pod, node *api.NodeInfo, schedulePolicy string) (int64, *fwk.Status) {
	s := float64(0)
	for deviceType, device := range node.Others {
		if device.(api.Devices).HasDeviceRequest(pod) {
			var ns float64
			// Only process device types that use NodeOrderFn (vgpu and gpushare)
			// vnpu devices use BatchNodeOrderFn, skip them here
			if deviceType == vgpu.DeviceName || deviceType == gpushare.DeviceName {
				ns = device.(api.Devices).ScoreNode(pod, schedulePolicy)
			} else {
				// Other device types (like vnpu) use BatchNodeOrderFn, skip scoring here
				continue
			}
			s += ns
		}
	}
	klog.V(4).Infof("deviceScore for task %s/%s is: %v", pod.Namespace, pod.Name, s)
	return int64(math.Floor(s + 0.5)), nil
}

func getDeviceScoresInBatch(pod *v1.Pod, schedulePolicy string, allDevices []api.Devices) []float64 {
	switch d := allDevices[0].(type) {
	case *vnpu.NPUDevices:
		// if you need to rewrite your score policy, add a case here
		return vnpu310p.ScoreBatchNodes(pod, schedulePolicy, d, allDevices)
	default:
		score := make([]float64, 0)
		return score
	}
}

func initScoreMap(nodes []*api.NodeInfo) map[string]float64 {
	scoreMap := make(map[string]float64, len(nodes))
	for _, node := range nodes {
		if reflect.ValueOf(node).IsNil() {
			continue
		}
		scoreMap[node.Name] = 0.0
	}
	return scoreMap
}

func initializeDevicesWithSession(ssn *framework.Session) {
	for _, nodeInfo := range ssn.Nodes { // initialize every device in every node with global ssn
		for _, val := range api.RegisteredDevices {
			if dev, ok := nodeInfo.Others[val].(api.Devices); ok {
				if err := initializeDevice(dev, ssn, nodeInfo); err != nil {
					klog.Warningf("Failed to initialize devices with session for node %s: %v", nodeInfo.Name, err)
				}
			}
		}
	}
}

// initialization function for different devices
func initializeDevice(device api.Devices, ssn *framework.Session, nodeInfo *api.NodeInfo) error {
	switch d := device.(type) {
	case *vnpu.NPUDevices:
		klog.V(3).Infof("initialize ascend310p device.")
		return vnpu310p.InitVNPUDevice(d, ssn, nodeInfo)
	default:
		return nil
	}
}

func (dp *deviceSharePlugin) OnSessionOpen(ssn *framework.Session) {
	// initialize devices which needs ssn as input
	initializeDevicesWithSession(ssn)

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddPredicateFn(dp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		predicateStatus := make([]*api.Status, 0)
		// Check PredicateWithCache
		for _, val := range api.RegisteredDevices {
			if dev, ok := node.Others[val].(api.Devices); ok {
				if reflect.ValueOf(dev).IsNil() {
					// TODO When a pod requests a device of the current type, but the current node does not have such a device, an error is thrown
					if dev == nil || dev.HasDeviceRequest(task.Pod) {
						predicateStatus = append(predicateStatus, &api.Status{
							Code:   devices.Unschedulable,
							Reason: "node not initialized with device" + val,
							Plugin: PluginName,
						})
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
					klog.V(4).Infof("pod %s/%s did not request device %s on %s, skipping it", task.Pod.Namespace, task.Pod.Name, val, node.Name)
					continue
				}
				code, msg, err := dev.FilterNode(task.Pod, dp.schedulePolicy)
				if err != nil {
					predicateStatus = append(predicateStatus, createStatus(code, msg))
					return api.NewFitErrWithStatus(task, node, predicateStatus...)
				}
				filterNodeStatus := createStatus(code, msg)
				if filterNodeStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, filterNodeStatus)
					return api.NewFitErrWithStatus(task, node, predicateStatus...)
				}
			} else {
				klog.Warningf("Devices %s assertion conversion failed, skip", val)
			}
		}

		klog.V(4).Infof("checkDevices predicates Task <%s/%s> on Node <%s>: fit ",
			task.Namespace, task.Name, node.Name)

		return nil
	})

	ssn.AddNodeOrderFn(dp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		// DeviceScore
		nodeScore := float64(0)
		if dp.scheduleWeight > 0 {
			score, status := getDeviceScore(context.TODO(), task.Pod, node, dp.schedulePolicy)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Calculate Device Score Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// TODO: we should use a seperate plugin for devices, and seperate them from predicates and nodeOrder plugin.
			nodeScore = float64(score) * float64(dp.scheduleWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Device Score weight %d, score: %f", node.Name, task.Namespace, task.Name, dp.scheduleWeight, nodeScore)
		}
		return nodeScore, nil
	})

	ssn.AddBatchNodeOrderFn(dp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		scoreMap := initScoreMap(nodes)
		if dp.scheduleWeight > 0 {
			for _, deviceType := range api.RegisteredDevices {
				// only process devices which needs score nodes in batch
				if deviceType != vnpu.DeviceName {
					continue
				}

				//get all nodes' device of this kind
				//allDevices store all devices of the global nodes
				allDevices := make([]api.Devices, 0)
				for _, node := range nodes {
					device, ok := node.Others[deviceType]
					if ok {
						if deviceInterface, isDeviceInterface := device.(api.Devices); isDeviceInterface {
							if reflect.ValueOf(deviceInterface).IsNil() {
								if deviceInterface == nil || deviceInterface.HasDeviceRequest(task.Pod) {
									return nil, fmt.Errorf("node not initialized with device %s", deviceType)
								}
								klog.V(4).Infof("pod %s/%s did not request device %s on %s, skipping it", task.Pod.Namespace, task.Pod.Name, deviceType, nodes[0].Name)
								continue
							}
							allDevices = append(allDevices, deviceInterface)
						}
					} else {
						klog.Warningf("Devices %s assertion conversion failed, skip", deviceType)
					}
				}

				// Check if there are devices available for scoring
				if len(allDevices) == 0 {
					klog.V(4).Infof("No devices of type %s found for scoring", deviceType)
					continue
				}

				scores := getDeviceScoresInBatch(task.Pod, dp.schedulePolicy, allDevices)
				// Ensure score array length matches nodes count
				if len(scores) != len(nodes) {
					klog.Warningf("Score array length (%d) doesn't match nodes length (%d) for device type %s", len(scores), len(nodes), deviceType)
					continue
				}

				for i := range nodes {
					finalScore := scores[i] * float64(dp.scheduleWeight)
					scoreMap[nodes[i].Node.Name] += finalScore
					klog.V(5).Infof("Node: %s, task<%s/%s> Device Score weight %d, score: %f", nodes[i].Name, task.Namespace, task.Name, dp.scheduleWeight, finalScore)
				}
			}
		}
		return scoreMap, nil
	})
}

func (dp *deviceSharePlugin) OnSessionClose(ssn *framework.Session) {}
