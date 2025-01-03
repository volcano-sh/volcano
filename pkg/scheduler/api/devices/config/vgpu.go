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

type MigTemplate struct {
	Name   string `yaml:"name"`
	Memory int32  `yaml:"memory"`
	Count  int32  `yaml:"count"`
}

type MigTemplateUsage struct {
	Name   string `json:"name,omitempty"`
	Memory int32  `json:"memory,omitempty"`
	InUse  bool   `json:"inuse,omitempty"`
}

type Geometry []MigTemplate

type MIGS []MigTemplateUsage

type MigInUse struct {
	Index     int32
	UsageList MIGS
}

type AllowedMigGeometries struct {
	Models     []string   `yaml:"models"`
	Geometries []Geometry `yaml:"allowedGeometries"`
}

type NvidiaConfig struct {
	ResourceCountName            string                 `yaml:"resourceCountName"`
	ResourceMemoryName           string                 `yaml:"resourceMemoryName"`
	ResourceCoreName             string                 `yaml:"resourceCoreName"`
	ResourceMemoryPercentageName string                 `yaml:"resourceMemoryPercentageName"`
	ResourcePriority             string                 `yaml:"resourcePriorityName"`
	OverwriteEnv                 bool                   `yaml:"overwriteEnv"`
	DefaultMemory                int32                  `yaml:"defaultMemory"`
	DefaultCores                 int32                  `yaml:"defaultCores"`
	DefaultGPUNum                int32                  `yaml:"defaultGPUNum"`
	DeviceSplitCount             uint                   `yaml:"deviceSplitCount"`
	DeviceMemoryScaling          float64                `yaml:"deviceMemoryScaling"`
	DeviceCoreScaling            float64                `yaml:"deviceCoreScaling"`
	DisableCoreLimit             bool                   `yaml:"disableCoreLimit"`
	MigGeometriesList            []AllowedMigGeometries `yaml:"knownMigGeometries"`
	GPUMemoryFactor              uint                   `yaml:"gpuMemoryFactor"`
}
