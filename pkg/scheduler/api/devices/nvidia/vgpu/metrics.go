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
	// VolcanoNamespace - namespace in prometheus used by volcano
	VolcanoNamespace = "volcano"

	// OnSessionOpen label
	OnSessionOpen = "OnSessionOpen"

	// OnSessionClose label
	OnSessionClose = "OnSessionClose"
)

var (
	VGPUDevicesSharedNumber = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "vgpu_device_shared_number",
			Help:      "The number of vgpu tasks sharing this card",
		},
		[]string{"devID"},
	)
	VGPUDevicesSharedMemory = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "vgpu_device_allocated_memory",
			Help:      "The number of vgpu memory allocated in this card",
		},
		[]string{"devID"},
	)
	VGPUDevicesSharedCores = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "vgpu_device_allocated_cores",
			Help:      "The percentage of gpu compute cores allocated in this card",
		},
		[]string{"devID"},
	)
	VGPUDevicesMemoryLimit = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "vgpu_device_memory_limit",
			Help:      "The number of total device memory allocated in this card",
		},
		[]string{"devID"},
	)
)

func (gs *GPUDevices) GetStatus() string {
	for _, val := range gs.Device {
		VGPUDevicesSharedNumber.WithLabelValues(val.UUID).Set(float64(val.UsedNum))
		VGPUDevicesSharedMemory.WithLabelValues(val.UUID).Set(float64(val.UsedMem))
		VGPUDevicesMemoryLimit.WithLabelValues(val.UUID).Set(float64(val.Memory))
		VGPUDevicesSharedCores.WithLabelValues(val.UUID).Set(float64(val.UsedCore))
	}
	return ""
}
