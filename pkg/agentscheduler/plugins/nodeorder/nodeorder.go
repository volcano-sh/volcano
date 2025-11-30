/*
Copyright 2025 The Volcano Authors.

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

package nodeorder

import (
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	vfwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "nodeorder"
)

type nodeOrderPlugin struct {
	*nodeorder.NodeOrderPlugin
}

func New(arguments vfwk.Arguments) framework.Plugin {
	plugin := nodeorder.New(arguments).(*nodeorder.NodeOrderPlugin)

	return &nodeOrderPlugin{
		NodeOrderPlugin: plugin,
	}
}

func (np *nodeOrderPlugin) Name() string {
	return PluginName
}

func (np *nodeOrderPlugin) OnPluginInit(fwk *framework.Framework) {
	// 1. Set the framework handle to the nodeorder plugin
	np.Handle = fwk
	// 2. Initialize all the plugin introduced from kube-scheduler ( Notice that Handle must be set before this step)
	np.NodeOrderPlugin.InitPlugin()
	// 3. Register nodeorder related extension points

	fwk.AddNodeOrderFn(np.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		if k8sNodeInfo, err := fwk.GetSnapshot().GetK8sNodeInfo(node.Name); err != nil {
			return 0, err
		} else {
			state := k8sframework.NewCycleState()
			return np.NodeOrderFn(task, node, k8sNodeInfo, state)
		}
	})

	fwk.AddBatchNodeOrderFn(np.Name(), func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		state := k8sframework.NewCycleState()
		return np.BatchNodeOrderFn(task, nodeInfo, state)
	})
}

func (np *nodeOrderPlugin) OnCycleStart(fwk *framework.Framework) {
}

func (np *nodeOrderPlugin) OnCycleEnd(fwk *framework.Framework) {
}
