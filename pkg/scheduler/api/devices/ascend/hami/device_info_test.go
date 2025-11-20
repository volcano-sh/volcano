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

package hami

import (
	"fmt"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

var config_yaml = `
vnpus:
- chipName: 910A
  commonWord: Ascend910A
  resourceName: huawei.com/Ascend910A
  resourceMemoryName: huawei.com/Ascend910A-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 30
  templates:
    - name: vir02
      memory: 2184
      aiCore: 2
    - name: vir04
      memory: 4369
      aiCore: 4
    - name: vir08
      memory: 8738
      aiCore: 8
    - name: vir16
      memory: 17476
      aiCore: 16
- chipName: 910B2
  commonWord: Ascend910B2
  resourceName: huawei.com/Ascend910B2
  resourceMemoryName: huawei.com/Ascend910B2-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 24
  aiCPU: 6
  templates:
    - name: vir03_1c_8g
      memory: 8192
      aiCore: 3
      aiCPU: 1
    - name: vir06_1c_16g
      memory: 16384
      aiCore: 6
      aiCPU: 1
    - name: vir12_3c_32g
      memory: 32768
      aiCore: 12
      aiCPU: 3
- chipName: 910B3
  commonWord: Ascend910B3
  resourceName: huawei.com/Ascend910B3
  resourceMemoryName: huawei.com/Ascend910B3-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_16g
      memory: 16384
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_32g
      memory: 32768
      aiCore: 10
      aiCPU: 3
- chipName: 910B4-1
  commonWord: Ascend910B4-1
  resourceName: huawei.com/Ascend910B4-1
  resourceMemoryName: huawei.com/Ascend910B4-1-memory
  memoryAllocatable: 65536
  memoryCapacity: 65536
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_8g
      memory: 8192
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_16g
      memory: 16384
      aiCore: 10
      aiCPU: 3
- chipName: 910B4
  commonWord: Ascend910B4
  resourceName: huawei.com/Ascend910B4
  resourceMemoryName: huawei.com/Ascend910B4-memory
  memoryAllocatable: 32768
  memoryCapacity: 32768
  aiCore: 20
  aiCPU: 7
  templates:
    - name: vir05_1c_8g
      memory: 8192
      aiCore: 5
      aiCPU: 1
    - name: vir10_3c_16g
      memory: 16384
      aiCore: 10
      aiCPU: 3
- chipName: 310P3
  commonWord: Ascend310P
  resourceName: huawei.com/Ascend310P
  resourceMemoryName: huawei.com/Ascend310P-memory
  memoryAllocatable: 21527
  memoryCapacity: 24576
  aiCore: 8
  aiCPU: 7
  templates:
    - name: vir01
      memory: 3072
      aiCore: 1
      aiCPU: 1
    - name: vir02
      memory: 6144
      aiCore: 2
      aiCPU: 2
    - name: vir04
      memory: 12288
      aiCore: 4
      aiCPU: 4
nvidia:
  resourceCountName: volcano.sh/vgpu-number
  resourceMemoryName: volcano.sh/vgpu-memory
  resourceMemoryPercentageName: volcano.sh/vgpu-memory-percentage
  resourceCoreName: volcano.sh/vgpu-cores
  overwriteEnv: false
  defaultMemory: 0
  defaultCores: 0
  defaultGPUNum: 1
  deviceSplitCount: 10
  deviceMemoryScaling: 1
  deviceCoreScaling: 1
  gpuMemoryFactor: 1
  knownMigGeometries:
  - models: [ "A30" ]
    allowedGeometries:
      - group: group1
        geometries: 
        - name: 1g.6gb
          memory: 6144
          count: 4
      - group: group2
        geometries: 
        - name: 2g.12gb
          memory: 12288
          count: 2
      - group: group3
        geometries: 
        - name: 4g.24gb
          memory: 24576
          count: 1
`

func yamlStringToConfig(yamlStr string) (*config.Config, error) {
	var config config.Config
	err := yaml.Unmarshal([]byte(yamlStr), &config)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal YAML: %v", err)
	}
	return &config, nil
}

func Test_trimMemory(t *testing.T) {
	conf, err := yamlStringToConfig(config_yaml)
	assert.Nil(t, err)
	dev := AscendDevice{
		config: conf.VNPUs[len(conf.VNPUs)-1],
	}
	tests := []struct {
		name     string
		inputMem int64
		wantMem  int64
	}{
		{"test1", 0, 3072},
		{"test2", 1, 3072},
		{"test3", 100, 3072},
		{"test4", 3071, 3072},
		{"test5", 3072, 3072},
		{"test6", 3073, 6144},
		{"test7", 6143, 6144},
		{"test8", 6144, 6144},
		{"test9", 6144, 6144},
		{"test10", 6145, 12288},
		{"test11", 12288, 12288},
		{"test12", 12289, 21527},
		{"test13", 21527, 21527},
		{"test14", 21528, 21527},
		{"test15", 24576, 21527},
		{"test16", 24577, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := dev.trimMemory(tt.inputMem)
			assert.Equal(t, tt.wantMem, got)
		})
	}
}

func Test_fit(t *testing.T) {
	conf, err := yamlStringToConfig(config_yaml)
	assert.Nil(t, err)
	ascend310PConfig := conf.VNPUs[len(conf.VNPUs)-1]
	device_info := &devices.DeviceInfo{
		ID:      "68496E64-20E05477-92C31323-6E78030A-BD003019",
		Index:   0,
		Count:   7,
		Devcore: 8,
		Devmem:  21527,
	}
	tests := []struct {
		name   string
		req    *devices.ContainerDeviceRequest
		dev    *AscendDevice
		result bool
	}{
		{
			"test1",
			&devices.ContainerDeviceRequest{
				Nums:   1,
				Type:   "Ascend310P",
				Memreq: 1024,
			},
			&AscendDevice{
				config:     ascend310PConfig,
				DeviceInfo: device_info,
				DeviceUsage: &devices.DeviceUsage{
					Used:    1,
					Usedmem: 3072,
				},
			},
			true,
		},
		{
			"test2",
			&devices.ContainerDeviceRequest{
				Nums:   1,
				Type:   "Ascend310P",
				Memreq: 21527,
			},
			&AscendDevice{
				config:     ascend310PConfig,
				DeviceInfo: device_info,
				DeviceUsage: &devices.DeviceUsage{
					Used:    1,
					Usedmem: 3072,
				},
			},
			false,
		},
		{
			"test3",
			&devices.ContainerDeviceRequest{
				Nums:   1,
				Type:   "Ascend310P",
				Memreq: 6144,
			},
			&AscendDevice{
				config:     ascend310PConfig,
				DeviceInfo: device_info,
				DeviceUsage: &devices.DeviceUsage{
					Used:    1,
					Usedmem: 12288,
				},
			},
			true,
		},
		{
			"test4",
			&devices.ContainerDeviceRequest{
				Nums:   1,
				Type:   "Ascend310P",
				Memreq: 24576,
			},
			&AscendDevice{
				config:     ascend310PConfig,
				DeviceInfo: device_info,
				DeviceUsage: &devices.DeviceUsage{
					Used:    0,
					Usedmem: 0,
				},
			},
			false,
		},
		{
			"test5_core",
			&devices.ContainerDeviceRequest{
				Nums:     1,
				Type:     "Ascend310P",
				Memreq:   6144,
				Coresreq: 4,
			},
			&AscendDevice{
				config:     ascend310PConfig,
				DeviceInfo: device_info,
				DeviceUsage: &devices.DeviceUsage{
					Used:      1,
					Usedmem:   12288,
					Usedcores: 6,
				},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ret := fit(tt.req, tt.dev)
			assert.Equal(t, tt.result, ret)
		})
	}
}
