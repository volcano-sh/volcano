/*
Copyright 2024 The Volcano Authors.

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

package node

import (
	"fmt"
	"sort"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
	utilpod "volcano.sh/volcano/pkg/agent/utils/pod"
)

func IsOverused(resName v1.ResourceName, resList *ResourceList) bool {
	podsRes, e1 := resList.TotalPodsRequest[resName]
	nodeRes, e2 := resList.TotalNodeRes[resName]

	if !e1 || !e2 {
		return false
	}

	return podsRes.MilliValue() > nodeRes.MilliValue()
}

func UseExtendResource(resName v1.ResourceName, resList *ResourceList) bool {
	if resName == v1.ResourceCPU {
		cpuReq, cpuExists := (*resList).TotalPodsRequest[apis.ExtendResourceCPU]
		return cpuExists && !cpuReq.IsZero()
	}
	if resName == v1.ResourceMemory {
		memReq, memoryExists := (*resList).TotalPodsRequest[apis.ExtendResourceMemory]
		return memoryExists && !memReq.IsZero()
	}
	return false
}

// GetAnnotationOverSubscription get current overSubscription resources on annotation.
func GetAnnotationOverSubscription(node *v1.Node) apis.Resource {
	resources := make(apis.Resource)
	resources[v1.ResourceCPU] = 0
	resources[v1.ResourceMemory] = 0
	if len(node.Annotations) == 0 {
		return resources
	}

	if value, found := node.Annotations[apis.NodeOverSubscriptionCPUKey]; found {
		b, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			resources[v1.ResourceCPU] = b
		} else {
			klog.InfoS("Invalid annotation "+apis.NodeOverSubscriptionCPUKey, "value", value)
		}
	}

	if value, found := node.Annotations[apis.NodeOverSubscriptionMemoryKey]; found {
		b, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			resources[v1.ResourceMemory] = b
		} else {
			klog.InfoS("Invalid annotation "+apis.NodeOverSubscriptionMemoryKey, "value", value)
		}
	}
	return resources
}

// GetNodeStatusOverSubscription get node status's overSubscription extend resources.
func GetNodeStatusOverSubscription(node *v1.Node) apis.Resource {
	resources := make(apis.Resource)
	resources[v1.ResourceCPU] = 0
	resources[v1.ResourceMemory] = 0
	if node.Status.Capacity == nil {
		return resources
	}

	if value, found := node.Status.Capacity[apis.ExtendResourceCPU]; found {
		resources[v1.ResourceCPU] = value.Value()
	}

	if value, found := node.Status.Capacity[apis.ExtendResourceMemory]; found {
		resources[v1.ResourceMemory] = value.Value()
	}
	return resources
}

// GetTotalNodeResource return total allocatable resource on the node, include overSubscription resource if include OverSubscription=true
func GetTotalNodeResource(node *v1.Node, includeOverSubscription bool) v1.ResourceList {
	total := make(v1.ResourceList)

	cpu := node.Status.Allocatable[v1.ResourceCPU]
	memory := node.Status.Allocatable[v1.ResourceMemory]

	if includeOverSubscription {
		overSubscriptionCPU := resource.MustParse("0")
		overSubscriptionMemory := resource.MustParse("0")
		if node.Annotations != nil {
			if node.Annotations[apis.NodeOverSubscriptionCPUKey] != "" {
				overSubscriptionCPU = resource.MustParse(fmt.Sprintf("%sm", node.Annotations[apis.NodeOverSubscriptionCPUKey]))
			}
			if node.Annotations[apis.NodeOverSubscriptionMemoryKey] != "" {
				overSubscriptionMemory = resource.MustParse(node.Annotations[apis.NodeOverSubscriptionMemoryKey])
			}
		}

		cpu.Add(overSubscriptionCPU)
		memory.Add(overSubscriptionMemory)
	}

	total[v1.ResourceCPU] = cpu
	total[v1.ResourceMemory] = memory

	return total
}

func GetLatestPodsAndResList(node *v1.Node, getPodFunc utilpod.ActivePods, resType v1.ResourceName) ([]*v1.Pod, *ResourceList, error) {
	pods, err := getPodFunc()
	if err != nil {
		return nil, nil, err
	}
	_, preemptablePods := utilpod.FilterOutPreemptablePods(pods)
	// TODO: Add more pods eviction sort policy.
	if resType == v1.ResourceCPU {
		sort.Sort(utilpod.SortedPodsByRequestCPU(preemptablePods))
	} else {
		sort.Sort(utilpod.SortedPodsByRequestMemory(preemptablePods))
	}
	resList := getResourceList(node, pods)
	return preemptablePods, resList, nil
}

func getResourceList(node *v1.Node, pods []*v1.Pod) *ResourceList {
	resList := new(ResourceList)
	fns := []utilpod.FilterPodsFunc{utilpod.IncludeOversoldPodFn(true), utilpod.IncludeGuaranteedPodsFn(true)}
	resList.TotalPodsRequest = utilpod.GetTotalRequest(pods, fns, apis.OverSubscriptionResourceTypesIncludeExtendResources)
	resList.TotalNodeRes = GetTotalNodeResource(node, false)
	return resList
}
