/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2021 The Volcano Authors.

Modifications made by Volcano authors:
- Added Max function for resource comparison operations

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

package helpers

import (
	"math"

	v1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// Min is used to find the min of two resource types
func Min(l, r *api.Resource) *api.Resource {
	res := &api.Resource{}

	res.MilliCPU = math.Min(l.MilliCPU, r.MilliCPU)
	res.Memory = math.Min(l.Memory, r.Memory)

	if l.ScalarResources == nil || r.ScalarResources == nil {
		return res
	}

	res.ScalarResources = map[v1.ResourceName]float64{}
	for lName, lQuant := range l.ScalarResources {
		res.ScalarResources[lName] = math.Min(lQuant, r.ScalarResources[lName])
	}

	return res
}

// Max returns the resource object with larger value in each dimension.
func Max(l, r *api.Resource) *api.Resource {
	res := &api.Resource{}

	res.MilliCPU = math.Max(l.MilliCPU, r.MilliCPU)
	res.Memory = math.Max(l.Memory, r.Memory)

	if l.ScalarResources == nil && r.ScalarResources == nil {
		return res
	}
	res.ScalarResources = map[v1.ResourceName]float64{}
	if l.ScalarResources != nil {
		for lName, lQuant := range l.ScalarResources {
			if lQuant >= 0 {
				res.ScalarResources[lName] = lQuant
			}
		}
	}
	if r.ScalarResources != nil {
		for rName, rQuant := range r.ScalarResources {
			if rQuant >= 0 {
				maxQuant := math.Max(rQuant, res.ScalarResources[rName])
				res.ScalarResources[rName] = maxQuant
			}
		}
	}
	return res
}

// Share is used to determine the share
func Share(l, r float64) float64 {
	var share float64
	if r == 0 {
		if l == 0 {
			share = 0
		} else {
			share = 1
		}
	} else {
		share = l / r
	}

	return share
}

func IsPodSpecMatch(taskPodSpec, resvPodSpec *v1.PodSpec) bool {
	if taskPodSpec.SchedulerName != resvPodSpec.SchedulerName {
		return false
	}
	if !apiequality.Semantic.DeepEqual(taskPodSpec.NodeSelector, resvPodSpec.NodeSelector) {
		return false
	}
	if !apiequality.Semantic.DeepEqual(taskPodSpec.Affinity, resvPodSpec.Affinity) {
		return false
	}
	if !apiequality.Semantic.DeepEqual(taskPodSpec.Tolerations, resvPodSpec.Tolerations) {
		return false
	}
	if taskPodSpec.PriorityClassName != resvPodSpec.PriorityClassName {
		return false
	}
	if !isContainerListEqual(taskPodSpec.Containers, resvPodSpec.Containers) {
		return false
	}
	if !isContainerListEqual(taskPodSpec.InitContainers, resvPodSpec.InitContainers) {
		return false
	}

	return true
}

func isContainerListEqual(a, b []v1.Container) bool {
	if len(a) != len(b) {
		return false
	}

	containerMap := make(map[string]v1.Container, len(a))
	for _, c := range a {
		containerMap[c.Name] = c
	}

	for _, c := range b {
		ref, ok := containerMap[c.Name]
		if !ok {
			return false
		}
		if c.Image != ref.Image {
			return false
		}
		if !apiequality.Semantic.DeepEqual(c.Resources.Requests, ref.Resources.Requests) {
			return false
		}
		if !apiequality.Semantic.DeepEqual(c.Resources.Limits, ref.Resources.Limits) {
			return false
		}
	}

	return true
}
