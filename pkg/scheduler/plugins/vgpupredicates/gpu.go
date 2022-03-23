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

package vgpupredicates

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/vgpupredicates/vgpuutil"
)

// checkNodeGPUSharingPredicate checks if a gpu sharing pod can be scheduled on a node.
func checkNodeGPUSharingPredicate(pod *v1.Pod, nodeInfo *api.NodeInfo) (bool, error) {
	// no gpu sharing request
	if api.GetGPUResourceOfPod(pod) <= 0 {
		return true, nil
	}

	id := predicateGPU(pod, nodeInfo)
	if len(id) > 0 {
		return false, fmt.Errorf("no enough gpu memory on single device of node %s", nodeInfo.Name)
	}
	return true, nil
}

func foundinarray(ar []int, c int) bool {
	for _, val := range ar {
		if val == c {
			return true
		}
	}
	return false
}

// predicateGPU returns the available GPU ID
func predicateGPU(pod *v1.Pod, node *api.NodeInfo) []int {
	//gpuRequest := api.GetGPUResourceOfPod(pod)
	vgpuRequest := vgpuutil.Resourcereqs(pod)
	allocatableGPUs := node.GetVGPURemains()
	klog.Infof("allocateableGPUs=", allocatableGPUs, "vgpurequest", vgpuRequest)
	res := []int{}

	for _, num := range vgpuRequest {
		if num.Nums == 0 {
			continue
		}
		remains := num.Nums
		for devID := 0; devID < len(allocatableGPUs); devID++ {
			availableGPU := allocatableGPUs[devID]
			if availableGPU.Memory >= num.Memreq {
				availableGPU.Memory = availableGPU.Memory - num.Memreq
				remains--
				if !foundinarray(res, devID) {
					res = append(res, devID)
				}
			}
			if remains == 0 {
				break
			}
		}
		if remains > 0 {
			return []int{}
		}
	}
	return res
}

// appointvGPU
func appointvGPU(pod *v1.Pod, node *api.NodeInfo) ([]int, error) {
	//gpuRequest := api.GetGPUResourceOfPod(pod)
	vgpuRequest := vgpuutil.Resourcereqs(pod)
	allocatableGPUs := node.GetVGPURemains()
	klog.Infof("allocateableGPUs=", allocatableGPUs, "vgpurequest", vgpuRequest)
	res := []int{}
	cdmap := map[int][]*v1.Container{}

	for idx, ctr := range pod.Spec.Containers {
		num := vgpuRequest[idx]
		if num.Nums == 0 {
			continue
		}
		remains := num.Nums
		for devID := 0; devID < len(allocatableGPUs); devID++ {
			availableGPU := allocatableGPUs[devID]
			if availableGPU.Memory >= num.Memreq {
				availableGPU.Memory = availableGPU.Memory - num.Memreq
				remains--
				cdmap[devID] = append(cdmap[devID], &ctr)
				if !foundinarray(res, devID) {
					res = append(res, devID)
				}
			}
			if remains == 0 {
				break
			}
		}
		if remains > 0 {
			return []int{}, nil
		}
	}
	// If success in allocating, then we add ContainerMap and PodMap to corresponding dev
	for devID, val := range cdmap {
		dev, err := api.GetvGPUDevice(*node, devID)
		if err != nil {
			return []int{}, err
		}
		dev.PodMap[string(pod.UID)] = pod
		for _, ctr := range val {
			dev.ContainerMap[ctr.Name] = ctr
		}
	}
	return res, nil
}
