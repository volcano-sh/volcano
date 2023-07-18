/*
Copyright 2023 The Kubernetes Authors.

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

package devicescore

import (
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "devicescore"

type priorityPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}
type deviceWeight map[string]float64

// New return priority plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &priorityPlugin{pluginArguments: arguments}
}

func (pp *priorityPlugin) Name() string {
	return PluginName
}

func enableDeviceScore(args framework.Arguments) deviceWeight {
	/*
		   actions: "reclaim, allocate, backfill, preempt"
		   tiers:
		   - plugins:
		     - name: priority
		     - name: gang
		     - name: conformance
		   - plugins:
		     - name: drf
		     - name: predicates
		         predicate.vGPUEnable: true
			 predicate.GPUSharingEnable: true
		     - name: devicescore
		       arguments:
		         GpuShare: 1.2
			 vgpu4pd:  3
		     - name: proportion
		     - name: nodeorder
	*/

	scoreMap := make(deviceWeight)
	for _, val := range api.RegisteredDevices {
		weight := float64(0)
		args.GetFloat64(&weight, val)
		scoreMap[val] = weight
	}
	return scoreMap
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddBatchNodeOrderFn(pp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		score := map[string]float64{}
		scoreMap := enableDeviceScore(pp.pluginArguments)
		for _, node := range nodes {
			for _, val := range api.RegisteredDevices {
				if devices, ok := node.Others[val].(api.Devices); ok {
					if !devices.HasDeviceRequest(task.Pod) {
						continue
					}
					devScore, err := devices.ScoreNode(task.Pod)
					if err != nil {
						klog.Warningln("scoreNode failed in predicate nodeorderFn", err.Error())
						return score, err
					}
					score[node.Name] += scoreMap[val] * devScore
				} else {
					klog.Warningf("Devices %s assertion conversion failed, skip", val)
				}
			}
		}
		return score, nil
	})
}

func (pp *priorityPlugin) OnSessionClose(ssn *framework.Session) {}
