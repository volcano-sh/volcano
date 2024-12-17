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

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestParseDeviceConfig(t *testing.T) {
	testCases := []struct {
		name            string
		deviceConfigStr string
	}{
		{
			name: "construct config from file string",
			deviceConfigStr: `nvidia:
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
      - models: [ "A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB" ]
        allowedGeometries:
          - group: "group1" 
            geometries: 
            - name: 1g.5gb
              memory: 5120
              count: 7
          - group: "group2"
            geometries: 
            - name: 2g.10gb
              memory: 10240
              count: 3
            - name: 1g.5gb
              memory: 5120
              count: 1
          - group: "group3"
            geometries: 
            - name: 3g.20gb
              memory: 20480
              count: 2
          - group: "group4"
            geometries: 
            - name: 7g.40gb
              memory: 40960
              count: 1
      - models: [ "A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"]
        allowedGeometries:
          - group: "group1" 
            geometries: 
            - name: 1g.10gb
              memory: 10240
              count: 7
          - group: "group2"
            geometries: 
            - name: 2g.20gb
              memory: 20480
              count: 3
            - name: 1g.10gb
              memory: 10240
              count: 1
          - group: "group3"
            geometries: 
            - name: 3g.40gb
              memory: 40960
              count: 2
          - group: "group4"
            geometries: 
            - name: 7g.79gb
              memory: 80896
              count: 1`,
		},
	}

	expected := Config{
		NvidiaConfig: NvidiaConfig{
			ResourceCountName:            "volcano.sh/vgpu-number",
			ResourceMemoryName:           "volcano.sh/vgpu-memory",
			ResourceMemoryPercentageName: "volcano.sh/vgpu-memory-percentage",
			ResourceCoreName:             "volcano.sh/vgpu-cores",
			OverwriteEnv:                 false,
			DefaultMemory:                0,
			DefaultCores:                 0,
			DefaultGPUNum:                1,
			DeviceSplitCount:             10,
			DeviceMemoryScaling:          1,
			DeviceCoreScaling:            1,
			GPUMemoryFactor:              1,
			MigGeometriesList: []AllowedMigGeometries{
				{
					Models: []string{"A30"},
					Geometries: []Geometry{
						{
							Group: "group1",
							Instances: []MigTemplate{
								{
									Name:   "1g.6gb",
									Memory: 6144,
									Count:  4,
								},
							},
						},
						{
							Group: "group2",
							Instances: []MigTemplate{
								{
									Name:   "2g.12gb",
									Memory: 12288,
									Count:  2,
								},
							},
						},
						{
							Group: "group3",
							Instances: []MigTemplate{
								{
									Name:   "4g.24gb",
									Memory: 24576,
									Count:  1,
								},
							},
						},
					},
				},
				{
					Models: []string{"A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB"},
					Geometries: []Geometry{
						{
							Group: "group1",
							Instances: []MigTemplate{
								{
									Name:   "1g.5gb",
									Memory: 5120,
									Count:  7,
								},
							},
						},
						{
							Group: "group2",
							Instances: []MigTemplate{
								{
									Name:   "2g.10gb",
									Memory: 10240,
									Count:  3,
								},
								{
									Name:   "1g.5gb",
									Memory: 5120,
									Count:  1,
								},
							},
						},
						{
							Group: "group3",
							Instances: []MigTemplate{
								{
									Name:   "3g.20gb",
									Memory: 20480,
									Count:  2,
								},
							},
						},
						{
							Group: "group4",
							Instances: []MigTemplate{
								{
									Name:   "7g.40gb",
									Memory: 40960,
									Count:  1,
								},
							},
						},
					},
				},
				{
					Models: []string{"A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"},
					Geometries: []Geometry{
						{
							Group: "group1",
							Instances: []MigTemplate{
								{
									Name:   "1g.10gb",
									Memory: 10240,
									Count:  7,
								},
							},
						},
						{
							Group: "group2",
							Instances: []MigTemplate{
								{
									Name:   "2g.20gb",
									Memory: 20480,
									Count:  3,
								},
								{
									Name:   "1g.10gb",
									Memory: 10240,
									Count:  1,
								},
							},
						},
						{
							Group: "group3",
							Instances: []MigTemplate{
								{
									Name:   "3g.40gb",
									Memory: 40960,
									Count:  2,
								},
							},
						},
						{
							Group: "group4",
							Instances: []MigTemplate{
								{
									Name:   "7g.79gb",
									Memory: 80896,
									Count:  1,
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var yamlData Config
			err := yaml.Unmarshal([]byte(tc.deviceConfigStr), &yamlData)
			assert.Nil(t, err)
			assert.True(t, reflect.DeepEqual(yamlData, expected))
		})
	}
}
