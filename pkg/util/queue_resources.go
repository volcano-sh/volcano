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

package util

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ValidateQueueResourceRelations validates object-local queue resource
// invariants without depending on API lookups.
func ValidateQueueResourceRelations(capability, guarantee, deserved corev1.ResourceList) error {
	for fieldName, resources := range map[string]corev1.ResourceList{
		"capability": capability,
		"guarantee":  guarantee,
		"deserved":   deserved,
	} {
		for resourceName, quantity := range resources {
			if quantity.Sign() < 0 {
				return fmt.Errorf("%s[%s] must be non-negative", fieldName, resourceName)
			}
		}
	}

	for resourceName, guaranteed := range guarantee {
		deservedQuantity, found := deserved[resourceName]
		if !found {
			return fmt.Errorf("deserved[%s] must be configured when guarantee[%s] is set", resourceName, resourceName)
		}
		if guaranteed.Cmp(deservedQuantity) > 0 {
			return fmt.Errorf("guarantee[%s] must not exceed deserved[%s]", resourceName, resourceName)
		}
	}

	for resourceName, deservedQuantity := range deserved {
		capabilityQuantity, found := capability[resourceName]
		if found && deservedQuantity.Cmp(capabilityQuantity) > 0 {
			return fmt.Errorf("deserved[%s] must not exceed capability[%s]", resourceName, resourceName)
		}
	}

	return nil
}

// AddResourceList adds source quantities to destination in place.
func AddResourceList(destination, source corev1.ResourceList) {
	for resourceName, quantity := range source {
		total := destination[resourceName]
		total.Add(quantity)
		destination[resourceName] = total
	}
}

// ValidateResourceListLimit checks only resources explicitly configured by
// the limit. Missing limit entries are treated as unconstrained.
func ValidateResourceListLimit(value, limit corev1.ResourceList, fieldName string) error {
	for resourceName, quantity := range value {
		limitQuantity, constrained := limit[resourceName]
		if constrained && quantity.Cmp(limitQuantity) > 0 {
			return fmt.Errorf("%s[%s]=%s exceeds limit %s", fieldName, resourceName, quantity.String(), limitQuantity.String())
		}
	}
	return nil
}
