/*
Copyright 2025 The Volcano Authors.

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

package config

const (
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
)

// MigTemplate is the template for a certain mig instance
type MigTemplate struct {
	// Name is the name for mig-instance, like '1g.10gb'
	Name string `yaml:"name"`
	// Memory is the device memory this mig-instance provides
	Memory int32 `yaml:"memory"`
	// Count is the number of corresponding mig-instances in this template
	Count int32 `yaml:"count"`
}

// MigTemplateUsage is the usage mask about certain mig instance is using or not
type MigTemplateUsage struct {
	// Name is the name for mig-instance, like '1g.10gb'
	Name string `json:"name,omitempty"`
	// Memory is the device memory this mig-instance provides
	Memory int32 `json:"memory,omitempty"`
	// InUse represents whether this mig-instance is being used
	InUse bool `json:"inuse,omitempty"`
}

// Geometry a group of mig instance, it represents a kind of mig pattern
type Geometry struct {
	// Group is the group name of Geometry
	Group string `yaml:"group"`
	// Instances is corresponding mig-instances in this geometry
	Instances []MigTemplate `yaml:"geometries"`
}

// MIGS is an array of MigUsage
type MIGS []MigTemplateUsage

// MigInUse is about maintaining the status about certain GPU and its related mig-instances
type MigInUse struct {
	// Index is the index in geometry group
	Index int32
	// UsageList is the corresponding usage list
	UsageList MIGS
}

// AllowedMigGeometries is about all kind of GPU curresponding supported and its mig patterns
type AllowedMigGeometries struct {
	// Models are the types of GPU
	Models []string `yaml:"models"`
	// Geometries are the mig-geometries of corresponding GPU
	Geometries []Geometry `yaml:"allowedGeometries"`
}

// NvidiaConfig is used for Nvidia-vgpu
type NvidiaConfig struct {
	// ResourceCountName is the name of GPU count
	ResourceCountName string `yaml:"resourceCountName"`
	// ResourceMemoryName is the name of GPU device memory
	ResourceMemoryName string `yaml:"resourceMemoryName"`
	// ResourceCoreName is the name of GPU core
	ResourceCoreName string `yaml:"resourceCoreName"`
	// ResourceMemoryPercentageName is the name of GPU device memory
	ResourceMemoryPercentageName string `yaml:"resourceMemoryPercentageName"`
	// ResourcePriority is the name of GPU priority
	ResourcePriority string `yaml:"resourcePriorityName"`
	// OverwriteEnv is whether we overwrite 'NVIDIA_VISIBLE_DEVICES' to 'none' for non-gpu tasks
	OverwriteEnv bool `yaml:"overwriteEnv"`
	// DefaultMemory is the number of device memory if not specified
	DefaultMemory int32 `yaml:"defaultMemory"`
	// DefaultCores is the number of device cores if not specified
	DefaultCores int32 `yaml:"defaultCores"`
	// DefaultGPUNum is the number of device number if not specified
	DefaultGPUNum int32 `yaml:"defaultGPUNum"`
	// DeviceSplitCount is the number of fake-devices reported by vgpu-device-plugin per GPU
	DeviceSplitCount uint `yaml:"deviceSplitCount"`
	// DeviceMemoryScaling is the device memory oversubscription factor
	DeviceMemoryScaling float64 `yaml:"deviceMemoryScaling"`
	// DeviceCoreScaling is the device core oversubscription factor
	DeviceCoreScaling float64 `yaml:"deviceCoreScaling"`
	// DisableCoreLimit is whether we disable hard resource limit inside container
	DisableCoreLimit bool `yaml:"disableCoreLimit"`
	// MigGeometriesList is the mig-template geometries
	MigGeometriesList []AllowedMigGeometries `yaml:"knownMigGeometries"`
	// GPUMemoryFactor is the multiplier to every unit of device memory
	GPUMemoryFactor uint `yaml:"gpuMemoryFactor"`
}
