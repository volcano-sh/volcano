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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

// NodeInfo is node level aggregated information.
type NodeInfo struct {
	Name string
	Node *v1.Node

	// The state of node
	State NodeState

	// The releasing resource on that node
	Releasing *Resource
	// The pipelined resource on that node
	Pipelined *Resource
	// The idle resource on that node
	Idle *Resource
	// The used resource on that node, including running and terminating
	// pods
	Used *Resource

	Allocatable *Resource
	Capability  *Resource

	Tasks map[TaskID]*TaskInfo

	// Used to store custom information
	Others     map[string]interface{}
	GPUDevices map[int]*GPUDevice
}

// FutureIdle returns resources that will be idle in the future:
//
// That is current idle resources plus released resources minus pipelined resources.
func (ni *NodeInfo) FutureIdle() *Resource {
	return ni.Idle.Clone().Add(ni.Releasing).Sub(ni.Pipelined)
}

// NodeState defines the current state of node.
type NodeState struct {
	Phase  NodePhase
	Reason string
}

// NewNodeInfo is used to create new nodeInfo object
func NewNodeInfo(node *v1.Node) *NodeInfo {
	nodeinfo := &NodeInfo{
		Releasing: EmptyResource(),
		Pipelined: EmptyResource(),
		Idle:      EmptyResource(),
		Used:      EmptyResource(),

		Allocatable: EmptyResource(),
		Capability:  EmptyResource(),

		Tasks: make(map[TaskID]*TaskInfo),

		GPUDevices: make(map[int]*GPUDevice),
	}

	if node != nil {
		nodeinfo.Name = node.Name
		nodeinfo.Node = node
		nodeinfo.Idle = NewResource(node.Status.Allocatable)
		nodeinfo.Allocatable = NewResource(node.Status.Allocatable)
		nodeinfo.Capability = NewResource(node.Status.Capacity)
	}
	nodeinfo.setNodeGPUInfo(node)
	nodeinfo.setNodeState(node)

	return nodeinfo
}

// Clone used to clone nodeInfo Object
func (ni *NodeInfo) Clone() *NodeInfo {
	res := NewNodeInfo(ni.Node)

	for _, p := range ni.Tasks {
		res.AddTask(p)
	}
	return res
}

// Ready returns whether node is ready for scheduling
func (ni *NodeInfo) Ready() bool {
	return ni.State.Phase == Ready
}

func (ni *NodeInfo) setNodeState(node *v1.Node) {
	// If node is nil, the node is un-initialized in cache
	if node == nil {
		ni.State = NodeState{
			Phase:  NotReady,
			Reason: "UnInitialized",
		}
		return
	}

	// set NodeState according to resources
	if !ni.Used.LessEqual(NewResource(node.Status.Allocatable)) {
		ni.State = NodeState{
			Phase:  NotReady,
			Reason: "OutOfSync",
		}
		return
	}

	// If node not ready, e.g. power off
	for _, cond := range node.Status.Conditions {
		if cond.Type == v1.NodeReady && cond.Status != v1.ConditionTrue {
			ni.State = NodeState{
				Phase:  NotReady,
				Reason: "NotReady",
			}
			return
		}
	}

	// Node is ready (ignore node conditions because of taint/toleration)
	ni.State = NodeState{
		Phase:  Ready,
		Reason: "",
	}
}

func (ni *NodeInfo) setNodeGPUInfo(node *v1.Node) {
	if node == nil {
		return
	}
	memory, ok := node.Status.Capacity[VolcanoGPUResource]
	if !ok {
		return
	}
	totalMemory := memory.Value()

	res, ok := node.Status.Capacity[VolcanoGPUNumber]
	if !ok {
		return
	}
	gpuNumber := res.Value()
	if gpuNumber == 0 {
		klog.Warningf("invalid %s=%s", VolcanoGPUNumber, res.String())
		return
	}

	memoryPerCard := uint(totalMemory / gpuNumber)
	for i := 0; i < int(gpuNumber); i++ {
		ni.GPUDevices[i] = NewGPUDevice(i, memoryPerCard)
	}
}

// SetNode sets kubernetes node object to nodeInfo object
func (ni *NodeInfo) SetNode(node *v1.Node) {
	ni.setNodeState(node)
	ni.setNodeGPUInfo(node)

	if !ni.Ready() {
		klog.Warningf("Failed to set node info, phase: %s, reason: %s",
			ni.State.Phase, ni.State.Reason)
		return
	}

	ni.Name = node.Name
	ni.Node = node

	ni.Allocatable = NewResource(node.Status.Allocatable)
	ni.Capability = NewResource(node.Status.Capacity)
	ni.Releasing = EmptyResource()
	ni.Pipelined = EmptyResource()
	ni.Idle = NewResource(node.Status.Allocatable)
	ni.Used = EmptyResource()

	for _, ti := range ni.Tasks {
		switch ti.Status {
		case Releasing:
			ni.Idle.Sub(ti.Resreq)
			ni.Releasing.Add(ti.Resreq)
			ni.Used.Add(ti.Resreq)
			ni.AddGPUResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Add(ti.Resreq)
		default:
			ni.Idle.Sub(ti.Resreq)
			ni.Used.Add(ti.Resreq)
			ni.AddGPUResource(ti.Pod)
		}
	}
}

func (ni *NodeInfo) allocateIdleResource(ti *TaskInfo) error {
	if ti.Resreq.LessEqual(ni.Idle) {
		ni.Idle.Sub(ti.Resreq)
		return nil
	}

	return fmt.Errorf("selected node NotReady")
}

// AddTask is used to add a task in nodeInfo object
//
// If error occurs both task and node are guaranteed to be in the original state.
func (ni *NodeInfo) AddTask(task *TaskInfo) error {
	if len(task.NodeName) > 0 && len(ni.Name) > 0 && task.NodeName != ni.Name {
		return fmt.Errorf("task <%v/%v> already on different node <%v>",
			task.Namespace, task.Name, task.NodeName)
	}

	key := PodKey(task.Pod)
	if _, found := ni.Tasks[key]; found {
		return fmt.Errorf("task <%v/%v> already on node <%v>",
			task.Namespace, task.Name, ni.Name)
	}

	// Node will hold a copy of task to make sure the status
	// change will not impact resource in node.
	ti := task.Clone()

	if ni.Node != nil {
		switch ti.Status {
		case Releasing:
			if err := ni.allocateIdleResource(ti); err != nil {
				return err
			}
			ni.Releasing.Add(ti.Resreq)
			ni.Used.Add(ti.Resreq)
			ni.AddGPUResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Add(ti.Resreq)
		default:
			if err := ni.allocateIdleResource(ti); err != nil {
				return err
			}
			ni.Used.Add(ti.Resreq)
			ni.AddGPUResource(ti.Pod)
		}
	}

	// Update task node name upon successful task addition.
	task.NodeName = ni.Name
	ti.NodeName = ni.Name
	ni.Tasks[key] = ti

	return nil
}

// RemoveTask used to remove a task from nodeInfo object.
//
// If error occurs both task and node are guaranteed to be in the original state.
func (ni *NodeInfo) RemoveTask(ti *TaskInfo) error {
	key := PodKey(ti.Pod)

	task, found := ni.Tasks[key]
	if !found {
		return fmt.Errorf("failed to find task <%v/%v> on host <%v>",
			ti.Namespace, ti.Name, ni.Name)
	}

	if ni.Node != nil {
		switch task.Status {
		case Releasing:
			ni.Releasing.Sub(task.Resreq)
			ni.Idle.Add(task.Resreq)
			ni.Used.Sub(task.Resreq)
			ni.AddGPUResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Sub(task.Resreq)
		default:
			ni.Idle.Add(task.Resreq)
			ni.Used.Sub(task.Resreq)
			ni.SubGPUResource(ti.Pod)
		}
	}

	delete(ni.Tasks, key)

	return nil
}

// UpdateTask is used to update a task in nodeInfo object.
//
// If error occurs both task and node are guaranteed to be in the original state.
func (ni *NodeInfo) UpdateTask(ti *TaskInfo) error {
	if err := ni.RemoveTask(ti); err != nil {
		return err
	}

	if err := ni.AddTask(ti); err != nil {
		// This should never happen if task removal was successful,
		// because only possible error during task addition is when task is still on a node.
		klog.Fatalf("Failed to add Task <%s,%s> to Node <%s> during task update",
			ti.Namespace, ti.Name, ni.Name)
	}
	return nil
}

// String returns nodeInfo details in string format
func (ni NodeInfo) String() string {
	tasks := ""

	i := 0
	for _, task := range ni.Tasks {
		tasks += fmt.Sprintf("\n\t %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Node (%s): idle <%v>, used <%v>, releasing <%v>, state <phase %s, reaseon %s>, taints <%v>%s",
		ni.Name, ni.Idle, ni.Used, ni.Releasing, ni.State.Phase, ni.State.Reason, ni.Node.Spec.Taints, tasks)

}

// Pods returns all pods running in that node
func (ni *NodeInfo) Pods() (pods []*v1.Pod) {
	for _, t := range ni.Tasks {
		pods = append(pods, t.Pod)
	}

	return
}

// GetDevicesIdleGPUMemory returns all the idle GPU memory by gpu card.
func (ni *NodeInfo) GetDevicesIdleGPUMemory() map[int]uint {
	devicesAllGPUMemory := ni.getDevicesAllGPUMemory()
	devicesUsedGPUMemory := ni.getDevicesUsedGPUMemory()
	res := map[int]uint{}
	for id, allMemory := range devicesAllGPUMemory {
		if usedMemory, found := devicesUsedGPUMemory[id]; found {
			res[id] = allMemory - usedMemory
		}
	}
	return res
}

func (ni *NodeInfo) getDevicesUsedGPUMemory() map[int]uint {
	res := map[int]uint{}
	for _, device := range ni.GPUDevices {
		res[device.ID] = device.getUsedGPUMemory()
	}
	return res
}

func (ni *NodeInfo) getDevicesAllGPUMemory() map[int]uint {
	res := map[int]uint{}
	for _, device := range ni.GPUDevices {
		res[device.ID] = device.Memory
	}
	return res
}

// AddGPUResource adds the pod to GPU pool if it is assigned
func (ni *NodeInfo) AddGPUResource(pod *v1.Pod) {
	gpuRes := GetGPUResourceOfPod(pod)
	if gpuRes > 0 {
		id := GetGPUIndex(pod)
		if dev := ni.GPUDevices[id]; dev != nil {
			dev.PodMap[string(pod.UID)] = pod
		}
	}
}

// SubGPUResource frees the gpu hold by the pod
func (ni *NodeInfo) SubGPUResource(pod *v1.Pod) {
	gpuRes := GetGPUResourceOfPod(pod)
	if gpuRes > 0 {
		id := GetGPUIndex(pod)
		if dev := ni.GPUDevices[id]; dev != nil {
			delete(dev.PodMap, string(pod.UID))
		}
	}
}
