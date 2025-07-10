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

	"k8s.io/klog/v2"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type sraWeight struct {
	Weight             int
	Resources          map[v1.ResourceName]int
	ResourcesWeightSum int
}

func (w *sraWeight) String() string {
	length := 1
	if extendLength := len(w.Resources); extendLength == 0 {
		length++
	} else {
		length += extendLength
	}

	msg := make([]string, 0, length)
	msg = append(msg,
		fmt.Sprintf(resourceFmt, SraWeight, w.Weight),
	)

	if len(w.Resources) == 0 {
		msg = append(msg, "no extend resources.")
	} else {
		for name, weight := range w.Resources {
			msg = append(msg, fmt.Sprintf(resourceFmt, name, weight))
		}
	}

	return strings.Join(msg, ", ")
}

func calculateSraWeight(resources []string, cfg sraConfig) sraWeight {
	// Values are initialized to 1.
	weight := sraWeight{
		Weight:             1,
		Resources:          make(map[v1.ResourceName]int),
		ResourcesWeightSum: 0,
	}

	weight.Weight = cfg.Weight
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		resourceWeight := 1
		if v, ok := cfg.ResourceWeight[resource]; ok {
			resourceWeight = v
		}
		if resourceWeight < 0 {
			resourceWeight = 1
		}
		weight.Resources[v1.ResourceName(resource)] = resourceWeight
		weight.ResourcesWeightSum += resourceWeight
	}

	return weight
}

// sraScore use the best fit polices during scheduling.
// Goals:
// - Schedule Tasks using BestFit Policy using sra policy
// - Improve the utilization of scarce resources on the cluster
func sraScore(task *api.TaskInfo, node *api.NodeInfo, sra sraWeight) float64 {
	requested := task.Resreq
	allocatable := node.Allocatable

	// check if the node has the requested resource item
	for _, resource := range requested.ResourceNames() {
		resourceCapacity := allocatable.Get(resource)

		if resourceCapacity == 0 {
			// node resources can't meet the task request, so it can be disregarded.
			klog.V(4).Infof("task %s/%s doesn't need to consider node %s, because node allocatable: %v , task need: %v",
				task.Namespace, task.Name, node.Name, allocatable, requested)
			return 0
		}
	}

	score, err := resourceSraScore(sra.Resources, allocatable)
	if err != nil {
		klog.V(4).Infof("task %s/%s cannot sra node %s, node allocatable: %v , task need: %v , error: %s",
			task.Namespace, task.Name, node.Name, allocatable, requested, err.Error())
		return 0
	}
	klog.V(5).Infof("task %s/%s on node %s , node allocatable: %s, task need: %v, score %v",
		task.Namespace, task.Name, node.Name, allocatable, requested, score)

	// mapping the result from [0, weightSum] to [0, MaxNodeScore]
	if sra.ResourcesWeightSum > 0 {
		score /= float64(sra.ResourcesWeightSum)
		score = 1.0 - score
	}
	score *= float64(k8sFramework.MaxNodeScore * int64(sra.Weight))

	return score
}

// resourceSraScore calculate the sra score for resource with provided info
func resourceSraScore(resources map[v1.ResourceName]int, capacity *api.Resource) (float64, error) {
	score := 0.0

	for resource, weight := range resources {
		resourceCapacity := capacity.Get(resource)

		if resourceCapacity > 0 {
			score += float64(weight)
		}
	}

	return score, nil
}
