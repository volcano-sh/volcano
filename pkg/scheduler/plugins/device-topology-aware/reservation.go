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

package devicetopologyaware

import (
	"context"
	"fmt"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// reserveGangPlan turns one pure static gang plan into an exact device plan
// and asks the cache to reserve it atomically. Allocation and bind handoff
// intentionally remain separate from this transaction.
func (plugin *deviceTopologyPlugin) reserveGangPlan(ctx context.Context, session *framework.Session, gangPlan *deviceTopologyGangPlan) (api.TopologyReservation, error) {
	if session == nil {
		return api.TopologyReservation{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation requires a scheduler session")
	}
	if gangPlan == nil {
		return api.TopologyReservation{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation requires a gang plan")
	}
	job, exists := session.Jobs[gangPlan.jobID]
	if !exists || job == nil {
		return api.TopologyReservation{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan references missing Job %q", gangPlan.jobID)
	}

	placementPlan, err := materializeDeviceTopologyPlacementPlan(session.DeviceTopology, job, gangPlan)
	if err != nil {
		return api.TopologyReservation{}, err
	}
	reservation, err := session.ReserveDeviceTopologyPlan(ctx, placementPlan)
	if err != nil {
		return api.TopologyReservation{}, fmt.Errorf("reserve device topology plan for scheduling unit %q: %w", gangPlan.schedulingUnitID, err)
	}
	return reservation, nil
}

// materializeDeviceTopologyPlacementPlan selects deterministic currently
// available device IDs for every static planner placement. The cache validates
// the snapshot revision and availability again before it contacts an adapter.
func materializeDeviceTopologyPlacementPlan(snapshot *api.TopologySnapshot, job *api.JobInfo, gangPlan *deviceTopologyGangPlan) (api.TopologyPlacementPlan, error) {
	if snapshot == nil {
		return api.TopologyPlacementPlan{}, newPolicyResolutionError(deviceTopologyDataNotReadyReason, "device topology reservation requires a topology snapshot")
	}
	if job == nil || gangPlan == nil {
		return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation requires a Job and gang plan")
	}
	if gangPlan.jobID != job.UID {
		return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan Job %q does not match Job %q", gangPlan.jobID, job.UID)
	}
	if gangPlan.revision == 0 || gangPlan.revision != snapshot.Revision() {
		return api.TopologyPlacementPlan{}, newPolicyResolutionError(deviceTopologyStaleReason, "device topology reservation plan revision %d does not match snapshot revision %d", gangPlan.revision, snapshot.Revision())
	}
	if len(gangPlan.placements) == 0 {
		return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan has no placements")
	}

	result := api.TopologyPlacementPlan{
		JobID:            gangPlan.jobID,
		SchedulingUnitID: gangPlan.schedulingUnitID,
		Revision:         gangPlan.revision,
		FabricID:         gangPlan.fabricID,
		Placements:       make([]api.TopologyTaskPlacement, 0, len(gangPlan.placements)),
	}
	placementIndexes := make(map[api.TaskID]int, len(gangPlan.placements))
	selectedDeviceIDs := make(map[api.DeviceDomainID]map[api.DeviceID]struct{})
	for _, plannedPlacement := range gangPlan.placements {
		task, exists := job.Tasks[plannedPlacement.taskID]
		if !exists || task == nil || task.Pod == nil || task.Pod.UID == "" {
			return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan references Task %q without a Pod UID", plannedPlacement.taskID)
		}
		if plannedPlacement.nodeName == "" || plannedPlacement.nodeUID == "" || plannedPlacement.quantity <= 0 {
			return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan has an incomplete placement for Task %q", plannedPlacement.taskID)
		}

		availableDeviceIDs := snapshot.AvailableDeviceIDs(plannedPlacement.localDomainID)
		selectedDeviceIDs[plannedPlacement.localDomainID], availableDeviceIDs = takeAvailableDeviceIDs(availableDeviceIDs, selectedDeviceIDs[plannedPlacement.localDomainID], plannedPlacement.quantity)
		if int64(len(availableDeviceIDs)) != plannedPlacement.quantity {
			return api.TopologyPlacementPlan{}, newPolicyResolutionError(deviceTopologyReservationConflictReason, "local domain %q no longer has %d available devices for Task %q", plannedPlacement.localDomainID.Value, plannedPlacement.quantity, plannedPlacement.taskID)
		}

		placementIndex, exists := placementIndexes[plannedPlacement.taskID]
		if !exists {
			placementIndex = len(result.Placements)
			placementIndexes[plannedPlacement.taskID] = placementIndex
			result.Placements = append(result.Placements, api.TopologyTaskPlacement{
				TaskID:   plannedPlacement.taskID,
				PodUID:   task.Pod.UID,
				NodeName: plannedPlacement.nodeName,
				NodeUID:  plannedPlacement.nodeUID,
			})
		} else if result.Placements[placementIndex].NodeName != plannedPlacement.nodeName || result.Placements[placementIndex].NodeUID != plannedPlacement.nodeUID {
			return api.TopologyPlacementPlan{}, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology reservation plan assigns Task %q to multiple Nodes", plannedPlacement.taskID)
		}

		result.Placements[placementIndex].Assignments = append(result.Placements[placementIndex].Assignments, api.TopologyContainerAssignment{
			ContainerName: plannedPlacement.containerName,
			ResourceName:  plannedPlacement.resourceName,
			LocalDomainID: plannedPlacement.localDomainID,
			DeviceIDs:     availableDeviceIDs,
		})
	}
	return result, nil
}

// takeAvailableDeviceIDs returns the first quantity device IDs that have not
// already been selected from the same local domain in this plan.
func takeAvailableDeviceIDs(availableDeviceIDs []api.DeviceID, selected map[api.DeviceID]struct{}, quantity int64) (map[api.DeviceID]struct{}, []api.DeviceID) {
	if selected == nil {
		selected = make(map[api.DeviceID]struct{})
	}
	capacity := len(availableDeviceIDs)
	if quantity < int64(capacity) {
		capacity = int(quantity)
	}
	result := make([]api.DeviceID, 0, capacity)
	for _, deviceID := range availableDeviceIDs {
		if _, exists := selected[deviceID]; exists {
			continue
		}
		selected[deviceID] = struct{}{}
		result = append(result, deviceID)
		if int64(len(result)) == quantity {
			break
		}
	}
	return selected, result
}
