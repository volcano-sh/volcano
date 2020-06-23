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

	"volcano.sh/volcano/pkg/scheduler/api"
)

// checkNodeGPUSharingPredicate checks if a gpu sharing pod can be scheduled on a node.
func checkNodeGPUSharingPredicate(pod *v1.Pod, nodeInfo *api.NodeInfo) (bool, error) {
	// no gpu sharing request
	if api.GetGPUResourceOfPod(pod) <= 0 {
		return true, nil
	}

	id := predicateGPU(pod, nodeInfo)
	if id < 0 {
		return false, fmt.Errorf("no enough gpu memory on single device of node %s", nodeInfo.Name)
	}
	return true, nil
}

// predicateGPU returns the available GPU ID
func predicateGPU(pod *v1.Pod, node *api.NodeInfo) int {
	gpuRequest := api.GetGPUResourceOfPod(pod)
	allocatableGPUs := node.GetDevicesIdleGPUMemory()

	for devID := 0; devID < len(allocatableGPUs); devID++ {
		availableGPU, ok := allocatableGPUs[devID]
		if ok {
			if availableGPU >= gpuRequest {
				return devID
			}
		}
	}

	return -1
}
