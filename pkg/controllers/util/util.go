/*
Copyright 2022 The Volcano Authors.

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
	"strings"

	v1 "k8s.io/api/core/v1"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/kubernetes/pkg/apis/core/v1/helper"
	quotacore "k8s.io/kubernetes/pkg/quota/v1/evaluator/core"
	"k8s.io/utils/clock"
)

func GetPodQuotaUsage(pod *v1.Pod) v1.ResourceList {
	res, _ := quotacore.PodUsageFunc(pod, clock.RealClock{})
	for name, quantity := range res {
		if !helper.IsNativeResource(name) && strings.HasPrefix(string(name), v1.DefaultResourceRequestsPrefix) {
			res[v1.ResourceName(strings.TrimPrefix(string(name), v1.DefaultResourceRequestsPrefix))] = quantity
		}
	}
	return res
}

// calTaskRequests returns requests resource with validReplica replicas
func CalTaskRequests(pod *v1.Pod, validReplica int32) v1.ResourceList {
	minReq := v1.ResourceList{}
	usage := GetPodQuotaUsage(pod)
	for i := int32(0); i < validReplica; i++ {
		minReq = quotav1.Add(minReq, usage)
	}
	return minReq
}
