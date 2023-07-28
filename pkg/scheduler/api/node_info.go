/*
 Copyright 2021 The Volcano Authors.

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
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/gpushare"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
)

type AllocateFailError struct {
	Reason string
}

func (o *AllocateFailError) Error() string {
	return o.Reason
}

type CSINodeStatusInfo struct {
	CSINodeName  string
	DriverStatus map[string]bool
}

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

	Allocatable   *Resource
	Capacity      *Resource
	ResourceUsage *NodeUsage

	Tasks             map[TaskID]*TaskInfo
	NumaInfo          *NumatopoInfo
	NumaChgFlag       NumaChgFlag
	NumaSchedulerInfo *NumatopoInfo
	RevocableZone     string

	// Used to store custom information
	Others map[string]interface{}
	//SharedDevices map[string]SharedDevicePool

	// enable node resource oversubscription
	OversubscriptionNode bool
	// OfflineJobEvicting true means node resource usage too high then dispatched pod can not use oversubscription resource
	OfflineJobEvicting bool

	// Resource Oversubscription feature: the Oversubscription Resource reported in annotation
	OversubscriptionResource *Resource

	// ImageStates holds the entry of an image if and only if this image is on the node. The entry can be used for
	// checking an image's existence and advanced usage (e.g., image locality scheduling policy) based on the image
	// state information.
	ImageStates map[string]*k8sframework.ImageStateSummary
}

// FutureIdle returns resources that will be idle in the future:
//
// That is current idle resources plus released resources minus pipelined resources.
func (ni *NodeInfo) FutureIdle() *Resource {
	return ni.Idle.Clone().Add(ni.Releasing).Sub(ni.Pipelined)
}

// GetNodeAllocatable return node Allocatable without OversubscriptionResource resource
func (ni *NodeInfo) GetNodeAllocatable() *Resource {
	return NewResource(ni.Node.Status.Allocatable)
}

// NodeState defines the current state of node.
type NodeState struct {
	Phase  NodePhase
	Reason string
}

// NodeUsage defines the real load usage of node
type NodeUsage struct {
	CPUUsageAvg map[string]float64
	MEMUsageAvg map[string]float64
}

func (nu *NodeUsage) DeepCopy() *NodeUsage {
	newUsage := &NodeUsage{
		CPUUsageAvg: make(map[string]float64),
		MEMUsageAvg: make(map[string]float64),
	}
	for k, v := range nu.CPUUsageAvg {
		newUsage.CPUUsageAvg[k] = v
	}
	for k, v := range nu.MEMUsageAvg {
		newUsage.MEMUsageAvg[k] = v
	}
	return newUsage
}

// NewNodeInfo is used to create new nodeInfo object
func NewNodeInfo(node *v1.Node) *NodeInfo {
	nodeInfo := &NodeInfo{
		Releasing: EmptyResource(),
		Pipelined: EmptyResource(),
		Idle:      EmptyResource(),
		Used:      EmptyResource(),

		Allocatable:   EmptyResource(),
		Capacity:      EmptyResource(),
		ResourceUsage: &NodeUsage{},

		OversubscriptionResource: EmptyResource(),
		Tasks:                    make(map[TaskID]*TaskInfo),

		Others:      make(map[string]interface{}),
		ImageStates: make(map[string]*k8sframework.ImageStateSummary),
	}

	nodeInfo.setOversubscription(node)

	if node != nil {
		nodeInfo.Name = node.Name
		nodeInfo.Node = node
		nodeInfo.Idle = NewResource(node.Status.Allocatable).Add(nodeInfo.OversubscriptionResource)
		nodeInfo.Allocatable = NewResource(node.Status.Allocatable).Add(nodeInfo.OversubscriptionResource)
		nodeInfo.Capacity = NewResource(node.Status.Capacity).Add(nodeInfo.OversubscriptionResource)
	}
	nodeInfo.setNodeOthersResource(node)
	nodeInfo.setNodeState(node)
	nodeInfo.setRevocableZone(node)

	return nodeInfo
}

// RefreshNumaSchedulerInfoByCrd used to update scheduler numa information based the CRD numatopo
func (ni *NodeInfo) RefreshNumaSchedulerInfoByCrd() {
	if ni.NumaInfo == nil {
		ni.NumaSchedulerInfo = nil
		return
	}

	tmp := ni.NumaInfo.DeepCopy()
	if ni.NumaChgFlag == NumaInfoMoreFlag {
		ni.NumaSchedulerInfo = tmp
	} else if ni.NumaChgFlag == NumaInfoLessFlag {
		numaResMap := ni.NumaSchedulerInfo.NumaResMap
		for resName, resInfo := range tmp.NumaResMap {
			klog.V(5).Infof("resource %s Allocatable : current %v new %v on node %s",
				resName, numaResMap[resName], resInfo, ni.Name)
			if numaResMap[resName].Allocatable.Size() >= resInfo.Allocatable.Size() {
				numaResMap[resName].Allocatable = resInfo.Allocatable.Clone()
				numaResMap[resName].Capacity = resInfo.Capacity
			}
		}
	}

	ni.NumaChgFlag = NumaInfoResetFlag
}

// Clone used to clone nodeInfo Object
func (ni *NodeInfo) Clone() *NodeInfo {
	res := NewNodeInfo(ni.Node)

	for _, p := range ni.Tasks {
		res.AddTask(p)
	}
	if ni.NumaInfo != nil {
		res.NumaInfo = ni.NumaInfo.DeepCopy()
	}
	if ni.ResourceUsage != nil {
		res.ResourceUsage = ni.ResourceUsage.DeepCopy()
	}

	if ni.NumaSchedulerInfo != nil {
		res.NumaSchedulerInfo = ni.NumaSchedulerInfo.DeepCopy()
		klog.V(5).Infof("node[%s]", ni.Name)
		for resName, resInfo := range res.NumaSchedulerInfo.NumaResMap {
			klog.V(5).Infof("current resource %s : %v", resName, resInfo)
		}

		klog.V(5).Infof("current Policies : %v", res.NumaSchedulerInfo.Policies)
	}

	klog.V(5).Infof("imageStates is %v", res.ImageStates)

	res.Others = ni.CloneOthers()
	res.ImageStates = ni.CloneImageSummary()
	return res
}

// Ready returns whether node is ready for scheduling
func (ni *NodeInfo) Ready() bool {
	return ni.State.Phase == Ready
}

func (ni *NodeInfo) setRevocableZone(node *v1.Node) {
	if node == nil {
		klog.Warningf("the argument node is null.")
		return
	}

	revocableZone := ""
	if len(node.Labels) > 0 {
		if value, found := node.Labels[v1beta1.RevocableZone]; found {
			revocableZone = value
		}
	}
	ni.RevocableZone = revocableZone
}

// Check node if enable Oversubscription and set Oversubscription resources
// Only support oversubscription cpu and memory resource for this version
func (ni *NodeInfo) setOversubscription(node *v1.Node) {
	if node == nil {
		return
	}

	ni.OversubscriptionNode = false
	ni.OfflineJobEvicting = false
	if len(node.Labels) > 0 {
		if value, found := node.Labels[OversubscriptionNode]; found {
			b, err := strconv.ParseBool(value)
			if err == nil {
				ni.OversubscriptionNode = b
			} else {
				ni.OversubscriptionNode = false
			}
			klog.V(5).Infof("Set node %s Oversubscription to %v", node.Name, ni.OversubscriptionNode)
		}
	}

	if len(node.Annotations) > 0 {
		if value, found := node.Annotations[OfflineJobEvicting]; found {
			b, err := strconv.ParseBool(value)
			if err == nil {
				ni.OfflineJobEvicting = b
			} else {
				ni.OfflineJobEvicting = false
			}
			klog.V(5).Infof("Set node %s OfflineJobEvicting to %v", node.Name, ni.OfflineJobEvicting)
		}
		if value, found := node.Annotations[OversubscriptionCPU]; found {
			ni.OversubscriptionResource.MilliCPU, _ = strconv.ParseFloat(value, 64)
			klog.V(5).Infof("Set node %s Oversubscription CPU to %v", node.Name, ni.OversubscriptionResource.MilliCPU)
		}
		if value, found := node.Annotations[OversubscriptionMemory]; found {
			ni.OversubscriptionResource.Memory, _ = strconv.ParseFloat(value, 64)
			klog.V(5).Infof("Set node %s Oversubscription Memory to %v", node.Name, ni.OversubscriptionResource.Memory)
		}
	}
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
	if ok, resources := ni.Used.LessEqualWithResourcesName(ni.Allocatable, Zero); !ok {
		klog.ErrorS(nil, "Node out of sync", "name", ni.Name, "resources", resources)
	}

	// If node not ready, e.g. power off
	for _, cond := range node.Status.Conditions {
		if cond.Type == v1.NodeReady && cond.Status != v1.ConditionTrue {
			ni.State = NodeState{
				Phase:  NotReady,
				Reason: "NotReady",
			}
			klog.Warningf("set the node %s status to %s.", node.Name, NotReady.String())
			return
		}
	}

	// Node is ready (ignore node conditions because of taint/toleration)
	ni.State = NodeState{
		Phase:  Ready,
		Reason: "",
	}

	klog.V(4).Infof("set the node %s status to %s.", node.Name, Ready.String())
}

// SetNode sets kubernetes node object to nodeInfo object
func (ni *NodeInfo) SetNode(node *v1.Node) {
	ni.setNodeState(node)
	if !ni.Ready() {
		klog.Warningf("Failed to set node info for %s, phase: %s, reason: %s",
			ni.Name, ni.State.Phase, ni.State.Reason)
		return
	}

	// Dry run, make sure all fields other than `State` are in the original state.
	copy := ni.Clone()
	copy.setNode(node)
	copy.setNodeState(node)
	if !copy.Ready() {
		klog.Warningf("SetNode makes node %s not ready, phase: %s, reason: %s",
			copy.Name, copy.State.Phase, copy.State.Reason)
		// Set state of node to !Ready, left other fields untouched
		ni.State = copy.State
		return
	}

	ni.setNode(node)
}

// setNodeOthersResource initialize sharable devices
func (ni *NodeInfo) setNodeOthersResource(node *v1.Node) {
	IgnoredDevicesList = []string{}
	ni.Others[GPUSharingDevice] = gpushare.NewGPUDevices(ni.Name, node)
	ni.Others[vgpu.DeviceName] = vgpu.NewGPUDevices(ni.Name, node)
	IgnoredDevicesList = append(IgnoredDevicesList, ni.Others[GPUSharingDevice].(Devices).GetIgnoredDevices()...)
	IgnoredDevicesList = append(IgnoredDevicesList, ni.Others[vgpu.DeviceName].(Devices).GetIgnoredDevices()...)
}

// setNode sets kubernetes node object to nodeInfo object without assertion
func (ni *NodeInfo) setNode(node *v1.Node) {
	ni.setOversubscription(node)
	ni.setNodeOthersResource(node)
	ni.setRevocableZone(node)

	ni.Name = node.Name
	ni.Node = node

	ni.Allocatable = NewResource(node.Status.Allocatable).Add(ni.OversubscriptionResource)
	ni.Capacity = NewResource(node.Status.Capacity).Add(ni.OversubscriptionResource)
	ni.Releasing = EmptyResource()
	ni.Pipelined = EmptyResource()
	ni.Idle = NewResource(node.Status.Allocatable).Add(ni.OversubscriptionResource)
	ni.Used = EmptyResource()

	for _, ti := range ni.Tasks {
		switch ti.Status {
		case Releasing:
			ni.allocateIdleResource(ti)
			ni.Releasing.Add(ti.Resreq)
			ni.Used.Add(ti.Resreq)
			ni.addResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Add(ti.Resreq)
		default:
			ni.allocateIdleResource(ti)
			ni.Used.Add(ti.Resreq)
			ni.addResource(ti.Pod)
		}
	}
}

func (ni *NodeInfo) allocateIdleResource(ti *TaskInfo) {
	ok, resources := ti.Resreq.LessEqualWithResourcesName(ni.Idle, Zero)
	if ok {
		ni.Idle.sub(ti.Resreq)
		return
	}

	ni.Idle.sub(ti.Resreq)
	klog.ErrorS(nil, "Idle resources turn into negative after allocated",
		"nodeName", ni.Name, "task", klog.KObj(ti.Pod), "resources", resources, "idle", ni.Idle.String(), "req", ti.Resreq.String())
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
			ni.allocateIdleResource(ti)
			ni.Releasing.Add(ti.Resreq)
			ni.Used.Add(ti.Resreq)
			ni.addResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Add(ti.Resreq)
		case Binding:
			// When task in Binding status, it will bind to node, we should double-check whether idle resources are enough to put task before bind to apiserver.
			if ok, resNames := ti.Resreq.LessEqualWithResourcesName(ni.Idle, Zero); !ok {
				return fmt.Errorf("node %s resources %v are not enough to put task <%s/%s>, idle: %s, req: %s", ni.Name, resNames, ti.Namespace, ti.Name, ni.Idle.String(), ti.Resreq.String())
			}
			ni.allocateIdleResource(ti)
			ni.Used.Add(ti.Resreq)
			ni.addResource(ti.Pod)
		default:
			ni.allocateIdleResource(ti)
			ni.Used.Add(ti.Resreq)
			ni.addResource(ti.Pod)
		}
	}

	if ni.NumaInfo != nil {
		ni.NumaInfo.AddTask(ti)
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
		klog.Warningf("failed to find task <%v/%v> on host <%v>",
			ti.Namespace, ti.Name, ni.Name)
		return nil
	}

	if ni.Node != nil {
		switch task.Status {
		case Releasing:
			ni.Releasing.Sub(task.Resreq)
			ni.Idle.Add(task.Resreq)
			ni.Used.Sub(task.Resreq)
			ni.subResource(ti.Pod)
		case Pipelined:
			ni.Pipelined.Sub(task.Resreq)
		default:
			ni.Idle.Add(task.Resreq)
			ni.Used.Sub(task.Resreq)
			ni.subResource(ti.Pod)
		}
	}

	if ni.NumaInfo != nil {
		ni.NumaInfo.RemoveTask(ti)
	}

	delete(ni.Tasks, key)

	return nil
}

// addResource is used to add sharable devices
func (ni *NodeInfo) addResource(pod *v1.Pod) {
	ni.Others[GPUSharingDevice].(Devices).AddResource(pod)
	ni.Others[vgpu.DeviceName].(Devices).AddResource(pod)
}

// subResource is used to substract sharable devices
func (ni *NodeInfo) subResource(pod *v1.Pod) {
	ni.Others[GPUSharingDevice].(Devices).SubResource(pod)
	ni.Others[vgpu.DeviceName].(Devices).SubResource(pod)
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

	return fmt.Sprintf("Node (%s): allocatable<%v> idle <%v>, used <%v>, releasing <%v>, oversubscribution <%v>, "+
		"state <phase %s, reaseon %s>, oversubscributionNode <%v>, offlineJobEvicting <%v>,taints <%v>%s, imageStates %v",
		ni.Name, ni.Allocatable, ni.Idle, ni.Used, ni.Releasing, ni.OversubscriptionResource, ni.State.Phase, ni.State.Reason, ni.OversubscriptionNode, ni.OfflineJobEvicting, ni.Node.Spec.Taints, tasks, ni.ImageStates)
}

// Pods returns all pods running in that node
func (ni *NodeInfo) Pods() (pods []*v1.Pod) {
	for _, t := range ni.Tasks {
		pods = append(pods, t.Pod)
	}

	return
}

// CloneImageSummary Clone Image State
func (ni *NodeInfo) CloneImageSummary() map[string]*k8sframework.ImageStateSummary {
	nodeImageStates := make(map[string]*k8sframework.ImageStateSummary)
	for imageName, summary := range ni.ImageStates {
		newImageSummary := &k8sframework.ImageStateSummary{
			Size:     summary.Size,
			NumNodes: summary.NumNodes,
		}
		nodeImageStates[imageName] = newImageSummary
	}
	return nodeImageStates
}

// CloneOthers clone other map resources
func (ni *NodeInfo) CloneOthers() map[string]interface{} {
	others := make(map[string]interface{})
	for k, v := range ni.Others {
		others[k] = v
	}
	return others
}

// Clone clone csi node status info
func (cs *CSINodeStatusInfo) Clone() *CSINodeStatusInfo {
	newcs := &CSINodeStatusInfo{
		CSINodeName:  cs.CSINodeName,
		DriverStatus: make(map[string]bool),
	}
	for k, v := range cs.DriverStatus {
		newcs.DriverStatus[k] = v
	}
	return newcs
}
