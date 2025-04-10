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

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
)

const (
	// VolcanoSubSystemName - subsystem name in prometheus used by volcano
	VolcanoSubSystemName = "volcano"

	// OnSessionOpen label
	OnSessionOpen = "OnSessionOpen"

	// OnSessionClose label
	OnSessionClose = "OnSessionClose"
)

var (
	VGPUDevicesSharedNumber = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_shared_number",
			Help:      "The number of vgpu tasks sharing this card",
		},
		[]string{"devID", "NodeName"},
	)
	VGPUDevicesAllocatedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_allocated_memory",
			Help:      "The number of vgpu memory allocated in this card",
		},
		[]string{"devID", "NodeName"},
	)
	VGPUDevicesAllocatedCores = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_allocated_cores",
			Help:      "The percentage of gpu compute cores allocated in this card",
		},
		[]string{"devID", "NodeName"},
	)
	VGPUDevicesMemoryTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_memory_limit",
			Help:      "The number of total device memory in this card",
		},
		[]string{"devID", "NodeName"},
	)
	VGPUPodMemoryAllocated = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_memory_allocation_for_a_certain_pod",
			Help:      "The vgpu device memory allocated for a certain pod",
		},
		[]string{"devID", "NodeName", "podName"},
	)
	VGPUPodCoreAllocated = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "vgpu_device_core_allocation_for_a_certain_pod",
			Help:      "The vgpu device core allocated for a certain pod",
		},
		[]string{"devID", "NodeName", "podName"},
	)
)

func (gs *GPUDevices) GetStatus() string {
	return ""
}

func ResetDeviceMetrics(UUID string, nodeName string, memory float64) {
	VGPUDevicesMemoryTotal.WithLabelValues(UUID, nodeName).Set(memory)
	VGPUDevicesSharedNumber.WithLabelValues(UUID, nodeName).Set(0)
	VGPUDevicesAllocatedCores.WithLabelValues(UUID, nodeName).Set(0)
	VGPUDevicesAllocatedMemory.WithLabelValues(UUID, nodeName).Set(0)

	VGPUPodMemoryAllocated.DeletePartialMatch(prometheus.Labels{"devID": UUID})
	VGPUPodCoreAllocated.DeletePartialMatch(prometheus.Labels{"devID": UUID})
}
func (gs *GPUDevices) AddPodMetrics(index int, PodName string) {
	UUID := gs.Device[index].UUID
	NodeName := gs.Device[index].Node
	usage := gs.Device[index].PodMap[PodName]
	VGPUPodMemoryAllocated.WithLabelValues(UUID, NodeName, PodName).Set(float64(usage.UsedMem))
	VGPUPodCoreAllocated.WithLabelValues(UUID, NodeName, PodName).Set(float64(usage.UsedCore))
	VGPUDevicesSharedNumber.WithLabelValues(UUID, NodeName).Inc()
	VGPUDevicesAllocatedCores.WithLabelValues(UUID, NodeName).Set(float64(gs.Device[index].UsedCore))
	VGPUDevicesAllocatedMemory.WithLabelValues(UUID, NodeName).Add(float64(gs.Device[index].UsedMem))
}

func (gs *GPUDevices) SubPodMetrics(index int, PodName string) {
	UUID := gs.Device[index].UUID
	NodeName := gs.Device[index].Node
	usage := gs.Device[index].PodMap[PodName]
	VGPUPodMemoryAllocated.WithLabelValues(UUID, NodeName, PodName).Set(float64(usage.UsedMem))
	VGPUPodCoreAllocated.WithLabelValues(UUID, NodeName, PodName).Set(float64(usage.UsedCore))
	if usage.UsedMem == 0 {
		delete(gs.Device[index].PodMap, PodName)
		VGPUPodMemoryAllocated.DeleteLabelValues(UUID, NodeName, PodName)
		VGPUPodCoreAllocated.DeleteLabelValues(UUID, NodeName, PodName)
	}
	VGPUDevicesSharedNumber.WithLabelValues(UUID, NodeName).Dec()
	VGPUDevicesAllocatedCores.WithLabelValues(UUID, NodeName).Sub(float64(gs.Device[index].UsedCore))
	VGPUDevicesAllocatedMemory.WithLabelValues(UUID, NodeName).Sub(float64(gs.Device[index].UsedMem))
}
