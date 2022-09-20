package util

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
)

const (
	defaultSchedulerName = "volcano"
)

// Contains check if slice contains element
func Contains(slice []string, element string) bool {
	for _, item := range slice {
		if item == element {
			return true
		}
	}
	return false
}

// GenerateComponentName generate component name volcano
func GenerateComponentName(schedulerNames []string) string {
	if len(schedulerNames) == 1 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}

// GenerateSchedulerName generate scheduler name for volcano job
func GenerateSchedulerName(schedulerNames []string) string {
	// choose the first scheduler name for volcano job if its schedulerName is empty
	if len(schedulerNames) > 0 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}

func V1ResourceListToString(resourceList v1.ResourceList) string {
	resourceListStr := ""
	for key, quantity := range resourceList {
		if key == v1.ResourceCPU || key == v1.ResourceMemory {
			resourceListStr += fmt.Sprintf("{%s: %s}", key, quantity.String())
		}
	}
	return resourceListStr
}
