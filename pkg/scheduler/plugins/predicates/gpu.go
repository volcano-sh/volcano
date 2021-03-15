/*
Copyright 2020 The Kubernetes Authors.

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

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// checkNodeGPUSharingPredicate checks if a pod with gpu requirement can be scheduled on a node.
func checkNodeGPUSharingPredicate(pod *v1.Pod, nodeInfo *api.NodeInfo) (bool, error) {
	// no gpu sharing request
	if api.GetGPUMemoryOfPod(pod) <= 0 {
		return true, nil
	}
	ids := predicateGPUbyMemory(pod, nodeInfo)
	if ids == nil {
		return false, fmt.Errorf("no enough gpu memory on node %s", nodeInfo.Name)
	}
	return true, nil
}

func checkNodeGPUNumberPredicate(pod *v1.Pod, nodeInfo *api.NodeInfo) (bool, error) {
	//no gpu number request
	if api.GetGPUNumberOfPod(pod) <= 0 {
		return true, nil
	}
	ids := predicateGPUbyNumber(pod, nodeInfo)
	if ids == nil {
		return false, fmt.Errorf("no enough gpu number on node %s", nodeInfo.Name)
	}
	return true, nil
}

// predicateGPU returns the available GPU ID
func predicateGPUbyMemory(pod *v1.Pod, node *api.NodeInfo) []int {
	gpuRequest := api.GetGPUMemoryOfPod(pod)
	allocatableGPUs := node.GetDevicesIdleGPUMemory()

	var devIDs []int

	for devID := 0; devID < len(allocatableGPUs); devID++ {
		availableGPU, ok := allocatableGPUs[devID]
		if ok {
			if availableGPU >= gpuRequest {
				devIDs = append(devIDs, devID)
				return devIDs
			}
		}
	}

	return nil
}

// predicateGPU returns the available GPU IDs
func predicateGPUbyNumber(pod *v1.Pod, node *api.NodeInfo) []int {
	gpuRequest := api.GetGPUNumberOfPod(pod)
	allocatableGPUs := node.GetDevicesIdleGPUs()

	var devIDs []int

	if len(allocatableGPUs) < gpuRequest {
		klog.Errorf("Not enough gpu cards")
		return nil
	}

	for devID := 0; devID < len(allocatableGPUs); devID++ {
		devIDs = append(devIDs, allocatableGPUs[devID])
		if len(devIDs) == gpuRequest {
			return devIDs
		}
	}

	return nil
}
