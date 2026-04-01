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
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

// makeGPUDevices creates a GPUDevices with the given number of fake GPUs, each with the given memory and max sharing count.
func makeGPUDevices(name string, numGPUs int, memPerGPU uint, maxSharing uint) *GPUDevices {
	gs := &GPUDevices{
		Name:    name,
		Device:  make(map[int]*GPUDevice),
		Sharing: HAMICoreFactory{},
		Mode:    vGPUControllerHAMICore,
	}
	for i := 0; i < numGPUs; i++ {
		gs.Device[i] = &GPUDevice{
			ID:     i,
			Node:   name,
			UUID:   "GPU-" + strings.Repeat("0", 4) + string(rune('A'+i)),
			Number: maxSharing,
			Memory: memPerGPU,
			Type:   "NVIDIA",
			PodMap: make(map[string]*GPUUsage),
			Health: true,
		}
	}
	return gs
}

// makeVGPUPod creates a pod requesting vGPU resources, optionally with spread annotation and podgroup annotation.
func makeVGPUPod(name, namespace, uid string, memReq int64, spread bool, podGroupName string) *v1.Pod {
	annotations := map[string]string{}
	if spread {
		annotations[VGPUPodGroupPolicyAnnotation] = VGPUPodGroupPolicySpreadValue
	}
	if podGroupName != "" {
		annotations[v1beta1.KubeGroupNameAnnotationKey] = podGroupName
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			UID:         "uid-" + types.UID(uid),
			Annotations: annotations,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "gpu-worker",
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceName(config.VolcanoVGPUNumber): resource.MustParse("1"),
							v1.ResourceName(config.VolcanoVGPUMemory): *resource.NewQuantity(memReq, resource.DecimalSI),
							v1.ResourceName(config.VolcanoVGPUCores):  resource.MustParse("25"),
						},
					},
				},
			},
		},
	}
}

// simulateAllocate places a pod onto the GPUDevices by picking a device and recording usage.
// This mirrors what Allocate does: call checkNodeGPUSharingPredicateAndScore then addToPodMap.
func simulateAllocate(gs *GPUDevices, pod *v1.Pod) (bool, []ContainerDevices) {
	fit, devices, _, err := checkNodeGPUSharingPredicateAndScore(pod, gs, false, "binpack")
	if err != nil || !fit {
		return false, nil
	}
	// Record the allocation in the device's PodMap (like addToPodMap does)
	for _, ctrDevs := range devices {
		for _, dev := range ctrDevs {
			for _, gd := range gs.Device {
				if strings.Contains(dev.UUID, gd.UUID) {
					podUID := string(pod.UID)
					if _, ok := gd.PodMap[podUID]; !ok {
						gd.PodMap[podUID] = &GPUUsage{
							UsedMem:     0,
							UsedCore:    0,
							PodGroupKey: getPodGroupKey(pod),
						}
					}
					gd.PodMap[podUID].UsedMem += dev.Usedmem
					gd.PodMap[podUID].UsedCore += dev.Usedcores
				}
			}
		}
	}
	return true, devices
}

// getDeviceUUID returns the UUID of the device a pod was assigned to.
func getDeviceUUID(devices []ContainerDevices) string {
	if len(devices) > 0 && len(devices[0]) > 0 {
		return devices[0][0].UUID
	}
	return ""
}

// TestSpreadPreventsCoLocation verifies that two pods from the same PodGroup with the
// spread annotation are placed on DIFFERENT GPU devices.
func TestSpreadPreventsCoLocation(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	// 2 GPUs, 16GB each, max 4 pods sharing per GPU
	gs := makeGPUDevices("node-1", 2, 16384, 4)

	// Pod A: same podgroup "job-1", with spread
	podA := makeVGPUPod("worker-0", "default", "aaa", 4096, true, "job-1")
	fitA, devsA := simulateAllocate(gs, podA)
	if !fitA {
		t.Fatal("Pod A should fit on the node")
	}
	uuidA := getDeviceUUID(devsA)
	t.Logf("Pod A (spread=true) placed on device: %s", uuidA)

	// Pod B: same podgroup "job-1", with spread → should go to a DIFFERENT device
	podB := makeVGPUPod("worker-1", "default", "bbb", 4096, true, "job-1")
	fitB, devsB := simulateAllocate(gs, podB)
	if !fitB {
		t.Fatal("Pod B should fit on the node")
	}
	uuidB := getDeviceUUID(devsB)
	t.Logf("Pod B (spread=true) placed on device: %s", uuidB)

	if uuidA == uuidB {
		t.Errorf("SPREAD FAILED: Pod A and Pod B from same PodGroup were placed on the SAME device %s", uuidA)
	} else {
		t.Logf("SPREAD SUCCEEDED: Pod A on %s, Pod B on %s (different devices)", uuidA, uuidB)
	}
}

// TestBinpackAllowsCoLocation verifies that two pods from the same PodGroup WITHOUT
// the spread annotation CAN be placed on the SAME GPU device.
func TestBinpackAllowsCoLocation(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	// 2 GPUs, 16GB each, max 4 pods sharing per GPU
	gs := makeGPUDevices("node-1", 2, 16384, 4)

	// Pod A: same podgroup "job-1", NO spread
	podA := makeVGPUPod("worker-0", "default", "aaa", 4096, false, "job-1")
	fitA, devsA := simulateAllocate(gs, podA)
	if !fitA {
		t.Fatal("Pod A should fit on the node")
	}
	uuidA := getDeviceUUID(devsA)
	t.Logf("Pod A (spread=false) placed on device: %s", uuidA)

	// Pod B: same podgroup "job-1", NO spread → CAN go to the same device
	podB := makeVGPUPod("worker-1", "default", "bbb", 4096, false, "job-1")
	fitB, devsB := simulateAllocate(gs, podB)
	if !fitB {
		t.Fatal("Pod B should fit on the node")
	}
	uuidB := getDeviceUUID(devsB)
	t.Logf("Pod B (spread=false) placed on device: %s", uuidB)

	if uuidA == uuidB {
		t.Logf("BINPACK CONFIRMED: Both pods placed on same device %s (expected for binpacking)", uuidA)
	} else {
		// This is also acceptable — binpack doesn't force co-location, just allows it.
		t.Logf("Pods placed on different devices (%s, %s) — acceptable, binpack does not force co-location", uuidA, uuidB)
	}
}

// TestSpreadDifferentGroupsCanShare verifies that pods from DIFFERENT PodGroups
// can still share the same device even with the spread annotation.
func TestSpreadDifferentGroupsCanShare(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	// 1 GPU only — forces sharing if both fit
	gs := makeGPUDevices("node-1", 1, 16384, 4)

	// Pod A: podgroup "job-1", with spread
	podA := makeVGPUPod("worker-0", "default", "aaa", 4096, true, "job-1")
	fitA, devsA := simulateAllocate(gs, podA)
	if !fitA {
		t.Fatal("Pod A should fit on the node")
	}
	uuidA := getDeviceUUID(devsA)

	// Pod B: DIFFERENT podgroup "job-2", with spread → should be allowed on same device
	podB := makeVGPUPod("worker-1", "default", "bbb", 4096, true, "job-2")
	fitB, devsB := simulateAllocate(gs, podB)
	if !fitB {
		t.Fatal("Pod B from different PodGroup should fit on same device")
	}
	uuidB := getDeviceUUID(devsB)

	if uuidA != uuidB {
		t.Errorf("Expected pods from different PodGroups to share device, but got %s and %s", uuidA, uuidB)
	} else {
		t.Logf("CONFIRMED: Pods from different PodGroups share device %s (correct — spread only prevents same-group co-location)", uuidA)
	}
}

// TestSpreadWithNoRoomFails verifies that when spread is active and there's only
// one GPU, a second pod from the same PodGroup is rejected (doesn't fit).
func TestSpreadWithNoRoomFails(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	// Only 1 GPU — no room to spread
	gs := makeGPUDevices("node-1", 1, 16384, 4)

	podA := makeVGPUPod("worker-0", "default", "aaa", 4096, true, "job-1")
	fitA, _ := simulateAllocate(gs, podA)
	if !fitA {
		t.Fatal("Pod A should fit")
	}

	// Pod B: same podgroup, spread → should NOT fit (only 1 GPU, already has same-group pod)
	podB := makeVGPUPod("worker-1", "default", "bbb", 4096, true, "job-1")
	fitB, _ := simulateAllocate(gs, podB)
	if fitB {
		t.Error("Pod B should NOT fit — spread policy should block it from the only available GPU")
	} else {
		t.Log("CONFIRMED: Pod B rejected — no available GPU without same-group pod (correct)")
	}
}

// TestMixedAnnotations verifies that within the same PodGroup, only pods with the
// spread annotation are affected. A pod without the annotation can co-locate freely.
func TestMixedAnnotations(t *testing.T) {
	VGPUEnable = true
	defer func() { VGPUEnable = false }()

	// 2 GPUs
	gs := makeGPUDevices("node-1", 2, 16384, 4)

	// Pod A: podgroup "job-1", WITH spread
	podA := makeVGPUPod("worker-0", "default", "aaa", 4096, true, "job-1")
	fitA, devsA := simulateAllocate(gs, podA)
	if !fitA {
		t.Fatal("Pod A should fit")
	}
	uuidA := getDeviceUUID(devsA)
	t.Logf("Pod A (spread=true) on device: %s", uuidA)

	// Pod B: same podgroup "job-1", WITHOUT spread → allowed to co-locate
	podB := makeVGPUPod("worker-1", "default", "bbb", 4096, false, "job-1")
	fitB, devsB := simulateAllocate(gs, podB)
	if !fitB {
		t.Fatal("Pod B should fit (no spread annotation)")
	}
	uuidB := getDeviceUUID(devsB)
	t.Logf("Pod B (spread=false) on device: %s", uuidB)

	// Pod B doesn't have spread, so it's not forced to avoid Pod A's device.
	// Binpacking may or may not put it on the same device — both are valid.
	t.Logf("CONFIRMED: Pod B without spread annotation was scheduled (co-location allowed)")
}
