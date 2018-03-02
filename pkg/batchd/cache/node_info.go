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

package cache

import (
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
	// The resources that is planned to used
	// policy will use it to decide if assign pods on this node
	UnAcceptedAllocated *Resource

	Allocatable *Resource
	Capability  *Resource

	Pods map[string]*PodInfo
}

func NewNodeInfo(node *v1.Node) *NodeInfo {
	if node == nil {
		return &NodeInfo{
			Idle:                EmptyResource(),
			Used:                EmptyResource(),
			UnAcceptedAllocated: EmptyResource(),

			Allocatable: EmptyResource(),
			Capability:  EmptyResource(),

			Pods: make(map[string]*PodInfo),
		}
	}

	return &NodeInfo{
		Name:                node.Name,
		Node:                node,
		Idle:                NewResource(node.Status.Allocatable),
		Used:                EmptyResource(),
		UnAcceptedAllocated: EmptyResource(),

		Allocatable: NewResource(node.Status.Allocatable),
		Capability:  NewResource(node.Status.Capacity),

		Pods: make(map[string]*PodInfo),
	}
}

func (ni *NodeInfo) Clone() *NodeInfo {
	pods := make(map[string]*PodInfo, len(ni.Pods))

	for _, p := range ni.Pods {
		pods[podKey(p.Pod)] = p.Clone()
	}

	return &NodeInfo{
		Name:                ni.Name,
		Node:                ni.Node,
		Idle:                ni.Idle.Clone(),
		Used:                ni.Used.Clone(),
		UnAcceptedAllocated: ni.UnAcceptedAllocated.Clone(),
		Allocatable:         ni.Allocatable.Clone(),
		Capability:          ni.Capability.Clone(),

		Pods: pods,
	}
}

func (ni *NodeInfo) SetNode(node *v1.Node) {
	if ni.Node == nil {
		ni.Idle = NewResource(node.Status.Allocatable)

		for _, p := range ni.Pods {
			ni.Idle.Sub(p.Request)
			ni.Used.Add(p.Request)
		}
	}

	ni.Name = node.Name
	ni.Node = node
	ni.Allocatable = NewResource(node.Status.Allocatable)
	ni.Capability = NewResource(node.Status.Capacity)
}

func (ni *NodeInfo) AddPod(p *PodInfo) {
	if ni.Node != nil {
		ni.Idle.Sub(p.Request)
		ni.Used.Add(p.Request)
	}

	ni.Pods[podKey(p.Pod)] = p
}

func (ni *NodeInfo) RemovePod(p *PodInfo) {
	if ni.Node != nil {
		ni.Idle.Add(p.Request)
		ni.Used.Sub(p.Request)
	}

	delete(ni.Pods, podKey(p.Pod))
}

func (ni *NodeInfo) AcceptAllocated() {
	ni.Idle.Sub(ni.UnAcceptedAllocated)
	ni.UnAcceptedAllocated = EmptyResource()
}

func (ni *NodeInfo) DiscardAllocated() {
	ni.UnAcceptedAllocated = EmptyResource()
}
