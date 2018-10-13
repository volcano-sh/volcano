/*
Copyright 2018 The Kubernetes Authors.

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

package predicates

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/kubernetes/pkg/scheduler/algorithm/predicates"
	"k8s.io/kubernetes/pkg/scheduler/cache"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type nodeAffinityPlugin struct {
	args *framework.PluginArgs
}

func New(args *framework.PluginArgs) framework.Plugin {
	return &nodeAffinityPlugin{
		args: args,
	}
}

func (pp *nodeAffinityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddPredicateFn(func(task *api.TaskInfo, node *api.NodeInfo) error {
		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)

		// NodeSeletor Predicate
		fit, _, err := predicates.PodMatchNodeSelector(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(3).Infof("NodeSelect predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> didn't match task <%s/%s> node selector",
				node.Name, task.Namespace, task.Name)
		}

		// HostPorts Predicate
		fit, _, err = predicates.PodFitsHostPorts(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(3).Infof("HostPorts predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> didn't have available host ports for task <%s/%s>",
				node.Name, task.Namespace, task.Name)
		}

		// Toleration/Taint Predicate
		fit, _, err = predicates.PodToleratesNodeTaints(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(3).Infof("Toleration/Taint predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> does not tolerate node <%s> taints",
				task.Namespace, task.Name, node.Name)
		}

		return nil
	})
}

func (pp *nodeAffinityPlugin) OnSessionClose(ssn *framework.Session) {}
