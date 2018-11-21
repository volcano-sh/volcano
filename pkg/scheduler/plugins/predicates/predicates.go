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

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
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

type podLister struct {
	session *framework.Session
}

func (pl *podLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if selector.Matches(labels.Set(task.Pod.Labels)) {
					pod := task.Pod.DeepCopy()
					pod.Spec.NodeName = task.NodeName
					pods = append(pods, pod)
				}
			}
		}
	}

	return pods, nil
}

func (pl *podLister) FilteredList(podFilter cache.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if podFilter(task.Pod) && selector.Matches(labels.Set(task.Pod.Labels)) {
					pod := task.Pod.DeepCopy()
					pod.Spec.NodeName = task.NodeName
					pods = append(pods, pod)
				}
			}
		}
	}

	return pods, nil
}

type cachedNodeInfo struct {
	session *framework.Session
}

func (c *cachedNodeInfo) GetNodeInfo(name string) (*v1.Node, error) {
	node, found := c.session.NodeIndex[name]
	if !found {
		return nil, fmt.Errorf("failed to find node <%s>", name)
	}

	return node.Node, nil
}

// Check to see if node spec is set to Schedulable or not
func CheckNodeUnschedulable(pod *v1.Pod, nodeInfo *cache.NodeInfo) (bool, []algorithm.PredicateFailureReason, error) {
	if nodeInfo.Node().Spec.Unschedulable {
		return false, []algorithm.PredicateFailureReason{predicates.ErrNodeUnschedulable}, nil
	}
	return true, nil, nil
}

func (pp *nodeAffinityPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := &podLister{
		session: ssn,
	}

	ni := &cachedNodeInfo{
		session: ssn,
	}

	ssn.AddPredicateFn(func(task *api.TaskInfo, node *api.NodeInfo) error {
		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)

		if node.Allocatable.MaxTaskNum <= len(nodeInfo.Pods()) {
			return fmt.Errorf("Node <%s> can not allow more task running on it.", node.Name)
		}

		// NodeSeletor Predicate
		fit, _, err := predicates.PodMatchNodeSelector(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("NodeSelect predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
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

		glog.V(4).Infof("HostPorts predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("node <%s> didn't have available host ports for task <%s/%s>",
				node.Name, task.Namespace, task.Name)
		}

		// Check to see if node.Spec.Unschedulable is set
		fit, _, err = CheckNodeUnschedulable(task.Pod, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("Check Unschedulable Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> node <%s> set to unschedulable",
				task.Namespace, task.Name, node.Name)
		}

		// Toleration/Taint Predicate
		fit, _, err = predicates.PodToleratesNodeTaints(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("Toleration/Taint predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> does not tolerate node <%s> taints",
				task.Namespace, task.Name, node.Name)
		}

		// Pod Affinity/Anti-Affinity Predicate
		podAffinityPredicate := predicates.NewPodAffinityPredicate(ni, pl)
		fit, _, err = podAffinityPredicate(task.Pod, nil, nodeInfo)
		if err != nil {
			return err
		}

		glog.V(4).Infof("Pod Affinity/Anti-Affinity predicates Task <%s/%s> on Node <%s>: fit %t, err %v",
			task.Namespace, task.Name, node.Name, fit, err)

		if !fit {
			return fmt.Errorf("task <%s/%s> affinity/anti-affinity failed on node <%s>",
				node.Name, task.Namespace, task.Name)
		}

		return nil
	})
}

func (pp *nodeAffinityPlugin) OnSessionClose(ssn *framework.Session) {}
