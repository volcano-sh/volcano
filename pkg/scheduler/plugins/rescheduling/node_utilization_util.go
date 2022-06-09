/*
Copyright 2022 The Volcano Authors.

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

package rescheduling

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
	v1qos "k8s.io/kubernetes/pkg/apis/core/v1/helper/qos"
	"sort"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type NodeUtilization struct {
	nodeInfo    *v1.Node
	utilization map[v1.ResourceName]float64
	pods        []*v1.Pod
}

type thresholdFilter func(*NodeUtilization, interface{}) bool

type isContinueEviction func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool

// groupNodesByUtilization divides the nodes into two groups by resource utilization filters
func groupNodesByUtilization(nodeUtilizationList []*NodeUtilization, lowThresholdFilter, highThresholdFilter thresholdFilter, config interface{}) ([]*NodeUtilization, []*NodeUtilization) {
	lowNodes := make([]*NodeUtilization, 0)
	highNodes := make([]*NodeUtilization, 0)

	for _, nodeUtilization := range nodeUtilizationList {
		if lowThresholdFilter(nodeUtilization, config) {
			lowNodes = append(lowNodes, nodeUtilization)
		} else if highThresholdFilter(nodeUtilization, config) {
			highNodes = append(highNodes, nodeUtilization)
		}
	}
	klog.V(4).Infof("lowNodes: %v\n", lowNodes)
	klog.V(4).Infof("highNodes: %v\n", highNodes)
	return lowNodes, highNodes
}

// getNodeUtilization returns all node resource utilization list
func getNodeUtilization() []*NodeUtilization {
	nodeUtilizationList := make([]*NodeUtilization, 0)
	for _, nodeInfo := range Session.Nodes {
		nodeUtilization := &NodeUtilization{
			nodeInfo: nodeInfo.Node,
			utilization: map[v1.ResourceName]float64{
				v1.ResourceCPU:    nodeInfo.ResourceUsage.CPUUsageAvg[Interval],
				v1.ResourceMemory: nodeInfo.ResourceUsage.MEMUsageAvg[Interval],
			},
			pods: nodeInfo.Pods(),
		}
		nodeUtilizationList = append(nodeUtilizationList, nodeUtilization)
		klog.V(4).Infof("node: %s, cpu: %v, memory: %v\n", nodeUtilization.nodeInfo.Name, nodeUtilization.utilization[v1.ResourceCPU], nodeUtilization.utilization[v1.ResourceMemory])
	}
	return nodeUtilizationList
}

// evictPodsFromSourceNodes evict pods from source nodes to target nodes according to priority and QoS
func evictPodsFromSourceNodes(sourceNodes, targetNodes []*NodeUtilization, tasks []*api.TaskInfo, evictionCon isContinueEviction, config interface{}) []*api.TaskInfo {
	resourceNames := []v1.ResourceName{
		v1.ResourceCPU,
		v1.ResourceMemory,
	}
	utilizationConfig := parseArgToConfig(config)
	totalAllocatableResource := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    {},
		v1.ResourceMemory: {},
	}
	for _, node := range targetNodes {
		nodeCapacity := getNodeCapacity(node.nodeInfo)
		for _, rName := range resourceNames {
			totalAllocatableResource[rName].Add(*convertPercentToQuan(rName, utilizationConfig.TargetThresholds[string(rName)], nodeCapacity))
			totalAllocatableResource[rName].Sub(*convertPercentToQuan(rName, node.utilization[rName], nodeCapacity))
		}
	}
	klog.V(4).Infof("totalAllocatableResource: %s", totalAllocatableResource)

	// sort the source nodes in descending order
	sortNodes(sourceNodes)

	// victims select algorithm:
	// 1. Evict pods from nodes with high utilization to low utilization
	// 2. As to one node, evict pods from low priority to high priority. If the priority is same, evict pods according to QoS from low to high
	victims := make([]*api.TaskInfo, 0)
	for _, node := range sourceNodes {
		if len(node.pods) == 0 {
			klog.V(4).Infof("No pods can be removed on node: %s", node.nodeInfo.Name)
			continue
		}
		sortPods(node.pods)
		victims = append(victims, evict(node.pods, node, totalAllocatableResource, evictionCon, tasks, config)...)
	}
	klog.V(3).Infof("victims: %v\n", victims)
	return victims
}

// parseArgToConfig returns a nodeUtilizationConfig object from parameters
// TODO: It is just for lowNodeUtilization now, which should be abstracted as a common function.
func parseArgToConfig(config interface{}) *LowNodeUtilizationConf {
	var utilizationConfig *LowNodeUtilizationConf
	if arg, ok := config.(LowNodeUtilizationConf); ok {
		utilizationConfig = &arg
	}
	return utilizationConfig
}

// sortNodes sorts all the nodes according the usage of cpu and memory with weight score
func sortNodes(nodeUtilizationList []*NodeUtilization) {
	cmpFn := func(i, j int) bool {
		return getScoreForNode(i, nodeUtilizationList) > getScoreForNode(j, nodeUtilizationList)
	}
	sort.Slice(nodeUtilizationList, cmpFn)
}

// getScoreForNode returns the score for node which considers only for CPU and memory
func getScoreForNode(index int, nodeUtilizationList []*NodeUtilization) float64 {
	cpuScore := nodeUtilizationList[index].utilization[v1.ResourceCPU]
	memoryScore := nodeUtilizationList[index].utilization[v1.ResourceMemory]
	return cpuScore + memoryScore
}

// sortPods return the pods in order according the priority and QoS
func sortPods(pods []*v1.Pod) {
	cmp := func(i, j int) bool {
		if pods[i].Spec.Priority == nil && pods[j].Spec.Priority != nil {
			return true
		}
		if pods[j].Spec.Priority == nil && pods[i].Spec.Priority != nil {
			return false
		}
		if (pods[j].Spec.Priority == nil && pods[i].Spec.Priority == nil) || (*pods[i].Spec.Priority == *pods[j].Spec.Priority) {
			if v1qos.GetPodQOS(pods[i]) == v1.PodQOSBestEffort {
				return true
			}
			if v1qos.GetPodQOS(pods[i]) == v1.PodQOSBurstable && v1qos.GetPodQOS(pods[j]) == v1.PodQOSGuaranteed {
				return true
			}
			return false
		}
		return *pods[i].Spec.Priority < *pods[j].Spec.Priority
	}
	sort.Slice(pods, cmp)
}

// evict select victims and add to the eviction list
func evict(pods []*v1.Pod, utilization *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, continueEviction isContinueEviction, tasks []*api.TaskInfo, config interface{}) []*api.TaskInfo {
	victims := make([]*api.TaskInfo, 0)
	for _, pod := range pods {
		if !continueEviction(utilization, totalAllocatableResource, config) {
			klog.V(3).Infoln("stop evict pods")
			return victims
		}
		for _, task := range tasks {
			if task.Pod.Name == pod.Name {
				usedCPU := *resource.NewMilliQuantity(int64(task.Resreq.MilliCPU), resource.DecimalSI)
				usedMem := *resource.NewQuantity(int64(task.Resreq.Memory), resource.BinarySI)
				totalAllocatableResource[v1.ResourceCPU].Sub(usedCPU)
				totalAllocatableResource[v1.ResourceMemory].Sub(usedMem)
				utilization.utilization[v1.ResourceCPU] -= convertQuanToPercent(v1.ResourceCPU, &usedCPU, utilization.nodeInfo.Status.Capacity)
				utilization.utilization[v1.ResourceMemory] -= convertQuanToPercent(v1.ResourceMemory, &usedMem, utilization.nodeInfo.Status.Capacity)
				klog.V(4).Infof("totalAllocatableResource: %v\n", totalAllocatableResource)
				klog.V(4).Infof("node: %s, utilization: %v\n", utilization.nodeInfo.Name, utilization.utilization)
				victims = append(victims, task)
				break
			}
		}
	}
	return victims
}

// getNodeCapacity returns node's capacity
func getNodeCapacity(node *v1.Node) v1.ResourceList {
	nodeCapacity := node.Status.Capacity
	if len(node.Status.Allocatable) > 0 {
		nodeCapacity = node.Status.Allocatable
	}
	return nodeCapacity
}

// convertPercentToQuan converts resource percentage to amount
func convertPercentToQuan(rName v1.ResourceName, percent float64, nodeCapacity v1.ResourceList) *resource.Quantity {
	var amount *resource.Quantity
	if rName == v1.ResourceCPU {
		amount = resource.NewMilliQuantity(int64(percent*float64(nodeCapacity.Cpu().MilliValue())*0.01), resource.DecimalSI)
	} else if rName == v1.ResourceMemory {
		amount = resource.NewQuantity(int64(percent*float64(nodeCapacity.Memory().Value())*0.01), resource.BinarySI)
	}
	return amount
}

// convertQuanToPercent converts resource amount to percentage
func convertQuanToPercent(rName v1.ResourceName, amount *resource.Quantity, nodeCapacity v1.ResourceList) float64 {
	var percent float64
	if rName == v1.ResourceCPU {
		percent = amount.AsApproximateFloat64() / nodeCapacity.Cpu().AsApproximateFloat64()
	} else if rName == v1.ResourceMemory {
		percent = amount.AsApproximateFloat64() / nodeCapacity.Memory().AsApproximateFloat64()
	}
	return percent
}
