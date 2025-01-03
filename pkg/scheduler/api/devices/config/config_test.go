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
      knownMigGeometries:
      - models: [ "A30" ]
        allowedGeometries:
          - 
            - name: 1g.6gb
              memory: 6144
              count: 4
          - 
            - name: 2g.12gb
              memory: 12288
              count: 2
          - 
            - name: 4g.24gb
              memory: 24576
              count: 1
      - models: [ "A100-SXM4-40GB", "A100-40GB-PCIe", "A100-PCIE-40GB", "A100-SXM4-40GB" ]
        allowedGeometries:
          - 
            - name: 1g.5gb
              memory: 5120
              count: 7
          - 
            - name: 2g.10gb
              memory: 10240
              count: 3
            - name: 1g.5gb
              memory: 5120
              count: 1
          - 
            - name: 3g.20gb
              memory: 20480
              count: 2
          - 
            - name: 7g.40gb
              memory: 40960
              count: 1
      - models: [ "A100-SXM4-80GB", "A100-80GB-PCIe", "A100-PCIE-80GB"]
        allowedGeometries:
          - 
            - name: 1g.10gb
              memory: 10240
              count: 7
          - 
            - name: 2g.20gb
              memory: 20480
              count: 3
            - name: 1g.10gb
              memory: 10240
              count: 1
          - 
            - name: 3g.40gb
              memory: 40960
              count: 2
          - 
            - name: 7g.79gb
              memory: 80896
              count: 1`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var yamlData Config
			err := yaml.Unmarshal([]byte(tc.deviceConfigStr), &yamlData)
			assert.Nil(t, err)
			assert.Equal(t, len(yamlData.NvidiaConfig.MigGeometriesList), 3)
			assert.Equal(t, yamlData.NvidiaConfig.MigGeometriesList[2].Geometries[1][1].Count, int32(1))
		})
	}
}
