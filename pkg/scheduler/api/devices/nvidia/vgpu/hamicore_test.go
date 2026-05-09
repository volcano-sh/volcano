/*
Copyright 2026 The Volcano Authors.

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

import "testing"

func newHAMITestDevice() *GPUDevice {
	return &GPUDevice{
		ID:     0,
		UUID:   "GPU-0",
		Number: 4,
		Memory: 16384,
		Type:   "NVIDIA",
		Health: true,
		PodMap: make(map[string]*GPUUsage),
	}
}

func TestHAMICoreFactory_AddPod(t *testing.T) {
	const podUID = "pod-a"
	testCases := []struct {
		name         string
		setup        func(gd *GPUDevice)
		wantUsedNum  uint
		wantUsedMem  uint
		wantUsedCore uint
	}{
		{
			name:         "fresh add increments counters and seeds PodMap",
			setup:        func(gd *GPUDevice) {},
			wantUsedNum:  1,
			wantUsedMem:  1000,
			wantUsedCore: 25,
		},
		{
			name: "duplicate add is a no-op",
			setup: func(gd *GPUDevice) {
				gd.UsedNum, gd.UsedMem, gd.UsedCore = 1, 1000, 25
				gd.PodMap[podUID] = &GPUUsage{UsedMem: 1000, UsedCore: 25}
			},
			wantUsedNum:  1,
			wantUsedMem:  1000,
			wantUsedCore: 25,
		},
		{
			name: "add after Allocate's TryAddPod and addToPodMap does not double-count",
			setup: func(gd *GPUDevice) {
				HAMICoreFactory{}.TryAddPod(gd, 1000, 25)
				gd.PodMap[podUID] = &GPUUsage{UsedMem: 1000, UsedCore: 25}
			},
			wantUsedNum:  1,
			wantUsedMem:  1000,
			wantUsedCore: 25,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gd := newHAMITestDevice()
			tc.setup(gd)
			if err := (HAMICoreFactory{}).AddPod(gd, 1000, 25, podUID, gd.UUID); err != nil {
				t.Fatalf("AddPod: %v", err)
			}
			if gd.UsedNum != tc.wantUsedNum || gd.UsedMem != tc.wantUsedMem || gd.UsedCore != tc.wantUsedCore {
				t.Errorf("counters: got %d/%d/%d, want %d/%d/%d",
					gd.UsedNum, gd.UsedMem, gd.UsedCore,
					tc.wantUsedNum, tc.wantUsedMem, tc.wantUsedCore)
			}
			if _, ok := gd.PodMap[podUID]; !ok {
				t.Errorf("PodMap[%q] missing after AddPod", podUID)
			}
		})
	}
}

func TestHAMICoreFactory_AddPodDistinctUIDsAccumulate(t *testing.T) {
	gd := newHAMITestDevice()
	for _, uid := range []string{"pod-a", "pod-b", "pod-c"} {
		if err := (HAMICoreFactory{}).AddPod(gd, 1000, 25, uid, gd.UUID); err != nil {
			t.Fatalf("AddPod(%s): %v", uid, err)
		}
	}
	if gd.UsedNum != 3 || gd.UsedMem != 3000 || gd.UsedCore != 75 {
		t.Errorf("counters after 3 distinct adds: got %d/%d/%d, want 3/3000/75",
			gd.UsedNum, gd.UsedMem, gd.UsedCore)
	}
}

func TestHAMICoreFactory_SubPodRemovesEntry(t *testing.T) {
	const podUID = "pod-a"
	f := HAMICoreFactory{}
	gd := newHAMITestDevice()

	if err := f.AddPod(gd, 1000, 25, podUID, gd.UUID); err != nil {
		t.Fatalf("AddPod: %v", err)
	}
	if err := f.SubPod(gd, 1000, 25, podUID, gd.UUID); err != nil {
		t.Fatalf("SubPod: %v", err)
	}
	if _, ok := gd.PodMap[podUID]; ok {
		t.Errorf("SubPod left a stale PodMap entry")
	}
	if gd.UsedNum != 0 || gd.UsedMem != 0 || gd.UsedCore != 0 {
		t.Errorf("after SubPod: got %d/%d/%d, want 0/0/0", gd.UsedNum, gd.UsedMem, gd.UsedCore)
	}
	if err := f.AddPod(gd, 1000, 25, podUID, gd.UUID); err != nil {
		t.Fatalf("re-AddPod: %v", err)
	}
	if gd.UsedNum != 1 || gd.UsedMem != 1000 {
		t.Errorf("re-AddPod after SubPod: got %d/%d, want 1/1000", gd.UsedNum, gd.UsedMem)
	}
}
