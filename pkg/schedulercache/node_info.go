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

package schedulercache

import (
	"k8s.io/api/core/v1"
)

type PodInfo struct {
	Name      string
	Namespace string

	Pod *v1.Pod

	Request *Resource
}

func NewPodInfo(pod *v1.Pod) *PodInfo {
	req := EmptyResource()

	for _, c := range pod.Spec.Containers {
		req.Add(NewResource(c.Resources.Requests))
	}

	return &PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Pod:       pod,
		Request:   req,
	}
}

func (pi *PodInfo) Clone() *PodInfo {
	return &PodInfo{
		Name:      pi.Name,
		Namespace: pi.Namespace,
		Pod:       pi.Pod,
		Request:   pi.Request.Clone(),
	}
}

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

	Pods []*PodInfo
}

func NewNodeInfo(node *v1.Node) *NodeInfo {
	return &NodeInfo{
		Name: node.Name,
		Node: node,
		Idle: NewResource(node.Status.Allocatable),
		Used: EmptyResource(),

		Allocatable: NewResource(node.Status.Allocatable),
		Capability:  NewResource(node.Status.Capacity),

		Pods: make([]*PodInfo, 10),
	}
}

func (ni *NodeInfo) Clone() *NodeInfo {
	pods := make([]*PodInfo, len(ni.Pods))

	for _, p := range ni.Pods {
		pods = append(pods, p.Clone())
	}

	return &NodeInfo{
		Name:        ni.Name,
		Node:        ni.Node,
		Idle:        ni.Idle.Clone(),
		Used:        ni.Used.Clone(),
		Allocatable: ni.Allocatable.Clone(),
		Capability:  ni.Capability.Clone(),

		Pods: pods,
	}
}

func (ni *NodeInfo) AddPod(pod *PodInfo) {
	ni.Idle.Sub(pod.Request)
	ni.Used.Add(pod.Request)

	ni.Pods = append(ni.Pods, pod)
}

func (ni *NodeInfo) RemovePod(pod *PodInfo) {
	// TODO:
}
