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

package api

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestTopologyPlacementPlanClone(t *testing.T) {
	deviceID := DeviceID{ProviderID: "test-provider", Namespace: "test.example", Value: "device-a"}
	plan := TopologyPlacementPlan{
		JobID:            "job-a",
		SchedulingUnitID: "subjob-a",
		Revision:         7,
		FabricID:         FabricDomainID{ProviderID: "test-provider", Namespace: "test.example", Value: "fabric-a"},
		Placements: []TopologyTaskPlacement{{
			TaskID:        "task-a",
			PodUID:        types.UID("pod-a"),
			NodeName:      "node-a",
			LocalDomainID: DeviceDomainID{ProviderID: "test-provider", Namespace: "test.example", NodeUID: "node-a-uid", Value: "domain-a"},
			Assignments: []TopologyContainerAssignment{{
				ContainerName: "trainer",
				ResourceName:  corev1.ResourceName("nvidia.com/gpu"),
				DeviceIDs:     []DeviceID{deviceID},
			}},
		}},
	}

	clone := plan.Clone()
	plan.Placements[0].Assignments[0].DeviceIDs[0].Value = "mutated-device"
	plan.Placements[0].Assignments[0].ContainerName = "mutated-container"

	assignment := clone.Placements[0].Assignments[0]
	if assignment.ContainerName != "trainer" || len(assignment.DeviceIDs) != 1 || assignment.DeviceIDs[0] != deviceID {
		t.Fatalf("Clone() = %#v, want independent placement assignments", clone)
	}
}
