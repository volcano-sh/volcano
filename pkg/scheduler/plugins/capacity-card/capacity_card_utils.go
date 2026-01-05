/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"encoding/json"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	// EventTypeGetTaskRequestResourceFailed is get task request resource failed event type
	EventTypeGetTaskRequestResourceFailed = "GetTaskRequestResourceFailed"

	// EventTypeInsufficientCPUMemoryQuota is empty queue capability event type
	EventTypeEmptyQueueCapability = "EmptyQueueCapability"

	// EventTypeInsufficientCPUQuota is insufficient CPU quota event type
	EventTypeInsufficientCPUQuota = "InsufficientCPUQuota"

	// EventTypeInsufficientMemoryQuota is insufficient memory quota event type
	EventTypeInsufficientMemoryQuota = "InsufficientMemoryQuota"

	// EventTypeInsufficientScalarQuota is insufficient scalar quota event type
	EventTypeInsufficientScalarQuota = "InsufficientScalarQuota"
)

// CheckMode defines the mode for checking scalar resources
type CheckMode int

const (
	// CheckModeJob is for job enqueue phase, allows sum of multi-card quotas
	CheckModeJob CheckMode = iota
	// CheckModeTask is for task allocate phase, requires single card type to satisfy
	CheckModeTask
)

// GetCardResourceFromAnnotations extracts card resource from annotations.
// name is the name of the object name (e.g. job, task, queue, etc.) in string format which is used for logging the error message when unmarshal card json failed.
// annotations is the annotations of the object
// key is the key of the annotations
// return the card resource
func GetCardResourceFromAnnotations(name string, annotations map[string]string, key string) *api.Resource {
	cardResource := api.EmptyResource()
	if cardJson, ok := annotations[key]; ok && cardJson != "" {
		cardMap := make(map[string]int)
		if err := json.Unmarshal([]byte(cardJson), &cardMap); err != nil {
			klog.Warningf(
				`failed to unmarshal card json: %s, key: %s, name: %s, %+v`,
				cardJson, key, name, err,
			)
			return cardResource
		}
		if cardResource.ScalarResources == nil {
			cardResource.ScalarResources = make(map[v1.ResourceName]float64)
		}
		for cardName, cardCount := range cardMap {
			cardResource.ScalarResources[v1.ResourceName(cardName)] = float64(
				cardCount * cardCountQuantityMultiplier,
			)
		}
	}
	return cardResource
}

// CheckSingleScalarResourceResult is the result of CheckSingleScalarResource.
type CheckSingleScalarResourceResult struct {
	Ok                   bool
	NoEnoughScalarName   v1.ResourceName
	NoEnoughScalarCount  float64
	ToBeUsedScalarQuant  float64
	QueueCapabilityQuant float64
}

// CheckSingleScalarResource checks whether the scalar resource is enough.
// mode: CheckModeJob for job enqueue phase (allows sum of multi-card quotas),
//
//	CheckModeTask for task allocate phase (requires single card type to satisfy)
func CheckSingleScalarResource(
	scalarName v1.ResourceName,
	scalarQuant float64,
	toBeUsedResource, queueCapability *api.Resource,
	mode CheckMode,
) CheckSingleScalarResourceResult {
	result := CheckSingleScalarResourceResult{
		Ok: true,
	}
	// The multi-cards name is given like: NVIDIA-GTX-GeForce-4090D|NVIDIA-H200 .
	// The card name is confirmed after the task is assigned to certain node,
	// in which the card name is extracted from node's label.
	//
	// For Task mode: any one of the card can satisfy the request is ok (single task uses single card type).
	// For Job mode: sum of all card quotas can satisfy the request (different tasks in same job can use different card types).
	if strings.Contains(scalarName.String(), MultiCardSeparator) {
		multiCardNames := strings.Split(scalarName.String(), MultiCardSeparator)

		if mode == CheckModeJob {
			// Job mode: check if sum of all card quotas can satisfy the request
			// This allows different tasks in the same job to use different card types
			totalAvailableQuota := float64(0)
			totalToBeUsed := float64(0)

			for _, cardName := range multiCardNames {
				cardResourceName := v1.ResourceName(cardName)
				totalAvailableQuota += queueCapability.ScalarResources[cardResourceName]
				totalToBeUsed += toBeUsedResource.ScalarResources[cardResourceName]
			}

			// Add the current request to the total to be used
			totalToBeUsed += scalarQuant

			if scalarQuant > 0 && totalToBeUsed > totalAvailableQuota {
				result.Ok = false
				result.NoEnoughScalarName = scalarName
				result.NoEnoughScalarCount = scalarQuant
				result.ToBeUsedScalarQuant = totalToBeUsed
				result.QueueCapabilityQuant = totalAvailableQuota
				return result
			}

			result.Ok = true
			result.ToBeUsedScalarQuant = totalToBeUsed
			result.QueueCapabilityQuant = totalAvailableQuota
			return result
		}

		// Task mode: check if any single card type can satisfy the request
		// If the scalar name is multi-cards name, the scalar quant should be added to each card.
		// The multi-cards name is given like: NVIDIA-GTX-GeForce-4090D|NVIDIA-H200 .
		// Now has allocated 5 NVIDIA-GTX-GeForce-4090D and 8 NVIDIA-H200, requests another 2 NVIDIA-GTX-GeForce-4090D|NVIDIA-H200
		// The `toBeUsedResource` scalar quant is {"NVIDIA-GTX-GeForce-4090D|NVIDIA-H200": 2, "NVIDIA-GTX-GeForce-4090D": 5, "NVIDIA-H200": 8}
		// NVIDIA-GTX-GeForce-4090D and NVIDIA-H200 in `toBeUsedResource` do not contain the requested 2 card, so it should be added to each card name.
		// `multiCardToBeUsedResource` scalar quant is {"NVIDIA-GTX-GeForce-4090D|NVIDIA-H200": 2, "NVIDIA-GTX-GeForce-4090D": 7, "NVIDIA-H200": 10}
		multiCardToBeUsedResource := toBeUsedResource.Clone()
		for _, cardName := range multiCardNames {
			multiCardToBeUsedResource.ScalarResources[v1.ResourceName(cardName)] += scalarQuant
			if result = CheckSingleScalarResource(
				v1.ResourceName(cardName), scalarQuant, multiCardToBeUsedResource, queueCapability, mode,
			); result.Ok {
				return result
			}
		}
		result.Ok = false
		result.NoEnoughScalarName = scalarName
		result.NoEnoughScalarCount = scalarQuant
		return result
	}

	result.ToBeUsedScalarQuant = toBeUsedResource.ScalarResources[scalarName]
	result.QueueCapabilityQuant = queueCapability.ScalarResources[scalarName]
	if scalarQuant > 0 && result.ToBeUsedScalarQuant > result.QueueCapabilityQuant {
		result.Ok = false
		result.NoEnoughScalarName = scalarName
		result.NoEnoughScalarCount = scalarQuant
		return result
	}
	result.Ok = true
	return result
}
