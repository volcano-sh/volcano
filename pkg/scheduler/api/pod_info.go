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
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/features"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// Refer k8s.io/kubernetes/pkg/scheduler/algorithm/predicates/predicates.go#GetResourceRequest.
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

	// take max_resource(sum_pod, any_init_container)
	for _, container := range pod.Spec.InitContainers {
		result.SetMaxResource(NewResource(container.Resources.Requests))
	}

	return result
}

// GetPodPreemptable return volcano.sh/preemptable value for pod
func GetPodPreemptable(pod *v1.Pod) bool {
	// check annotaion first
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

	return false
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
	if pod.Spec.Overhead != nil && utilfeature.DefaultFeatureGate.Enabled(features.PodOverhead) {
		result.Add(NewResource(pod.Spec.Overhead))
	}

	return result
}

// GetGPUIndex returns the ID of the GPU
//return the gpu index list
func GetGPUIndex(pod *v1.Pod) []int {
	if len(pod.Annotations) > 0 {
		value, found := pod.Annotations[GPUIndex]
		if found {
			ids := strings.Split(value, ",")
			if len(ids) == 0 {
				klog.Errorf("invalid gpu index annotation %s=%s", GPUIndex, value)
			}
			idSlice := make([]int, len(ids))
			for idx, id := range ids {
				j, err := strconv.Atoi(id)
				if err != nil {
					klog.Errorf("invalid %s=%s", GPUIndex, value)
					return nil
				}
				idSlice[idx] = j
			}
			return idSlice
		}
	}

	return nil
}

func escapeJSONPointer(p string) string {
	// Escaping reference name using https://tools.ietf.org/html/rfc6901
	p = strings.Replace(p, "~", "~0", -1)
	p = strings.Replace(p, "/", "~1", -1)
	return p
}

// AddGPUIndexPatch returns the patch adding GPU index
func AddGPUIndexPatch(ids []int) string {
	idsstring := strings.Trim(strings.Replace(fmt.Sprint(ids), " ", ",", -1), "[]")
	return fmt.Sprintf(`[{"op": "add", "path": "/metadata/annotations/%s", "value":"%d"},`+
		`{"op": "add", "path": "/metadata/annotations/%s", "value": "%s"}]`,
		escapeJSONPointer(PredicateTime), time.Now().UnixNano(),
		escapeJSONPointer(GPUIndex), idsstring)
}

// RemoveGPUIndexPatch returns the patch removing GPU index
func RemoveGPUIndexPatch() string {
	return fmt.Sprintf(`[{"op": "remove", "path": "/metadata/annotations/%s"},`+
		`{"op": "remove", "path": "/metadata/annotations/%s"}]`, escapeJSONPointer(PredicateTime), escapeJSONPointer(GPUIndex))
}
