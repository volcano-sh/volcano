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
	"context"
	"fmt"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
	"volcano.sh/volcano/third_party/hami/util"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
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

func Test_verifyReq(t *testing.T) {
	conf, err := yamlStringToConfig(config_yaml)
	assert.Nil(t, err)
	dev := AscendDevice{
		config: conf.VNPUs[len(conf.VNPUs)-1],
	}
	tests := []struct {
		name string
		req  devices.ContainerDeviceRequest
		pass bool
	}{
		{
			"single_device_with_partial_memory_should_pass",
			devices.ContainerDeviceRequest{
				Nums:             1,
				Type:             "Ascend910B2C",
				Memreq:           1024,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
			true,
		},
		{
			"multiple_devices_with_partial_memory_should_fail",
			devices.ContainerDeviceRequest{
				Nums:             2,
				Type:             "Ascend910B2C",
				Memreq:           1024,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
			false,
		},
		{
			"multiple_devices_with_full_memory_should_pass",
			devices.ContainerDeviceRequest{
				Nums:             2,
				Type:             "Ascend910B2C",
				Memreq:           21527,
				MemPercentagereq: 100,
				Coresreq:         0,
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyReq(tt.req, &dev)
			if tt.pass {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestAscendDevices_AddResource(t *testing.T) {
	tests := []struct {
		name          string
		ascendDevices *AscendDevices
		pod           *v1.Pod
		expectedUsage map[string]*devices.DeviceUsage // device ID -> expected DeviceUsage
	}{
		{
			name: "Successfully add single device resource",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "device-1,Ascend910,16000,50",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      1,
					Usedmem:   16000,
					Usedcores: 50,
				},
			},
		},
		{
			name: "Successfully add multiple device resources",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
				"device-2": createTestAscendDevice("device-2", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "device-1,Ascend910,8000,25:device-2,Ascend910,8000,25",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      1,
					Usedmem:   8000,
					Usedcores: 25,
				},
				"device-2": {
					Used:      1,
					Usedmem:   8000,
					Usedcores: 25,
				},
			},
		},
		{
			name: "Add resource to existing device usage - accumulate",
			ascendDevices: func() *AscendDevices {
				ads := createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
					"device-1": createTestAscendDevice("device-1", 100, 32000, 1, 8000, 25, map[string]*devices.DeviceUsage{
						"existing-pod": {Used: 1, Usedmem: 8000, Usedcores: 25},
					}),
				})
				return ads
			}(),
			pod: createTestPod("new-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "device-1,Ascend910,8000,25",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      2,     // Should increase from 1 to 2
					Usedmem:   16000, // Should increase from 8000 to 16000
					Usedcores: 50,    // Should increase from 25 to 50
				},
			},
		},
		{
			name: "Add same pod multiple times - should not duplicate",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "device-1,Ascend910,16000,50",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      1,
					Usedmem:   16000,
					Usedcores: 50,
				},
			},
		},
		{
			name: "Pod has invalid annotation format",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "invalid format",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      0,
					Usedmem:   0,
					Usedcores: 0,
				},
			},
		},
		{
			name: "Pod missing required annotation",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      0,
					Usedmem:   0,
					Usedcores: 0,
				},
			},
		},
		{
			name: "Device not found in devices map",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "non-existent-device,Ascend910,16000,50",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      0,
					Usedmem:   0,
					Usedcores: 0,
				},
			},
		},
		{
			name: "Multiple containers with different devices",
			ascendDevices: createTestAscendDevices("node1", "Ascend910", map[string]*AscendDevice{
				"device-1": createTestAscendDevice("device-1", 100, 32000, 0, 0, 0, nil),
				"device-2": createTestAscendDevice("device-2", 100, 32000, 0, 0, 0, nil),
				"device-3": createTestAscendDevice("device-3", 100, 32000, 0, 0, 0, nil),
			}),
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: "device-1,Ascend910,4000,10:device-2,Ascend910,4000,10",
			}),
			expectedUsage: map[string]*devices.DeviceUsage{
				"device-1": {
					Used:      1,
					Usedmem:   4000,
					Usedcores: 10,
				},
				"device-2": {
					Used:      1,
					Usedmem:   4000,
					Usedcores: 10,
				},
				"device-3": {
					Used:      0,
					Usedmem:   0,
					Usedcores: 0,
				},
			},
		},
		{
			name:          "Nil AscendDevices receiver",
			ascendDevices: nil,
			pod: createTestPod("test-pod", "ns1", map[string]string{
				devices.SupportDevices["Ascend910"]: `[{"UUID":"device-1","Type":"Ascend910","Usedmem":16000,"Usedcores":50}]`,
			}),
			expectedUsage: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Handle nil receiver case separately
			if tt.ascendDevices == nil {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("AddResource panicked with nil receiver: %v", r)
					}
				}()
				tt.ascendDevices.AddResource(tt.pod)
				return
			}

			// For test case where adding same pod twice
			if tt.name == "Add same pod multiple times - should not duplicate" {
				// First addition
				tt.ascendDevices.AddResource(tt.pod)
				// Second addition (should not duplicate)
				tt.ascendDevices.AddResource(tt.pod)
			} else {
				tt.ascendDevices.AddResource(tt.pod)
			}

			// Verify resource usage for each device
			for deviceID, expected := range tt.expectedUsage {
				dev, exists := tt.ascendDevices.Devices[deviceID]
				if !exists && expected.Used > 0 {
					t.Errorf("Device %s not found in AscendDevices", deviceID)
					continue
				}
				if dev != nil {
					if dev.DeviceUsage.Used != expected.Used {
						t.Errorf("Device %s: Expected Used=%d, got %d", deviceID, expected.Used, dev.DeviceUsage.Used)
					}
					if dev.DeviceUsage.Usedmem != expected.Usedmem {
						t.Errorf("Device %s: Expected Usedmem=%d, got %d", deviceID, expected.Usedmem, dev.DeviceUsage.Usedmem)
					}
					if dev.DeviceUsage.Usedcores != expected.Usedcores {
						t.Errorf("Device %s: Expected Usedcores=%d, got %d", deviceID, expected.Usedcores, dev.DeviceUsage.Usedcores)
					}
				}
			}
		})
	}
}

func createTestAscendDevices(nodeName, deviceType string, devices map[string]*AscendDevice) *AscendDevices {
	return &AscendDevices{
		NodeName: nodeName,
		Type:     deviceType,
		Devices:  devices,
		Policy:   binpackPolicy,
	}
}

func createTestAscendDevice(id string, totalCores, totalMem int32, used, usedmem, usedcores int32, existingPods map[string]*devices.DeviceUsage) *AscendDevice {
	device := &AscendDevice{
		DeviceInfo: &devices.DeviceInfo{
			ID:      id,
			Devcore: totalCores,
			Devmem:  totalMem,
			Count:   1,
		},
		DeviceUsage: &devices.DeviceUsage{
			Used:      used,
			Usedmem:   usedmem,
			Usedcores: usedcores,
		},
		PodMap: make(map[string]*devices.DeviceUsage),
		config: config.VNPUConfig{
			CommonWord:         "Ascend910",
			ResourceName:       "huawei.com/Ascend910",
			ResourceMemoryName: "huawei.com/Ascend910Memory",
			MemoryCapacity:     32000,
			MemoryAllocatable:  32000,
		},
	}

	// Add existing pods to PodMap
	for podUID, usage := range existingPods {
		device.PodMap[podUID] = &devices.DeviceUsage{
			Used:      usage.Used,
			Usedmem:   usage.Usedmem,
			Usedcores: usage.Usedcores,
		}
	}

	return device
}

func createTestPod(name, namespace string, annotations map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         getTestUID(name),
			Annotations: annotations,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "test-container",
				},
			},
		},
	}
}

func getTestUID(podName string) types.UID {
	return types.UID(podName + "-uid-1234")
}

const (
	testInRequestKey = "hami.io/Ascend910A-devices-to-allocate"
	testSupportKey   = "hami.io/Ascend910A-devices-allocated"
	testHuaweiKey    = "huawei.com/Ascend910A"
)

// setupReleaseTestKeys populates the global device key maps needed by Release and
// returns a cleanup function to restore them.  This avoids leaking state across
// tests via init().
func setupReleaseTestKeys() func() {
	prevInReq, prevSup := devices.InRequestDevices["Ascend910A"], devices.SupportDevices["Ascend910A"]
	devices.InRequestDevices["Ascend910A"] = testInRequestKey
	devices.SupportDevices["Ascend910A"] = testSupportKey
	return func() {
		if prevInReq == "" {
			delete(devices.InRequestDevices, "Ascend910A")
		} else {
			devices.InRequestDevices["Ascend910A"] = prevInReq
		}
		if prevSup == "" {
			delete(devices.SupportDevices, "Ascend910A")
		} else {
			devices.SupportDevices["Ascend910A"] = prevSup
		}
	}
}

func hamiPodWithAnnotations(phase string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "worker-0",
			Namespace: "default",
			Annotations: map[string]string{
				util.AssignedNodeAnnotations: "node-a",
				util.AssignedTimeAnnotations: "1700000000",
				util.DeviceBindPhase:         phase,
				util.BindTimeAnnotations:     "1700000000",
				"predicate-time":             "1700000000",
				testInRequestKey:             "GPU-aaaa,Ascend910A,4096,0",
				testSupportKey:               "GPU-aaaa,Ascend910A,4096,0",
				testHuaweiKey:                `[{"UUID":"GPU-aaaa","Temp":"vir04"}]`,
				"keep-me":                    "yes",
			},
		},
	}
}

func TestHAMiReleaseCleansSpeculativeAnnotations(t *testing.T) {
	cleanup := setupReleaseTestKeys()
	defer cleanup()

	pod := hamiPodWithAnnotations("allocating")
	client := fake.NewSimpleClientset(pod)
	ads := &AscendDevices{NodeName: "node-a"}

	if err := ads.Release(client, pod); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}

	for _, k := range []string{
		util.AssignedNodeAnnotations,
		util.DeviceBindPhase,
		"predicate-time",
		testInRequestKey,
		testSupportKey,
		testHuaweiKey,
	} {
		if _, ok := pod.Annotations[k]; ok {
			t.Errorf("in-memory annotation %s should have been removed", k)
		}
	}
	if pod.Annotations["keep-me"] != "yes" {
		t.Errorf("non-device annotation must be preserved in-memory")
	}

	got, err := client.CoreV1().Pods("default").Get(context.Background(), "worker-0", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pod: %v", err)
	}
	if _, ok := got.Annotations[util.AssignedNodeAnnotations]; ok {
		t.Errorf("apiserver annotation %s should have been removed", util.AssignedNodeAnnotations)
	}
	if got.Annotations["keep-me"] != "yes" {
		t.Errorf("non-device annotation must be preserved on apiserver")
	}
}

func TestHAMiReleaseKeepsCommittedAnnotations(t *testing.T) {
	cleanup := setupReleaseTestKeys()
	defer cleanup()

	pod := hamiPodWithAnnotations("success")
	client := fake.NewSimpleClientset(pod)
	ads := &AscendDevices{NodeName: "node-a"}

	if err := ads.Release(client, pod); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if pod.Annotations[util.AssignedNodeAnnotations] != "node-a" {
		t.Errorf("committed pod's vgpu-node must be preserved")
	}
	if pod.Annotations[util.DeviceBindPhase] != "success" {
		t.Errorf("committed pod's bind-phase must be preserved")
	}
}
