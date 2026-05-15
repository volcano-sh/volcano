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

package vgpu

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

func TestFindMatch(t *testing.T) {
	testCases := []struct {
		name              string
		uuid              string
		requestMem        uint
		usage             config.MigInUse
		allowedGeometries []config.Geometry
		wantFit           bool
		wantUUID          string
		wantMem           uint
	}{
		{
			name:       "Match 1g.10gb and available",
			uuid:       "gpu-1234",
			requestMem: 10,
			usage: config.MigInUse{
				Index: 0,
				UsageList: config.MIGS{
					{
						Name:      "1g.10gb",
						Memory:    10240,
						InUse:     false,
						UsedIndex: []int{},
					},
				},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "default",
					Instances: []config.MigTemplate{
						{
							Name:   "1g.10gb",
							Memory: 10240,
							Count:  2,
						},
					},
				},
			},
			wantFit:  true,
			wantUUID: "gpu-1234[default-0]",
			wantMem:  10240,
		},
		{
			name:       "No match due to memory too high",
			uuid:       "gpu-1234",
			requestMem: 20000,
			usage: config.MigInUse{
				Index: 0,
				UsageList: config.MIGS{
					{
						Name:      "1g.10gb",
						Memory:    10,
						InUse:     false,
						UsedIndex: []int{},
					},
				},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "default",
					Instances: []config.MigTemplate{
						{
							Name:   "1g.10gb",
							Memory: 10,
							Count:  1,
						},
					},
				},
			},
			wantFit:  false,
			wantUUID: "",
			wantMem:  0,
		},
		{
			name:       "Empty geometry list",
			uuid:       "gpu-1234",
			requestMem: 10,
			usage: config.MigInUse{
				Index:     0,
				UsageList: config.MIGS{},
			},
			allowedGeometries: []config.Geometry{
				{},
			},
			wantFit:  false,
			wantUUID: "",
			wantMem:  0,
		},
		{
			name:       "No matching MIG template in geometry",
			uuid:       "gpu-0002",
			requestMem: 20,
			usage: config.MigInUse{
				Index: 0,
				UsageList: config.MIGS{
					{
						Name:      "1g.10gb",
						Memory:    10,
						InUse:     false,
						UsedIndex: []int{},
					},
				},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "default",
					Instances: []config.MigTemplate{
						{
							Name:   "2g.20gb",
							Memory: 20,
							Count:  1,
						},
					},
				},
			},
			wantFit:  false,
			wantUUID: "",
			wantMem:  0,
		},
		{
			name:       "MIG instance already in use",
			uuid:       "gpu-1234",
			requestMem: 10,
			usage: config.MigInUse{
				Index: 0,
				UsageList: config.MIGS{
					{
						Name:      "1g.10gb",
						Memory:    10,
						InUse:     true,
						UsedIndex: []int{0},
					},
				},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "default",
					Instances: []config.MigTemplate{
						{
							Name:   "1g.10gb",
							Memory: 10,
							Count:  1,
						},
					},
				},
			},
			wantFit:  false,
			wantUUID: "",
			wantMem:  0,
		},
		{
			name:       "Multiple templates, pick first matching and available",
			uuid:       "gpu-1234",
			requestMem: 20,
			usage: config.MigInUse{
				Index:     -1,
				UsageList: config.MIGS{},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{
							Name:   "1g.10gb",
							Memory: 10,
							Count:  1,
						},
					},
				},
				{
					Group: "group2",
					Instances: []config.MigTemplate{
						{
							Name:   "2g.20gb",
							Memory: 24,
							Count:  1,
						},
					},
				},
			},
			wantFit:  true,
			wantUUID: "gpu-1234[group2-0]",
			wantMem:  24,
		},
		{
			name:       "Request memory smaller than template, still match",
			uuid:       "gpu-0005",
			requestMem: 5,
			usage: config.MigInUse{
				Index: 0,
				UsageList: config.MIGS{
					{
						Name:      "1g.10gb",
						Memory:    10,
						InUse:     false,
						UsedIndex: []int{},
					},
				},
			},
			allowedGeometries: []config.Geometry{
				{
					Group: "default",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10, Count: 1},
					},
				},
			},
			wantFit:  true,
			wantUUID: "gpu-0005[default-0]",
			wantMem:  10,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotFit, gotUUID, gotMem := findMatch(tc.uuid, tc.requestMem, tc.usage, tc.allowedGeometries)

			if gotFit != tc.wantFit {
				t.Errorf("findMatch() gotFit = %v, want %v", gotFit, tc.wantFit)
			}
			if gotUUID != tc.wantUUID {
				t.Errorf("findMatch() gotUUID = %v, want %v", gotUUID, tc.wantUUID)
			}
			if gotMem != tc.wantMem {
				t.Errorf("findMatch() gotMem = %v, want %v", gotMem, tc.wantMem)
			}
		})
	}
}

func TestAddAndSubMigUsed(t *testing.T) {
	cases := []struct {
		name            string
		initialDevice   *GPUDevice
		groupName       string
		addPosition     int
		wantAddMemory   uint
		wantAddInUse    bool
		wantAddUsedPos  int
		wantAfterSubLen int
		wantSubMemory   uint
	}{
		{
			name: "Add then Sub with valid position",
			initialDevice: &GPUDevice{
				UUID: "gpu-1",
				MigTemplate: []config.Geometry{
					{
						Group: "group-a",
						Instances: []config.MigTemplate{
							{Name: "1g.10gb", Memory: 10240, Count: 2},
						},
					},
				},
				MigUsage: config.MigInUse{},
			},
			groupName:       "group-a",
			addPosition:     1,
			wantAddMemory:   10240,
			wantAddInUse:    true,
			wantAddUsedPos:  1,
			wantAfterSubLen: 0,
			wantSubMemory:   10240,
		},
		{
			name: "Add twice same instance, then sub once",
			initialDevice: &GPUDevice{
				UUID: "gpu-2",
				MigTemplate: []config.Geometry{
					{
						Group: "group-b",
						Instances: []config.MigTemplate{
							{Name: "2g.20gb", Memory: 20480, Count: 3},
						},
					},
				},
				MigUsage: config.MigInUse{},
			},
			groupName:       "group-b",
			addPosition:     1,
			wantAddMemory:   20480,
			wantAddInUse:    true,
			wantAddUsedPos:  1,
			wantAfterSubLen: 0,
			wantSubMemory:   20480,
		},
		{
			name: "Sub from unused slot returns 0",
			initialDevice: &GPUDevice{
				UUID: "gpu-3",
				MigTemplate: []config.Geometry{
					{
						Group: "group-c",
						Instances: []config.MigTemplate{
							{Name: "3g.30gb", Memory: 30720, Count: 1},
						},
					},
				},
				MigUsage: config.MigInUse{},
			},
			groupName:       "group-c",
			addPosition:     0,
			wantAddMemory:   30720,
			wantAddInUse:    true,
			wantAddUsedPos:  0,
			wantAfterSubLen: 0,
			wantSubMemory:   30720,
		},
		{
			name: "Invalid group name should return 0",
			initialDevice: &GPUDevice{
				UUID: "gpu-4",
				MigTemplate: []config.Geometry{
					{
						Group: "group-d",
						Instances: []config.MigTemplate{
							{Name: "1g.10gb", Memory: 10240, Count: 1},
						},
					},
				},
				MigUsage: config.MigInUse{},
			},
			groupName:       "nonexistent-group",
			addPosition:     0,
			wantAddMemory:   0,
			wantAddInUse:    false,
			wantAddUsedPos:  -1,
			wantAfterSubLen: 0,
			wantSubMemory:   0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gd := c.initialDevice

			mem := addMigUsed(gd, c.groupName, c.addPosition)
			if mem != c.wantAddMemory {
				t.Errorf("addMigUsed memory: want %d, got %d", c.wantAddMemory, mem)
			}

			if c.wantAddUsedPos >= 0 {
				found := false
				for _, usage := range gd.MigUsage.UsageList {
					if usage.InUse == c.wantAddInUse {
						for _, pos := range usage.UsedIndex {
							if pos == c.wantAddUsedPos {
								found = true
								break
							}
						}
					}
				}
				if !found {
					t.Errorf("addMigUsed: used position %d not found in UsageList", c.wantAddUsedPos)
				}
			}

			subMem := subMigUsed(gd, c.groupName, c.addPosition)
			if subMem != c.wantSubMemory {
				t.Errorf("subMigUsed memory: want %d, got %d", c.wantSubMemory, subMem)
			}

			if len(gd.MigUsage.UsageList) != c.wantAfterSubLen {
				t.Errorf("UsageList length after sub: want %d, got %d", c.wantAfterSubLen, len(gd.MigUsage.UsageList))
			}
		})
	}
}

func TestMIGFactory_TryAddPod_WithMemoryFactor(t *testing.T) {
	// Initialize config to avoid nil pointer
	config.InitDevicesConfig("", "")

	testCases := []struct {
		name                string
		memoryFactor        uint
		requestMem          uint
		core                uint
		migTemplate         []config.Geometry
		expectedFound       bool
		expectedGroup       string
		expectedMemReturned uint
		expectedDeviceMem   uint
		description         string
	}{
		{
			name:         "MemoryFactor=1, request 10MB matches 1g.10gb",
			memoryFactor: 1,
			requestMem:   10,
			core:         50,
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedFound:       true,
			expectedGroup:       "group1",
			expectedMemReturned: 10240, // MIG device memory (raw)
			expectedDeviceMem:   10240, // Device records: usedMem / factor = 10240 / 1 = 10240
			description:         "With factor=1, 10MB request matches 1g.10gb (10240MB), device records 10240MB",
		},
		{
			name:         "MemoryFactor=2, request 6GB matches 1g.10gb",
			memoryFactor: 2,
			requestMem:   6144,
			core:         50,
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedFound:       true,
			expectedGroup:       "group1",
			expectedMemReturned: 20480, // MIG device memory (raw)
			expectedDeviceMem:   10240,
			description:         "With factor=2, 6GB request matches 2g.20gb",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set memory factor
			cfg := config.GetConfig()
			cfg.NvidiaConfig.GPUMemoryFactor = tc.memoryFactor

			device := &GPUDevice{
				UUID:        "gpu-1234",
				UsedNum:     0,
				UsedMem:     0,
				UsedCore:    0,
				PodMap:      make(map[string]*GPUUsage),
				MigTemplate: tc.migTemplate,
				MigUsage: config.MigInUse{
					Index:     -1,
					UsageList: config.MIGS{},
				},
			}

			factory := MIGFactory{}
			found, devID := factory.TryAddPod(device, tc.requestMem, tc.core)

			if found != tc.expectedFound {
				t.Errorf("TryAddPod found = %v, want %v\nDescription: %s", found, tc.expectedFound, tc.description)
			}

			if tc.expectedFound {
				if devID == "" {
					t.Errorf("TryAddPod returned empty devID\nDescription: %s", tc.description)
				}

				// Verify the devID contains the expected group
				if tc.expectedGroup != "" {
					group, position, err := decodeMIGID(devID)
					if err != nil {
						t.Errorf("Failed to decode devID %s: %v", devID, err)
					}
					if group != tc.expectedGroup {
						t.Errorf("Expected group %s, got %s in devID %s\nDescription: %s",
							tc.expectedGroup, group, devID, tc.description)
					}
					actualMem := addMigUsed(device, group, position)
					if actualMem != tc.expectedMemReturned {
						t.Errorf("addMigUsed returned memory = %d, want %d\nDescription: %s",
							actualMem, tc.expectedMemReturned, tc.description)
					}
				}

				// Verify device statistics
				if device.UsedNum != 1 {
					t.Errorf("After TryAddPod: UsedNum = %d, want 1\nDescription: %s", device.UsedNum, tc.description)
				}

				if device.UsedMem != tc.expectedDeviceMem {
					t.Errorf("After TryAddPod: UsedMem = %d, want %d\nDescription: %s",
						device.UsedMem, tc.expectedDeviceMem, tc.description)
				}

				if device.UsedCore != tc.core {
					t.Errorf("After TryAddPod: UsedCore = %d, want %d\nDescription: %s",
						device.UsedCore, tc.core, tc.description)
				}
			}
		})
	}
}

func TestMIGFactory_AddPod_WithMemoryFactor(t *testing.T) {
	// Initialize config to avoid nil pointer
	config.InitDevicesConfig("", "")

	testCases := []struct {
		name             string
		memoryFactor     uint
		requestMem       uint
		core             uint
		podUID           string
		devID            string
		migTemplate      []config.Geometry
		expectedError    bool
		expectedUsedMem  uint
		expectedUsedCore uint
		expectedUsedNum  uint
		expectedPodMem   uint
		expectedPodCore  uint
		expectedUsageLen int
		description      string
	}{
		{
			name:         "AddPod with MemoryFactor=1, request 10MB matches 1g.10gb",
			memoryFactor: 1,
			requestMem:   10,
			core:         50,
			podUID:       "pod-1",
			devID:        "gpu-1234[group1-0]",
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedError:    false,
			expectedUsedMem:  10240,
			expectedUsedCore: 50,
			expectedUsedNum:  1,
			expectedPodMem:   10240,
			expectedPodCore:  50,
			expectedUsageLen: 1,
			description:      "With factor=1, MIG raw memory 10240MB recorded as 10240MB",
		},
		{
			name:         "AddPod with MemoryFactor=2, request 6GB matches 1g.10gb",
			memoryFactor: 2,
			requestMem:   6144,
			core:         50,
			podUID:       "pod-2",
			devID:        "gpu-1234[group1-0]",
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedError:    false,
			expectedUsedMem:  5120,
			expectedUsedCore: 50,
			expectedUsedNum:  1,
			expectedPodMem:   5120,
			expectedPodCore:  50,
			expectedUsageLen: 1,
			description:      "With factor=2, MIG raw memory 10240MB becomes 5120MB recorded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set memory factor
			cfg := config.GetConfig()
			cfg.NvidiaConfig.GPUMemoryFactor = tc.memoryFactor

			device := &GPUDevice{
				UUID:        "gpu-1234",
				UsedNum:     0,
				UsedMem:     0,
				UsedCore:    0,
				PodMap:      make(map[string]*GPUUsage),
				MigTemplate: tc.migTemplate,
				MigUsage: config.MigInUse{
					Index:     -1,
					UsageList: config.MIGS{},
				},
			}

			factory := MIGFactory{}
			err := factory.AddPod(device, tc.requestMem, tc.core, tc.podUID, tc.devID)

			if tc.expectedError && err == nil {
				t.Errorf("AddPod expected error but got nil\nDescription: %s", tc.description)
			}
			if !tc.expectedError && err != nil {
				t.Errorf("AddPod unexpected error: %v\nDescription: %s", err, tc.description)
			}

			// Verify device statistics
			if device.UsedNum != tc.expectedUsedNum {
				t.Errorf("After AddPod: UsedNum = %d, want %d\nDescription: %s",
					device.UsedNum, tc.expectedUsedNum, tc.description)
			}

			if device.UsedMem != tc.expectedUsedMem {
				t.Errorf("After AddPod: UsedMem = %d, want %d\nDescription: %s",
					device.UsedMem, tc.expectedUsedMem, tc.description)
			}

			if device.UsedCore != tc.expectedUsedCore {
				t.Errorf("After AddPod: UsedCore = %d, want %d\nDescription: %s",
					device.UsedCore, tc.expectedUsedCore, tc.description)
			}

			// Verify pod usage
			podUsage, exists := device.PodMap[tc.podUID]
			if !exists && tc.expectedPodMem > 0 {
				t.Errorf("Pod %s not found in PodMap\nDescription: %s", tc.podUID, tc.description)
			}
			if exists {
				if podUsage.UsedMem != tc.expectedPodMem {
					t.Errorf("Pod UsedMem = %d, want %d\nDescription: %s",
						podUsage.UsedMem, tc.expectedPodMem, tc.description)
				}
				if podUsage.UsedCore != tc.expectedPodCore {
					t.Errorf("Pod UsedCore = %d, want %d\nDescription: %s",
						podUsage.UsedCore, tc.expectedPodCore, tc.description)
				}
			}
		})
	}
}

func TestMIGFactory_SubPod_WithMemoryFactor(t *testing.T) {
	// Initialize config to avoid nil pointer
	config.InitDevicesConfig("", "")

	testCases := []struct {
		name             string
		memoryFactor     uint
		requestMem       uint
		core             uint
		podUID           string
		devID            string
		migTemplate      []config.Geometry
		expectedError    bool
		expectedUsedMem  uint
		expectedUsedCore uint
		expectedUsedNum  uint
		description      string
	}{
		{
			name:         "SubPod with MemoryFactor=1, request 10MB from 1g.10gb",
			memoryFactor: 1,
			requestMem:   10,
			core:         50,
			podUID:       "pod-1",
			devID:        "gpu-1234[group1-0]",
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedError:    false,
			expectedUsedMem:  0,
			expectedUsedCore: 0,
			expectedUsedNum:  0,
			description:      "With factor=1, sub removes 10240MB from device",
		},
		{
			name:         "SubPod with MemoryFactor=2, request 6GB from 1g.10gb",
			memoryFactor: 2,
			requestMem:   6144,
			core:         50,
			podUID:       "pod-2",
			devID:        "gpu-1234[group1-0]",
			migTemplate: []config.Geometry{
				{
					Group: "group1",
					Instances: []config.MigTemplate{
						{Name: "1g.10gb", Memory: 10240, Count: 2},
						{Name: "2g.20gb", Memory: 20480, Count: 1},
					},
				},
			},
			expectedError:    false,
			expectedUsedMem:  0,
			expectedUsedCore: 0,
			expectedUsedNum:  0,
			description:      "With factor=2, sub removes 5120MB from device",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set memory factor
			cfg := config.GetConfig()
			cfg.NvidiaConfig.GPUMemoryFactor = tc.memoryFactor

			device := &GPUDevice{
				UUID:        "gpu-1234",
				UsedNum:     0,
				UsedMem:     0,
				UsedCore:    0,
				PodMap:      make(map[string]*GPUUsage),
				MigTemplate: tc.migTemplate,
				MigUsage: config.MigInUse{
					Index:     -1,
					UsageList: config.MIGS{},
				},
			}

			factory := MIGFactory{}

			// First add the pod to setup state
			err := factory.AddPod(device, tc.requestMem, tc.core, tc.podUID, tc.devID)
			if err != nil {
				t.Fatalf("Setup AddPod failed: %v\nDescription: %s", err, tc.description)
			}

			// Record state before sub
			beforeUsedMem := device.UsedMem
			beforeUsedCore := device.UsedCore

			// Now sub the pod
			err = factory.SubPod(device, tc.requestMem, tc.core, tc.podUID, tc.devID)

			if tc.expectedError && err == nil {
				t.Errorf("SubPod expected error but got nil\nDescription: %s", tc.description)
			}
			if !tc.expectedError && err != nil {
				t.Errorf("SubPod unexpected error: %v\nDescription: %s", err, tc.description)
			}

			// Verify device statistics after sub
			if device.UsedNum != tc.expectedUsedNum {
				t.Errorf("After SubPod: UsedNum = %d, want %d\nDescription: %s",
					device.UsedNum, tc.expectedUsedNum, tc.description)
			}

			if device.UsedMem != tc.expectedUsedMem {
				t.Errorf("After SubPod: UsedMem = %d, want %d (before was %d)\nDescription: %s",
					device.UsedMem, tc.expectedUsedMem, beforeUsedMem, tc.description)
			}

			if device.UsedCore != tc.expectedUsedCore {
				t.Errorf("After SubPod: UsedCore = %d, want %d (before was %d)\nDescription: %s",
					device.UsedCore, tc.expectedUsedCore, beforeUsedCore, tc.description)
			}

			// Verify pod is removed from PodMap
			if _, exists := device.PodMap[tc.podUID]; exists {
				t.Errorf("Pod %s still exists in PodMap after SubPod\nDescription: %s", tc.podUID, tc.description)
			}
		})
	}
}
