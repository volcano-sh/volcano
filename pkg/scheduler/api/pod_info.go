/*
Copyright 2019 The Kubernetes Authors.

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

package api

import (
	"encoding/json"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// Refer k8s.io/kubernetes/pkg/api/v1/resource/helpers.go#PodRequests.
//
// GetResourceRequest returns a *Resource that covers the largest width in each resource dimension.
// Because init-containers run sequentially, we collect the max in each dimension iteratively.
// In contrast, we sum the resource vectors for regular containers since they run simultaneously.
//
// To be consistent with kubernetes default scheduler, it is only used for predicates of actions(e.g.
// allocate, backfill, preempt, reclaim), please use GetPodResourceWithoutInitContainers for other cases.
//
// Example:
//
// Pod:
//   InitContainers
//     IC1:
//       CPU: 2
//       Memory: 1G
//     IC2:
//       CPU: 2
//       Memory: 3G
//   Containers
//     C1:
//       CPU: 2
//       Memory: 1G
//     C2:
//       CPU: 1
//       Memory: 1G
//
// Result: CPU: 3, Memory: 3G

// GetPodResourceRequest returns all the resource required for that pod
func GetPodResourceRequest(pod *v1.Pod) *Resource {
	result := GetPodResourceWithoutInitContainers(pod)

	restartableInitContainerReqs := EmptyResource()
	initContainerReqs := EmptyResource()
	for _, container := range pod.Spec.InitContainers {
		containerReq := NewResource(container.Resources.Requests)

		if container.RestartPolicy != nil && *container.RestartPolicy == v1.ContainerRestartPolicyAlways {
			// Add the restartable container's req to the resulting cumulative container requests.
			result.Add(containerReq)

			// Track our cumulative restartable init container resources
			restartableInitContainerReqs.Add(containerReq)
			containerReq = restartableInitContainerReqs
		} else {
			tmp := EmptyResource()
			tmp.Add(containerReq)
			tmp.Add(restartableInitContainerReqs)
			containerReq = tmp
		}
		initContainerReqs.SetMaxResource(containerReq)
	}

	result.SetMaxResource(initContainerReqs)
	result.AddScalar(v1.ResourcePods, 1)

	return result
}

// GetPodPreemptable return volcano.sh/preemptable value for pod
func GetPodPreemptable(pod *v1.Pod) bool {
	// check annotation first
	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	// it annotation does not exit, check label
	if len(pod.Labels) > 0 {
		if value, found := pod.Labels[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	return true
}

// GetPodRevocableZone return volcano.sh/revocable-zone value for pod/podgroup
func GetPodRevocableZone(pod *v1.Pod) string {
	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.RevocableZone]; found {
			if value != "*" {
				return ""
			}
			return value
		}

		if value, found := pod.Annotations[v1beta1.PodPreemptable]; found {
			if b, err := strconv.ParseBool(value); err == nil && b {
				return "*"
			}
		}
	}
	return ""
}

// GetPodTopologyInfo return volcano.sh/numa-topology-policy value for pod
func GetPodTopologyInfo(pod *v1.Pod) *TopologyInfo {
	info := TopologyInfo{
		ResMap: make(map[int]v1.ResourceList),
	}

	if len(pod.Annotations) > 0 {
		if value, found := pod.Annotations[v1beta1.NumaPolicyKey]; found {
			info.Policy = value
		}

		if value, found := pod.Annotations[topologyDecisionAnnotation]; found {
			decision := PodResourceDecision{}
			err := json.Unmarshal([]byte(value), &decision)
			if err == nil {
				info.ResMap = decision.NUMAResources
			}
		}
	}

	return &info
}

// GetPodResourceWithoutInitContainers returns Pod's resource request, it does not contain
// init containers' resource request.
func GetPodResourceWithoutInitContainers(pod *v1.Pod) *Resource {
	result := EmptyResource()
	for _, container := range pod.Spec.Containers {
		result.Add(NewResource(container.Resources.Requests))
	}

	// if PodOverhead feature is supported, add overhead for running a pod
	if pod.Spec.Overhead != nil {
		result.Add(NewResource(pod.Spec.Overhead))
	}

	return result
}
