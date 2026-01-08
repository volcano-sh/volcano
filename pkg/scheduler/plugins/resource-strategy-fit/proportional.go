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

package resourcestrategyfit

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type baseResource struct {
	CPU    float64
	Memory float64
}

func calculateProportionalResources(resources []string, cfg proportionalConfig) map[v1.ResourceName]baseResource {
	resourcesProportional := make(map[v1.ResourceName]baseResource)

	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}
		// proportional.resources.[ResourceName]
		cpuResourceKey := resource + ".cpu"
		cpuResourceRate := 1.0
		if v, ok := cfg.ResourceProportion[cpuResourceKey]; ok {
			cpuResourceRate = v
		}
		if cpuResourceRate < 0 {
			cpuResourceRate = 1.0
		}
		memoryResourceKey := resource + ".memory"
		memoryResourceRate := 1.0
		if v, ok := cfg.ResourceProportion[memoryResourceKey]; ok {
			memoryResourceRate = v
		}
		if memoryResourceRate < 0 {
			memoryResourceRate = 1.0
		}
		r := baseResource{
			CPU:    cpuResourceRate,
			Memory: memoryResourceRate,
		}
		resourcesProportional[v1.ResourceName(resource)] = r
	}

	return resourcesProportional
}

// checkNodeResourceIsProportional checks if a gpu:cpu:memory is Proportional
func checkNodeResourceIsProportional(task *api.TaskInfo, node *api.NodeInfo, proportional map[v1.ResourceName]baseResource) (*api.Status, error) {
	status := &api.Status{
		Code:   api.Success,
		Plugin: PluginName,
	}
	for resourceName := range proportional {
		if value, found := task.Resreq.ScalarResources[resourceName]; found && value > 0 {
			return status, nil
		}
	}

	for resourceName, resourceRate := range proportional {
		if value, found := node.Idle.ScalarResources[resourceName]; found {
			cpuReserved := value * resourceRate.CPU
			memoryReserved := value * resourceRate.Memory * 1000 * 1000

			if node.Idle.MilliCPU-task.Resreq.MilliCPU < cpuReserved || node.Idle.Memory-task.Resreq.Memory < memoryReserved {
				status.Code = api.UnschedulableAndUnresolvable
				status.Reason = fmt.Sprintf("proportional of resource %s check failed", resourceName)
				return status, fmt.Errorf("proportional of resource %s check failed", resourceName)
			}
		}
	}
	return status, nil
}
