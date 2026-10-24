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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TopologyContainerAssignment records the exact devices selected for one
// regular container resource or future DRA claim request.
type TopologyContainerAssignment struct {
	ContainerName string
	ResourceName  corev1.ResourceName
	ClaimName     string
	DeviceIDs     []DeviceID
}

// Clone returns an independent copy of a container assignment.
func (assignment TopologyContainerAssignment) Clone() TopologyContainerAssignment {
	clone := assignment
	clone.DeviceIDs = append([]DeviceID(nil), assignment.DeviceIDs...)
	return clone
}

// TopologyTaskPlacement records the selected Node, local domain, and concrete
// container device assignments for one Task in a complete topology plan.
type TopologyTaskPlacement struct {
	TaskID        TaskID
	PodUID        types.UID
	NodeName      string
	LocalDomainID DeviceDomainID
	Assignments   []TopologyContainerAssignment
}

// Clone returns an independent copy of a Task placement.
func (placement TopologyTaskPlacement) Clone() TopologyTaskPlacement {
	clone := placement
	clone.Assignments = make([]TopologyContainerAssignment, len(placement.Assignments))
	for index, assignment := range placement.Assignments {
		clone.Assignments[index] = assignment.Clone()
	}
	return clone
}

// TopologyPlacementPlan is the immutable complete device placement selected
// for one gang policy unit at a specific topology snapshot revision.
type TopologyPlacementPlan struct {
	JobID            JobID
	SchedulingUnitID string
	Revision         TopologyRevision
	FabricID         FabricDomainID
	Placements       []TopologyTaskPlacement
}

// Clone returns an independent copy of a topology placement plan.
func (plan TopologyPlacementPlan) Clone() TopologyPlacementPlan {
	clone := plan
	clone.Placements = make([]TopologyTaskPlacement, len(plan.Placements))
	for index, placement := range plan.Placements {
		clone.Placements[index] = placement.Clone()
	}
	return clone
}
