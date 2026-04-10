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

package framework

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestAddTaskDRAAllocatedByQueueDeduplicatesSharedClaims(t *testing.T) {
	queueID := api.QueueID("q1")
	allocated := make(map[api.QueueID]map[string]*api.DRAResource)
	resourceClaimRefs := make(map[api.QueueID]map[string]int)

	taskA := &api.TaskInfo{
		ResourceClaimKeys: []string{"ns1/shared"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"ns1/shared": {
				"gpu.example.com": {Count: 1},
			},
		},
	}
	taskB := &api.TaskInfo{
		ResourceClaimKeys: []string{"ns1/shared"},
		ResourceClaimDRAResreq: map[string]map[string]*api.DRAResource{
			"ns1/shared": {
				"gpu.example.com": {Count: 1},
			},
		},
	}

	addTaskDRAAllocatedByQueue(allocated, resourceClaimRefs, queueID, taskA)
	addTaskDRAAllocatedByQueue(allocated, resourceClaimRefs, queueID, taskB)

	got := allocated[queueID]["gpu.example.com"]
	if got == nil || got.Count != 1 {
		t.Fatalf("expected shared claim to be charged once, got %#v", got)
	}
}

func TestMergeDRAAllocatedIntoResourceList(t *testing.T) {
	allocated := map[string]*api.DRAResource{
		"gpu.example.com": {
			Count: 2,
			Capacity: map[string]resource.Quantity{
				"cores":  resource.MustParse("300"),
				"memory": resource.MustParse("8Gi"),
			},
		},
	}

	resourceList := mergeDRAAllocatedIntoResourceList(nil, allocated)

	expectations := map[v1.ResourceName]resource.Quantity{
		v1.ResourceName("deviceclass/gpu.example.com"):        resource.MustParse("2"),
		v1.ResourceName("cores.deviceclass/gpu.example.com"):  resource.MustParse("300"),
		v1.ResourceName("memory.deviceclass/gpu.example.com"): resource.MustParse("8Gi"),
	}

	for name, expected := range expectations {
		actual, found := resourceList[name]
		if !found {
			t.Fatalf("expected resource %s to exist", name)
		}
		if actual.Cmp(expected) != 0 {
			t.Fatalf("expected %s=%s, got %s", name, expected.String(), actual.String())
		}
	}
}
