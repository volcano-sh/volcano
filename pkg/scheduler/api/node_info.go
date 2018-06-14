/*
Copyright 2017 The Kubernetes Authors.

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

package api

import (
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
)

// NodeInfo is node level aggregated information.
type NodeInfo struct {
	Name string
	Node *v1.Node

	// The idle resource on that node
	Idle *Resource
	// The used resource on that node, including running and terminating
	// pods
	Used *Resource

	Allocatable *Resource
	Capability  *Resource

	Tasks map[TaskID]*TaskInfo
}

func NewNodeInfo(node *v1.Node) *NodeInfo {
	if node == nil {
		return &NodeInfo{
			Idle: EmptyResource(),
			Used: EmptyResource(),

			Allocatable: EmptyResource(),
			Capability:  EmptyResource(),

			Tasks: make(map[TaskID]*TaskInfo),
		}
	}

	return &NodeInfo{
		Name: node.Name,
		Node: node,
		Idle: NewResource(node.Status.Allocatable),
		Used: EmptyResource(),

		Allocatable: NewResource(node.Status.Allocatable),
		Capability:  NewResource(node.Status.Capacity),

		Tasks: make(map[TaskID]*TaskInfo),
	}
}

func (ni *NodeInfo) Clone() *NodeInfo {
	pods := make(map[TaskID]*TaskInfo, len(ni.Tasks))

	for _, p := range ni.Tasks {
		pods[PodKey(p.Pod)] = p.Clone()
	}

	return &NodeInfo{
		Name:        ni.Name,
		Node:        ni.Node,
		Idle:        ni.Idle.Clone(),
		Used:        ni.Used.Clone(),
		Allocatable: ni.Allocatable.Clone(),
		Capability:  ni.Capability.Clone(),

		Tasks: pods,
	}
}

func (ni *NodeInfo) SetNode(node *v1.Node) {
	if ni.Node == nil {
		ni.Idle = NewResource(node.Status.Allocatable)

		for _, p := range ni.Tasks {
			ni.Idle.Sub(p.Resreq)
			ni.Used.Add(p.Resreq)
		}
	}

	ni.Name = node.Name
	ni.Node = node
	ni.Allocatable = NewResource(node.Status.Allocatable)
	ni.Capability = NewResource(node.Status.Capacity)
}

func (ni *NodeInfo) AddTask(p *TaskInfo) {
	key := PodKey(p.Pod)
	if _, found := ni.Tasks[key]; found {
		glog.Errorf("Task <%v/%v> already on node <%v>, should not add again.",
			p.Namespace, p.Name, ni.Name)
		return
	}

	if ni.Node != nil {
		ni.Idle.Sub(p.Resreq)
		ni.Used.Add(p.Resreq)
	}

	ni.Tasks[key] = p
}

func (ni *NodeInfo) RemoveTask(p *TaskInfo) {
	key := PodKey(p.Pod)
	if _, found := ni.Tasks[key]; !found {
		return
	}

	if ni.Node != nil {
		ni.Idle.Add(p.Resreq)
		ni.Used.Sub(p.Resreq)
	}

	delete(ni.Tasks, PodKey(p.Pod))
}
