/*
Copyright 2019 The Kubernetes Authors.

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

package util

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

// PodLister is used in predicate and nodeorder plugin
type PodLister struct {
	Session *framework.Session
}

// List method is used to list all the pods
func (pl *PodLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.Session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if selector.Matches(labels.Set(task.Pod.Labels)) {
					if task.NodeName != task.Pod.Spec.NodeName {
						pod := task.Pod.DeepCopy()
						pod.Spec.NodeName = task.NodeName
						pods = append(pods, pod)
					} else {
						pods = append(pods, task.Pod)
					}
				}
			}
		}
	}

	return pods, nil
}

// FilteredList is used to list all the pods under filter condition
func (pl *PodLister) FilteredList(podFilter algorithm.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.Session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if podFilter(task.Pod) && selector.Matches(labels.Set(task.Pod.Labels)) {
					if task.NodeName != task.Pod.Spec.NodeName {
						pod := task.Pod.DeepCopy()
						pod.Spec.NodeName = task.NodeName
						pods = append(pods, pod)
					} else {
						pods = append(pods, task.Pod)
					}
				}
			}
		}
	}

	return pods, nil
}

// CachedNodeInfo is used in nodeorder and predicate plugin
type CachedNodeInfo struct {
	Session *framework.Session
}

// GetNodeInfo is used to get info of a particular node
func (c *CachedNodeInfo) GetNodeInfo(name string) (*v1.Node, error) {
	node, found := c.Session.Nodes[name]
	if !found {
		return nil, fmt.Errorf("failed to find node <%s>", name)
	}

	return node.Node, nil
}

// NodeLister is used in nodeorder plugin
type NodeLister struct {
	Session *framework.Session
}

// List is used to list all the nodes
func (nl *NodeLister) List() ([]*v1.Node, error) {
	var nodes []*v1.Node
	for _, node := range nl.Session.Nodes {
		nodes = append(nodes, node.Node)
	}
	return nodes, nil
}
