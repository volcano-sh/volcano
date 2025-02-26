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

package vgpu

var VGPUEnable bool
var NodeLockEnable bool

const (
	// DeviceName used to indicate this device
	DeviceName = "hamivgpu"

	GPUInUse                         = "nvidia.com/use-gputype"
	GPUNoUse                         = "nvidia.com/nouse-gputype"
	AssignedTimeAnnotations          = "volcano.sh/vgpu-time"
	AssignedIDsAnnotations           = "volcano.sh/vgpu-ids-new"
	AssignedIDsToAllocateAnnotations = "volcano.sh/devices-to-allocate"
	AssignedNodeAnnotations          = "volcano.sh/vgpu-node"
	BindTimeAnnotations              = "volcano.sh/bind-time"
	DeviceBindPhase                  = "volcano.sh/bind-phase"

	NvidiaGPUDevice = "NVIDIA"

	// VolcanoVGPUMemory extended gpu memory
	VolcanoVGPUMemory = "volcano.sh/vgpu-memory"
	// VolcanoVGPUMemoryPercentage extends gpu memory
	VolcanoVGPUMemoryPercentage = "volcano.sh/vgpu-memory-percentage"
	// VolcanoVGPUCores indicates utilization percentage of vgpu
	VolcanoVGPUCores = "volcano.sh/vgpu-cores"
	// VolcanoVGPUNumber virtual GPU card number
	VolcanoVGPUNumber = "volcano.sh/vgpu-number"
	// VolcanoVGPURegister virtual gpu information registered from device-plugin to scheduler
	VolcanoVGPURegister = "volcano.sh/node-vgpu-register"
	// VolcanoVGPUHandshake for vgpu
	VolcanoVGPUHandshake = "volcano.sh/node-vgpu-handshake"

	// PredicateTime is the key of predicate time
	PredicateTime = "volcano.sh/predicate-time"
	// GPUIndex is the key of gpu index
	GPUIndex = "volcano.sh/gpu-index"

	// UnhealthyGPUIDs list of unhealthy gpu ids
	UnhealthyGPUIDs = "volcano.sh/gpu-unhealthy-ids"

	// binpack means the lower device memory remained after this allocation, the better
	binpackPolicy = "binpack"
	// spread means better put this task into an idle GPU card than a shared GPU card
	spreadPolicy = "spread"
	// 101 means wo don't assign defaultMemPercentage value

	DefaultMemPercentage = 101
	binpackMultiplier    = 100
	spreadMultiplier     = 100
)

type ContainerDeviceRequest struct {
	Nums             int32
	Type             string
	Memreq           int32
	MemPercentagereq int32
	Coresreq         int32
}

type ContainerDevice struct {
	UUID      string
	Type      string
	Usedmem   int32
	Usedcores int32
}

type ContainerDevices []ContainerDevice
