/*
Copyright 2023 The Volcano Authors.

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

func gpuMemPod(uid string, mem int64) *v1.Pod {
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

func TestGetDevicesIdleGPUMemoryOverSubscribed(t *testing.T) {
	// A card with 1000 memory whose accounted usage exceeds its capacity, e.g.
	// because a pod set volcano.sh/gpu-index itself and was bound directly to the
	// node, must report 0 idle memory rather than a wrapped-around uint that makes
	// the full card look almost empty.
	pod := gpuMemPod("p1", 3000)
	pod.UID = "p1"
	gs := &GPUDevices{
		Name: "n1",
		Device: map[int]*GPUDevice{
			0: {
				ID:     0,
				Memory: 1000,
				PodMap: map[string]*v1.Pod{string(pod.UID): pod},
			},
		},
	}

	idle := getDevicesIdleGPUMemory(gs)
	if idle[0] != 0 {
		t.Errorf("idle memory of over-subscribed card: want 0, got %d", idle[0])
	}

	// the predicate must not offer the full card to another pod.
	newPod := gpuMemPod("p2", 500)
	newPod.UID = "p2"
	if ids := predicateGPUbyMemory(newPod, gs); len(ids) != 0 {
		t.Errorf("over-subscribed card offered for allocation: %v", ids)
	}
}
