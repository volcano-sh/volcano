/*
Copyright 2023 The Volcano Authors.

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

package gpushare

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// getDevicesIdleGPUMemory returns all the idle GPU memory by gpu card.
func getDevicesIdleGPUMemory(gs *GPUDevices) map[int]uint {
	devicesAllGPUMemory := getDevicesAllGPUMemory(gs)
	devicesUsedGPUMemory := getDevicesUsedGPUMemory(gs)
	res := map[int]uint{}
	for id, allMemory := range devicesAllGPUMemory {
		if usedMemory, found := devicesUsedGPUMemory[id]; found {
			res[id] = allMemory - usedMemory
		} else {
			res[id] = allMemory
		}
	}
	return res
}

func getDevicesUsedGPUMemory(gs *GPUDevices) map[int]uint {
	res := map[int]uint{}
	for _, device := range gs.Device {
		res[device.ID] = device.getUsedGPUMemory()
	}
	return res
}

func getDevicesAllGPUMemory(gs *GPUDevices) map[int]uint {
	res := map[int]uint{}
	for _, device := range gs.Device {
		res[device.ID] = device.Memory
	}
	return res
}

// GetDevicesIdleGPU returns all the idle gpu card.
func getDevicesIdleGPUs(gs *GPUDevices) []int {
	res := []int{}
	for _, device := range gs.Device {
		if device.isIdleGPU() {
			res = append(res, device.ID)
		}
	}
	return res
}

// getUnhealthyGPUs returns all the unhealthy GPU id.
func getUnhealthyGPUs(gs *GPUDevices, node *v1.Node) (unhealthyGPUs []int) {
	unhealthyGPUs = []int{}
	devicesStr, ok := node.Annotations[UnhealthyGPUIDs]

	if !ok {
		return
	}

	idsStr := strings.Split(devicesStr, ",")
	for _, sid := range idsStr {
		id, err := strconv.Atoi(sid)
		if err != nil {
			klog.Warningf("Failed to parse unhealthy gpu id %s due to %v", sid, err)
		} else {
			unhealthyGPUs = append(unhealthyGPUs, id)
		}
	}
	return
}

// GetGPUIndex returns the index list of gpu cards
func GetGPUIndex(pod *v1.Pod) []int {
	if len(pod.Annotations) == 0 {
		return nil
	}

	value, found := pod.Annotations[GPUIndex]
	if !found {
		return nil
	}

	ids := strings.Split(value, ",")
	if len(ids) == 0 {
		klog.Errorf("invalid gpu index annotation %s=%s", GPUIndex, value)
		return nil
	}

	idSlice := make([]int, len(ids))
	for idx, id := range ids {
		j, err := strconv.Atoi(id)
		if err != nil {
			klog.Errorf("invalid %s=%s", GPUIndex, value)
			return nil
		}
		idSlice[idx] = j
	}
	return idSlice
}

// checkNodeGPUSharingPredicate checks if a pod with gpu requirement can be scheduled on a node.
func checkNodeGPUSharingPredicate(pod *v1.Pod, gs *GPUDevices) (bool, error) {
	// no gpu sharing request
	if getGPUMemoryOfPod(pod) <= 0 {
		return true, nil
	}
	ids := predicateGPUbyMemory(pod, gs)
	if len(ids) == 0 {
		return false, fmt.Errorf("no enough gpu memory on node %s", gs.Name)
	}
	return true, nil
}

func checkNodeGPUNumberPredicate(pod *v1.Pod, gs *GPUDevices) (bool, error) {
	//no gpu number request
	if getGPUNumberOfPod(pod) <= 0 {
		return true, nil
	}
	ids := predicateGPUbyNumber(pod, gs)
	if len(ids) == 0 {
		return false, fmt.Errorf("no enough gpu number on node %s", gs.Name)
	}
	return true, nil
}

// predicateGPUbyMemory returns the available GPU ID
func predicateGPUbyMemory(pod *v1.Pod, gs *GPUDevices) []int {
	gpuRequest := getGPUMemoryOfPod(pod)
	allocatableGPUs := getDevicesIdleGPUMemory(gs)

	var devIDs []int

	for devID := range allocatableGPUs {
		if availableGPU, ok := allocatableGPUs[devID]; ok && availableGPU >= gpuRequest {
			devIDs = append(devIDs, devID)
		}
	}
	sort.Ints(devIDs)
	return devIDs
}

// predicateGPU returns the available GPU IDs
func predicateGPUbyNumber(pod *v1.Pod, gs *GPUDevices) []int {
	gpuRequest := getGPUNumberOfPod(pod)
	allocatableGPUs := getDevicesIdleGPUs(gs)

	if len(allocatableGPUs) < gpuRequest {
		klog.Errorf("Not enough gpu cards")
		return nil
	}

	return allocatableGPUs[:gpuRequest]
}

func escapeJSONPointer(p string) string {
	// Escaping reference name using https://tools.ietf.org/html/rfc6901
	p = strings.Replace(p, "~", "~0", -1)
	p = strings.Replace(p, "/", "~1", -1)
	return p
}

// AddGPUIndexPatch returns the patch adding GPU index
func AddGPUIndexPatch(ids []int) string {
	idsstring := strings.Trim(strings.Replace(fmt.Sprint(ids), " ", ",", -1), "[]")
	return fmt.Sprintf(`[{"op": "add", "path": "/metadata/annotations/%s", "value":"%d"},`+
		`{"op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`,
		escapeJSONPointer(PredicateTime), time.Now().UnixNano(),
		escapeJSONPointer(GPUIndex), idsstring)
}

// RemoveGPUIndexPatch returns the patch removing GPU index
func RemoveGPUIndexPatch() string {
	return fmt.Sprintf(`[{"op": "remove", "path": "/metadata/annotations/%s"},`+
		`{"op": "remove", "path": "/metadata/annotations/%s"}]`, escapeJSONPointer(PredicateTime), escapeJSONPointer(GPUIndex))
}

// getUsedGPUMemory calculates the used memory of the device.
func (g *GPUDevice) getUsedGPUMemory() uint {
	res := uint(0)
	for _, pod := range g.PodMap {
		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		} else {
			gpuRequest := getGPUMemoryOfPod(pod)
			res += gpuRequest
		}
	}
	return res
}

// isIdleGPU check if the device is idled.
func (g *GPUDevice) isIdleGPU() bool {
	return len(g.PodMap) == 0
}

// getGPUMemoryPod returns the GPU memory required by the pod.
func getGPUMemoryOfPod(pod *v1.Pod) uint {
	var initMem uint
	for _, container := range pod.Spec.InitContainers {
		res := getGPUMemoryOfContainer(container.Resources)
		if initMem < res {
			initMem = res
		}
	}

	var mem uint
	for _, container := range pod.Spec.Containers {
		mem += getGPUMemoryOfContainer(container.Resources)
	}

	if mem > initMem {
		return mem
	}
	return initMem
}

// getGPUMemoryOfContainer returns the GPU memory required by the container.
func getGPUMemoryOfContainer(resources v1.ResourceRequirements) uint {
	var mem uint
	if val, ok := resources.Limits[VolcanoGPUResource]; ok {
		mem = uint(val.Value())
	}
	return mem
}

// getGPUNumberOfPod returns the number of GPUs required by the pod.
func getGPUNumberOfPod(pod *v1.Pod) int {
	var gpus int
	for _, container := range pod.Spec.Containers {
		gpus += getGPUNumberOfContainer(container.Resources)
	}

	var initGPUs int
	for _, container := range pod.Spec.InitContainers {
		res := getGPUNumberOfContainer(container.Resources)
		if initGPUs < res {
			initGPUs = res
		}
	}

	if gpus > initGPUs {
		return gpus
	}
	return initGPUs
}

// getGPUNumberOfContainer returns the number of GPUs required by the container.
func getGPUNumberOfContainer(resources v1.ResourceRequirements) int {
	var gpus int
	if val, ok := resources.Limits[VolcanoGPUNumber]; ok {
		gpus = int(val.Value())
	}
	return gpus
}
