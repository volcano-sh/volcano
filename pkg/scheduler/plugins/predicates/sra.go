/*
Copyright 2024 The Volcano Authors.

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

package predicates

import (
	"strings"

	v1 "k8s.io/api/core/v1"

	"k8s.io/klog/v2"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func calculateWeight(resources []string, args framework.Arguments) resourceWeight {
	// Values are initialized to 1.
	weight := resourceWeight{
		Weight:             1,
		Resources:          make(map[v1.ResourceName]int),
		ResourcesWeightSum: 0,
	}

	// Checks whether sra.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.Weight, SraWeight)
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		// sra.resources.[ResourceName]
		resourceKey := SraResourcesPrefix + resource
		resourceWeight := 1
		args.GetInt(&resourceWeight, resourceKey)
		if resourceWeight < 0 {
			resourceWeight = 1
		}
		weight.Resources[v1.ResourceName(resource)] = resourceWeight
		weight.ResourcesWeightSum += resourceWeight
	}

	return weight
}

// SraScore use the best fit polices during scheduling.
// Goals:
// - Schedule Tasks using BestFit Policy using sra (scarce resource avoidance) strategy
// - Improve the utilization of scarce resources on the cluster
func sraScore(task *api.TaskInfo, node *api.NodeInfo, sra resourceWeight) float64 {
	requested := task.Resreq
	capacity := node.Capacity

	// check if the node has the requested resource item
	for _, resource := range requested.ResourceNames() {
		resourceCapacity := capacity.Get(resource)

		if resourceCapacity == 0 {
			// node resources can't meet the task request, so it can be disregarded.
			klog.V(4).Infof("task %s/%s cannot sra node %s, because node capacity: %v , task need: %v",
				task.Namespace, task.Name, node.Name, capacity, requested)
			return 0
		}
	}

	score, err := ResourceSraScore(sra.Resources, capacity)
	if err != nil {
		klog.V(4).Infof("task %s/%s cannot sra node %s, node capacity: %v , task need: %v , error: %s",
			task.Namespace, task.Name, node.Name, capacity, requested, err.Error())
		return 0
	}
	klog.V(5).Infof("task %s/%s on node %s , node capacity: %s, task need: %v, score %v",
		task.Namespace, task.Name, node.Name, capacity, requested, score)

	// mapping the result from [0, weightSum] to [0, MaxNodeScore]
	if sra.ResourcesWeightSum > 0 {
		score /= float64(sra.ResourcesWeightSum)
		score = 1.0 - score
	}
	score *= float64(k8sFramework.MaxNodeScore * int64(sra.Weight))

	return score
}

// ResourceSraScore calculate the sra score for resource with provided info
func ResourceSraScore(sraResources map[v1.ResourceName]int, capacity *api.Resource) (float64, error) {
	score := 0.0

	for resource, weight := range sraResources {
		resourceCapacity := capacity.Get(resource)

		if resourceCapacity > 0 {
			score += float64(weight)
		}
	}

	return score, nil
}
