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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/listers"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PodLister is used in predicate and nodeorder plugin
type PodLister struct {
	Session *framework.Session

	CachedPods       map[api.TaskID]*v1.Pod
	Tasks            map[api.TaskID]*api.TaskInfo
	TaskWithAffinity map[api.TaskID]*api.TaskInfo
}

// PodAffinityLister is used to list pod with affinity
type PodAffinityLister struct {
	pl *PodLister
}

// HaveAffinity checks pod have affinity or not
func HaveAffinity(pod *v1.Pod) bool {
	affinity := pod.Spec.Affinity
	return affinity != nil &&
		(affinity.NodeAffinity != nil ||
			affinity.PodAffinity != nil ||
			affinity.PodAntiAffinity != nil)
}

// NewPodLister returns a PodLister generate from ssn
func NewPodLister(ssn *framework.Session) *PodLister {
	pl := &PodLister{
		Session: ssn,

		CachedPods:       make(map[api.TaskID]*v1.Pod),
		Tasks:            make(map[api.TaskID]*api.TaskInfo),
		TaskWithAffinity: make(map[api.TaskID]*api.TaskInfo),
	}

	for _, job := range pl.Session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				pl.Tasks[task.UID] = task
				if HaveAffinity(task.Pod) {
					pl.TaskWithAffinity[task.UID] = task
				}
			}
		}
	}

	return pl
}

func (pl *PodLister) copyTaskPod(task *api.TaskInfo) *v1.Pod {
	pod := task.Pod.DeepCopy()
	pod.Spec.NodeName = task.NodeName
	return pod
}

// GetPod will get pod with proper nodeName, from cache or DeepCopy
// keeping this function read only to avoid concurrent panic of map
func (pl *PodLister) GetPod(task *api.TaskInfo) *v1.Pod {
	if task.NodeName == task.Pod.Spec.NodeName {
		return task.Pod
	}

	pod, found := pl.CachedPods[task.UID]
	if !found {
		// we could not write the copied pod back into cache for read only
		pod = pl.copyTaskPod(task)
		klog.Warningf("DeepCopy for pod %s/%s at PodLister.GetPod is unexpected", pod.Namespace, pod.Name)
	}
	return pod
}

// UpdateTask will update the pod nodeName in cache using nodeName
// NOT thread safe, please ensure UpdateTask is the only called function of PodLister at the same time.
func (pl *PodLister) UpdateTask(task *api.TaskInfo, nodeName string) *v1.Pod {
	pod, found := pl.CachedPods[task.UID]
	if !found {
		pod = pl.copyTaskPod(task)
		pl.CachedPods[task.UID] = pod
	}
	pod.Spec.NodeName = nodeName

	if !api.AllocatedStatus(task.Status) {
		delete(pl.Tasks, task.UID)
		if HaveAffinity(task.Pod) {
			delete(pl.TaskWithAffinity, task.UID)
		}
	} else {
		pl.Tasks[task.UID] = task
		if HaveAffinity(task.Pod) {
			pl.TaskWithAffinity[task.UID] = task
		}
	}

	return pod
}

// List method is used to list all the pods
func (pl *PodLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, task := range pl.Tasks {
		pod := pl.GetPod(task)
		if selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

// FilteredList is used to list all the pods under filter condition
func (pl *PodLister) filteredListWithTaskSet(taskSet map[api.TaskID]*api.TaskInfo, podFilter listers.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, task := range taskSet {
		pod := pl.GetPod(task)
		if podFilter(pod) && selector.Matches(labels.Set(pod.Labels)) {
			pods = append(pods, pod)
		}
	}

	return pods, nil
}

// FilteredList is used to list all the pods under filter condition
func (pl *PodLister) FilteredList(podFilter listers.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pl.filteredListWithTaskSet(pl.Tasks, podFilter, selector)
}

// AffinityFilteredList is used to list all the pods with affinity under filter condition
func (pl *PodLister) AffinityFilteredList(podFilter listers.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pl.filteredListWithTaskSet(pl.TaskWithAffinity, podFilter, selector)
}

// AffinityLister generate a PodAffinityLister following current PodLister
func (pl *PodLister) AffinityLister() *PodAffinityLister {
	pal := &PodAffinityLister{
		pl: pl,
	}
	return pal
}

// List method is used to list all the pods
func (pal *PodAffinityLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	return pal.pl.List(selector)
}

// FilteredList is used to list all the pods with affinity under filter condition
func (pal *PodAffinityLister) FilteredList(podFilter listers.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	return pal.pl.AffinityFilteredList(podFilter, selector)
}

// GenerateNodeMapAndSlice returns the nodeMap and nodeSlice generated from ssn
func GenerateNodeMapAndSlice(nodes map[string]*api.NodeInfo) (map[string]*schedulernodeinfo.NodeInfo, []*v1.Node) {
	var nodeMap map[string]*schedulernodeinfo.NodeInfo
	var nodeSlice []*v1.Node
	nodeMap = make(map[string]*schedulernodeinfo.NodeInfo)
	for _, node := range nodes {
		nodeInfo := schedulernodeinfo.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)
		nodeMap[node.Name] = nodeInfo
		nodeSlice = append(nodeSlice, node.Node)
	}
	return nodeMap, nodeSlice
}

// CachedNodeInfo is used in nodeorder and predicate plugin
type CachedNodeInfo struct {
	Session *framework.Session
}

// GetNodeInfo is used to get info of a particular node
func (c *CachedNodeInfo) GetNodeInfo(name string) (*v1.Node, error) {
	node, found := c.Session.Nodes[name]
	if !found {

		return nil, errors.NewNotFound(v1.Resource("node"), name)
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
