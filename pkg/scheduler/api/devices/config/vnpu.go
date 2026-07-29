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

import "fmt"

type VNPUsConfig struct {
	HamiVnpuCore bool         `yaml:"hamiVnpuCore,omitempty"`
	Configs      []VNPUConfig `yaml:"configs"`
}

func (v *VNPUsConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw map[string]interface{}
	if err := unmarshal(&raw); err == nil {
		type vnpusConfigAlias VNPUsConfig // alias prevents infinite recursion
		var wrapper vnpusConfigAlias
		if err := unmarshal(&wrapper); err != nil {
			return fmt.Errorf("vnpus: failed to parse wrapper struct: %w", err)
		}
		*v = VNPUsConfig(wrapper)
		return nil
	}

	// Fallback: YAML node is an array — parse as legacy direct array format.
	var configs []VNPUConfig
	if err := unmarshal(&configs); err != nil {
		return fmt.Errorf("vnpus: failed to parse as either wrapper struct or direct array: %w", err)
	}
	v.Configs = configs
	v.HamiVnpuCore = false
	return nil
}

type Template struct {
	Name   string `yaml:"name"`
	Memory int64  `yaml:"memory"`
	AICore int32  `yaml:"aiCore,omitempty"`
	AICPU  int32  `yaml:"aiCPU,omitempty"`
}

type VNPUConfig struct {
	CommonWord         string     `yaml:"commonWord"`
	ChipName           string     `yaml:"chipName"`
	ResourceName       string     `yaml:"resourceName"`
	ResourceMemoryName string     `yaml:"resourceMemoryName"`
	MemoryAllocatable  int64      `yaml:"memoryAllocatable"`
	MemoryCapacity     int64      `yaml:"memoryCapacity"`
	AICore             int32      `yaml:"aiCore"`
	AICPU              int32      `yaml:"aiCPU"`
	Templates          []Template `yaml:"templates"`
}
