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
	"errors"

	v1 "k8s.io/api/core/v1"
)

// GPUDevice include gpu id, memory and the pods that are sharing it.
type GPUDevice struct {
	// GPU ID
	ID int
	// UUID of this GPU device
	UUID string
	// The pods that are sharing this GPU
	PodMap map[string]*v1.Pod
	// memory per card
	Memory uint
	// How many containers are sharing this GPU
	Count int32
	// Healthy status of this device
	Health bool
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
			gpuRequest := GetGPUResourceOfPod(pod)
			res += gpuRequest
		}
	}
	return res
}

// GetGPUResourceOfPod returns the GPU resource required by the pod.
func GetGPUResourceOfPod(pod *v1.Pod) uint {
	var mem uint
	for _, container := range pod.Spec.Containers {
		mem += getGPUResourceOfContainer(&container)
	}
	return mem
}

// getGPUResourceOfPod returns the GPU resource required by the container.
func getGPUResourceOfContainer(container *v1.Container) uint {
	var mem uint
	if val, ok := container.Resources.Limits[VolcanoGPUResource]; ok {
		mem = uint(val.Value())
	}
	return mem
}

// GPUDevice include gpu id, memory and the pods that are sharing it.
type VGPUDevice struct {
	// GPU ID
	ID int
	// UUID of this GPU device
	UUID string
	// The pods that are sharing this GPU
	PodMap map[string]*v1.Pod
	// The containers that are sharing this vGPU
	ContainerMap map[string]*v1.Container
	// memory per card
	Memory uint
	// How many containers are sharing this GPU
	Count int32
	// Healthy status of this device
	Health bool
}

// getUsedGPUMemory calculates the used memory of the device.
func (g *VGPUDevice) getUsedvGPUMemory() uint {
	res := uint(0)
	for _, pod := range g.PodMap {
		if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
			continue
		} else {
			for _, ctr := range g.ContainerMap {
				gpuRequest := GetvGPUResourceOfContainer(ctr)
				res += gpuRequest
			}
		}
	}
	return res
}

// GetGPUResourceOfPod returns the GPU resource required by the pod.
func GetvGPUResourceOfPod(pod *v1.Pod) uint {
	var mem uint
	for _, container := range pod.Spec.Containers {
		mem += GetvGPUResourceOfContainer(&container)
	}
	return mem
}

// GetGPUResourceOfPod returns the GPU resource required by the container.
func GetvGPUResourceOfContainer(container *v1.Container) uint {
	var mem uint
	if val, ok := container.Resources.Limits[VolcanovGPUResource]; ok {
		mem = uint(val.Value())
	}
	return mem
}

// GetvGPUDevices returns the VGPUDevice array according to id
func GetvGPUDevices(ni NodeInfo, ids []int) ([]VGPUDevice, error) {
	res := []VGPUDevice{}
	for _, val := range ids {
		found := false
		for _, v := range ni.VGPUDevices {
			if val == v.ID {
				res = append(res, v)
				found = true
			}
		}
		if !found {
			return []VGPUDevice{}, errors.New("vgpu idx not found")
		}
	}
	return res, nil
}

// GetvGPUDevice returns the VGPUDevice according to id
func GetvGPUDevice(ni NodeInfo, id int) (VGPUDevice, error) {
	for _, v := range ni.VGPUDevices {
		if id == v.ID {
			return v, nil
		}
	}
	return VGPUDevice{}, errors.New("vgpu idx not found")
}
