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

package gpushare

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func gpuMemPod(mem int64) *v1.Pod {
	return &v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Limits: v1.ResourceList{
							VolcanoGPUResource: *resource.NewQuantity(mem, resource.DecimalSI),
						},
					},
				},
			},
		},
	}
}

func oversubscribedGPUDevices() *GPUDevices {
	pod := gpuMemPod(3000)
	pod.UID = "p1"
	return &GPUDevices{
		Name: "n1",
		Device: map[int]*GPUDevice{
			0: {
				ID:     0,
				Memory: 1000,
				PodMap: map[string]*v1.Pod{string(pod.UID): pod},
			},
		},
	}
}

func TestGetDevicesIdleGPUMemory_OverSubscribedCardReportsZero(t *testing.T) {
	gs := oversubscribedGPUDevices()

	idle := getDevicesIdleGPUMemory(gs)
	if idle[0] != 0 {
		t.Errorf("idle memory of over-subscribed card: want 0, got %d", idle[0])
	}
}

func TestPredicateGPUbyMemory_OverSubscribedCardNotOffered(t *testing.T) {
	gs := oversubscribedGPUDevices()

	newPod := gpuMemPod(500)
	newPod.UID = "p2"
	if ids := predicateGPUbyMemory(newPod, gs); len(ids) != 0 {
		t.Errorf("over-subscribed card offered for allocation: %v", ids)
	}
}
