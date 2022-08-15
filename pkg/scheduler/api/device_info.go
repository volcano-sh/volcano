/*
Copyright 2020 The Volcano Authors.

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
	v1 "k8s.io/api/core/v1"
)

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU ID
	ID int
	// The pods that are sharing this GPU
	PodMap map[string]*v1.Pod
	// memory per card
	Memory uint
}

// NewGPUDevice creates a device
func NewGPUDevice(id int, mem uint) *GPUDevice {
	return &GPUDevice{
		ID:     id,
		Memory: mem,
		PodMap: map[string]*v1.Pod{},
	}
}

// getUsedGPUMemory calculates the used memory of the device.
func (g *GPUDevice) getUsedGPUMemory() uint {
	res := uint(0)
	for _, pod := range g.PodMap {
		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		} else {
			gpuRequest := GetGPUMemoryOfPod(pod)
			res += gpuRequest
		}
	}
	return res
}

// isIdleGPU check if the device is idled.
func (g *GPUDevice) isIdleGPU() bool {
	return g.PodMap == nil || len(g.PodMap) == 0
}

// GetGPUMemoryPod returns the GPU memory required by the pod.
func GetGPUMemoryOfPod(pod *v1.Pod) uint {
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

// GetGPUNumberOfPod returns the number of GPUs required by the pod.
func GetGPUNumberOfPod(pod *v1.Pod) int {
	var gpus int
	for _, container := range pod.Spec.Containers {
		gpus += getGPUNumberOfContainer(container.Resources)
	}
	return gpus
}

// getGPUNumberOfContainer returns the number of GPUs required by the container.
func getGPUNumberOfContainer(resources v1.ResourceRequirements) int {
	var gpus int
	if val, ok := resources.Limits[VolcanoGPUNumber]; ok {
		gpus = int(val.Value())
	}
	return gpus
}
