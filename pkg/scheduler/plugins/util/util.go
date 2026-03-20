/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2024 The Volcano Authors.

Modifications made by Volcano authors:
- Added NormalizeScore function for node scoring normalization
- Added GetInqueueResource for calculating reserved resources of running jobs
- Added comprehensive resource allocation tracking functions (GetAllocatedResource, CalculateAllocatedTaskNum)

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

package util

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	v1helper "k8s.io/kubernetes/pkg/scheduler/util"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	// Permit indicates that plugin callback function permits job to be inqueue, pipelined, or other status
	Permit = 1
	// Abstain indicates that plugin callback function abstains in voting job to be inqueue, pipelined, or other status
	Abstain = 0
	// Reject indicates that plugin callback function rejects job to be inqueue, pipelined, or other status
	Reject = -1
)

// NormalizeScore normalizes the score for each filteredNode
func NormalizeScore(maxPriority int64, reverse bool, scores []api.ScoredNode) {
	var maxCount int64
	for _, scoreNode := range scores {
		if scoreNode.Score > maxCount {
			maxCount = scoreNode.Score
		}
	}

	if maxCount == 0 {
		if reverse {
			for idx := range scores {
				scores[idx].Score = maxPriority
			}
		}
		return
	}

	for idx, scoreNode := range scores {
		score := maxPriority * scoreNode.Score / maxCount
		if reverse {
			score = maxPriority - score
		}

		scores[idx].Score = score
	}
}

// GetAllocatedResource returns allocated resource for given job
func GetAllocatedResource(job *api.JobInfo) *api.Resource {
	allocated := &api.Resource{}
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) {
			for _, t := range tasks {
				allocated.Add(t.Resreq)
			}
		}
	}
	return allocated
}

// CalculateAllocatedTaskNum returns allocated resource num for given job
func CalculateAllocatedTaskNum(job *api.JobInfo) int {
	allocatedNum := 0
	for status, tasks := range job.TaskStatusIndex {
		if api.AllocatedStatus(status) {
			allocatedNum += len(tasks)
		}
	}
	return allocatedNum
}

// GetInqueueResource returns reserved resource for running job whose part of pods have not been allocated resource.
func GetInqueueResource(job *api.JobInfo, allocated *api.Resource) *api.Resource {
	inqueue := &api.Resource{}
	for rName, rQuantity := range *job.PodGroup.Spec.MinResources {
		switch rName {
		case v1.ResourceCPU:
			reservedCPU := float64(rQuantity.MilliValue()) - allocated.MilliCPU
			if reservedCPU > 0 {
				inqueue.MilliCPU = reservedCPU
			}
		case v1.ResourceMemory:
			reservedMemory := float64(rQuantity.Value()) - allocated.Memory
			if reservedMemory > 0 {
				inqueue.Memory = reservedMemory
			}
		default:
			if api.IsCountQuota(rName) || !v1helper.IsScalarResourceName(rName) {
				continue
			}
			ignore := false
			api.IgnoredDevicesList.Range(func(_ int, ignoredDevice string) bool {
				if len(ignoredDevice) > 0 && strings.Contains(rName.String(), ignoredDevice) {
					ignore = true
					return false
				}
				return true
			})
			if ignore {
				continue
			}
			if inqueue.ScalarResources == nil {
				inqueue.ScalarResources = make(map[v1.ResourceName]float64)
			}
			if allocatedMount, ok := allocated.ScalarResources[rName]; !ok {
				inqueue.ScalarResources[rName] = float64(rQuantity.Value())
			} else {
				reservedScalarRes := float64(rQuantity.Value()) - allocatedMount
				if reservedScalarRes > 0 {
					inqueue.ScalarResources[rName] = reservedScalarRes
				}
			}
		}
	}
	return inqueue
}

// ShouldAbort determines if the given status indicates that execution should be aborted.
// It checks if the status code corresponds to any of the following conditions:
// - UnschedulableAndUnresolvable: Indicates the task cannot be scheduled and resolved.
// - Error: Represents an error state that prevents further execution.
// - Wait: Suggests that the process should pause and not proceed further.
// - Skip: Indicates that the operation should be skipped entirely.
//
// Parameters:
// - status (*api.Status): The status object to evaluate.
//
// Returns:
// - bool: True if the status code matches any of the abort conditions; false otherwise.
func ShouldAbort(status *api.Status) bool {
	return status.Code == api.UnschedulableAndUnresolvable ||
		status.Code == api.Error ||
		status.Code == api.Wait ||
		status.Code == api.Skip
}

// FormatResourceNames formats a list of resource names into a human-readable string.
func FormatResourceNames(prefix, verb string, resourceNames []string) string {
	if len(resourceNames) == 0 {
		return prefix
	}

	var parts []string
	for _, name := range resourceNames {
		parts = append(parts, fmt.Sprintf("%s %s", verb, name))
	}

	return fmt.Sprintf("%s: %s", prefix, strings.Join(parts, ", "))
}
