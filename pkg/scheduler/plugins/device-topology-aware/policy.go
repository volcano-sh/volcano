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
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	corev1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type policyScope string

const (
	parentPolicyScope   policyScope = "PodGroup"
	subGroupPolicyScope policyScope = "SubJob"

	invalidDeviceTopologyPolicyReason = "DeviceTopologyInvalidPolicy"
	unsupportedPodRequestPolicyReason = "DeviceTopologyUnsupportedPodRequest"
)

// policyUnitKey distinguishes one parent PodGroup policy from a policy owned
// by one existing SubJob of the same Job.
type policyUnitKey struct {
	jobID    api.JobID
	subJobID api.SubJobID
}

// resolvedDeviceTopologyPolicy is the scheduler-internal representation of
// one immutable device topology policy unit. It contains only value data, so
// later scheduling callbacks cannot mutate workload API objects.
type resolvedDeviceTopologyPolicy struct {
	scope            policyScope
	jobID            api.JobID
	schedulingUnitID string
	taskIDs          []api.TaskID
	requirements     []resolvedDeviceTopologyRequirement
	fabricMode       scheduling.DeviceTopologyMode
	hasFabric        bool
}

// resolvedDeviceTopologyRequirement records one accelerator resource policy
// and the exact Task/container requests selected by that resource.
type resolvedDeviceTopologyRequirement struct {
	resourceName corev1.ResourceName
	mode         scheduling.DeviceTopologyMode
	tasks        []resolvedDeviceTopologyTaskRequest
}

// resolvedDeviceTopologyTaskRequest records the request shape that a later
// planner and allocation handoff must preserve for one Task.
type resolvedDeviceTopologyTaskRequest struct {
	taskID        api.TaskID
	containerName string
	resourceName  corev1.ResourceName
	quantity      int64
}

// policyResolutionError provides a stable reason prefix for malformed policy
// units, unsupported Pod request shapes, and pure planning failures.
type policyResolutionError struct {
	reason  string
	message string
}

// Error implements error.
func (err *policyResolutionError) Error() string {
	return fmt.Sprintf("%s: %s", err.reason, err.message)
}

// newPolicyResolutionError creates a fail-closed error with a structured
// reason that later predicate and status reporting can preserve.
func newPolicyResolutionError(reason, format string, arguments ...interface{}) error {
	return &policyResolutionError{
		reason:  reason,
		message: fmt.Sprintf(format, arguments...),
	}
}

// parentPolicyUnitKey returns the key for a policy that governs the entire
// PodGroup rather than one SubJob.
func parentPolicyUnitKey(job *api.JobInfo) policyUnitKey {
	return policyUnitKey{jobID: job.UID}
}

// subGroupPolicyUnitKey returns the key for a policy that governs one SubJob.
func subGroupPolicyUnitKey(job *api.JobInfo, subJob *api.SubJobInfo) policyUnitKey {
	return policyUnitKey{jobID: job.UID, subJobID: subJob.UID}
}

// policyUnitKeyFor returns the effective policy owner for the requested unit.
// A parent policy applies to all Job Tasks, including Tasks that belong to
// ordinary subgroups with no device topology policy.
func policyUnitKeyFor(job *api.JobInfo, subJob *api.SubJobInfo) (policyUnitKey, bool) {
	if job == nil {
		return policyUnitKey{}, false
	}
	if subJob != nil && subJob.DeviceTopology != nil {
		return subGroupPolicyUnitKey(job, subJob), true
	}
	if job.DeviceTopology != nil {
		return parentPolicyUnitKey(job), true
	}
	return policyUnitKey{}, false
}

// resolveDeviceTopologyPolicy converts a typed PodGroup policy and its policy
// unit Tasks into immutable scheduler-internal data. It does not choose Nodes,
// domains, or devices.
func resolveDeviceTopologyPolicy(scope policyScope, job *api.JobInfo, subJob *api.SubJobInfo, policy *scheduling.DeviceTopologySpec, tasks map[api.TaskID]*api.TaskInfo) (*resolvedDeviceTopologyPolicy, error) {
	if job == nil {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "policy has no owning Job")
	}
	if policy == nil {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Job %q has no device topology policy", job.UID)
	}
	if len(tasks) == 0 {
		return nil, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "policy unit for Job %q has no Tasks", job.UID)
	}
	if len(policy.Requirements) == 0 {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "device topology policy for Job %q has no requirements", job.UID)
	}
	if policy.Fabric != nil && len(policy.Requirements) != 1 {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "fabric affinity for Job %q requires exactly one resource requirement", job.UID)
	}

	result := &resolvedDeviceTopologyPolicy{
		scope:        scope,
		jobID:        job.UID,
		taskIDs:      sortedTaskIDs(tasks),
		requirements: make([]resolvedDeviceTopologyRequirement, 0, len(policy.Requirements)),
	}
	if scope == parentPolicyScope {
		result.schedulingUnitID = string(job.UID)
	} else if subJob != nil {
		result.schedulingUnitID = string(subJob.UID)
	} else {
		return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "subgroup policy for Job %q has no SubJob", job.UID)
	}
	if policy.Fabric != nil {
		if !validDeviceTopologyMode(policy.Fabric.Mode) {
			return nil, newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "fabric affinity for Job %q has unsupported mode %q", job.UID, policy.Fabric.Mode)
		}
		result.hasFabric = true
		result.fabricMode = policy.Fabric.Mode
	}

	seenResources := make(map[corev1.ResourceName]struct{}, len(policy.Requirements))
	for requirementIndex, requirement := range policy.Requirements {
		resourceName, err := resolveRequirementResource(job, requirementIndex, requirement, seenResources)
		if err != nil {
			return nil, err
		}

		resolvedRequirement := resolvedDeviceTopologyRequirement{
			resourceName: resourceName,
			mode:         requirement.Mode,
			tasks:        make([]resolvedDeviceTopologyTaskRequest, 0, len(tasks)),
		}
		for _, taskID := range result.taskIDs {
			taskRequest, selected, err := resolveTaskDeviceRequest(tasks[taskID], resourceName)
			if err != nil {
				return nil, err
			}
			if selected {
				resolvedRequirement.tasks = append(resolvedRequirement.tasks, *taskRequest)
			}
		}
		if len(resolvedRequirement.tasks) == 0 {
			return nil, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "requirement %q selects no Tasks in Job %q", resourceName, job.UID)
		}
		result.requirements = append(result.requirements, resolvedRequirement)
	}

	return result, nil
}

// resolveRequirementResource validates the alpha resource selector and returns
// its extended resource name.
func resolveRequirementResource(job *api.JobInfo, index int, requirement scheduling.DeviceTopologyRequirement, seen map[corev1.ResourceName]struct{}) (corev1.ResourceName, error) {
	resourceName := requirement.Resource.ExtendedResourceName
	if resourceName == "" {
		return "", newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "requirement %d for Job %q must set extendedResourceName", index, job.UID)
	}
	if !corev1helper.IsExtendedResourceName(resourceName) {
		return "", newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "requirement %d for Job %q uses invalid extended resource %q", index, job.UID, resourceName)
	}
	if !validDeviceTopologyMode(requirement.Mode) {
		return "", newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "requirement %d for Job %q has unsupported mode %q", index, job.UID, requirement.Mode)
	}
	if _, exists := seen[resourceName]; exists {
		return "", newPolicyResolutionError(invalidDeviceTopologyPolicyReason, "Job %q repeats device topology requirement %q", job.UID, resourceName)
	}
	seen[resourceName] = struct{}{}
	return resourceName, nil
}

// resolveTaskDeviceRequest returns the exact regular-container request selected
// by one requirement. A Task with no request for the resource is intentionally
// not selected; it remains in normal Volcano scheduling.
func resolveTaskDeviceRequest(task *api.TaskInfo, resourceName corev1.ResourceName) (*resolvedDeviceTopologyTaskRequest, bool, error) {
	if task == nil || task.Pod == nil {
		return nil, false, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "Task has no Pod for resource %q", resourceName)
	}
	for _, container := range task.Pod.Spec.InitContainers {
		if _, exists := container.Resources.Requests[resourceName]; exists {
			return nil, false, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "Task %q requests %q from init container %q", task.UID, resourceName, container.Name)
		}
	}

	var selected *resolvedDeviceTopologyTaskRequest
	for _, container := range task.Pod.Spec.Containers {
		quantity, exists := container.Resources.Requests[resourceName]
		if !exists {
			continue
		}
		if quantity.IsZero() {
			return nil, false, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "Task %q requests a non-positive quantity of %q", task.UID, resourceName)
		}
		wholeDevices, exact := quantity.AsInt64()
		if !exact || wholeDevices <= 0 {
			return nil, false, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "Task %q must request an integral whole-device quantity of %q", task.UID, resourceName)
		}
		if selected != nil {
			return nil, false, newPolicyResolutionError(unsupportedPodRequestPolicyReason, "Task %q requests %q from more than one regular container", task.UID, resourceName)
		}
		selected = &resolvedDeviceTopologyTaskRequest{
			taskID:        task.UID,
			containerName: container.Name,
			resourceName:  resourceName,
			quantity:      wholeDevices,
		}
	}
	if selected == nil {
		return nil, false, nil
	}
	return selected, true, nil
}

// sortedTaskIDs returns a deterministic order for a policy unit's Tasks.
func sortedTaskIDs(tasks map[api.TaskID]*api.TaskInfo) []api.TaskID {
	result := make([]api.TaskID, 0, len(tasks))
	for taskID := range tasks {
		result = append(result, taskID)
	}
	sort.Slice(result, func(left, right int) bool {
		return string(result[left]) < string(result[right])
	})
	return result
}

// validDeviceTopologyMode reports whether a mode is supported by the alpha
// device topology policy API.
func validDeviceTopologyMode(mode scheduling.DeviceTopologyMode) bool {
	return mode == scheduling.HardDeviceTopologyMode || mode == scheduling.SoftDeviceTopologyMode
}
