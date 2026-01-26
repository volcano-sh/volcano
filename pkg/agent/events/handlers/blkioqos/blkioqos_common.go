
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

package blkioqos

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/apis"
)

// ParseBlkioWeight returns the blkio weight from pod annotation.
// Returns 0 if missing or invalid. Used by the Linux handler and tests.
func ParseBlkioWeight(pod *corev1.Pod) int64 {
	if pod == nil || pod.Annotations == nil {
		return 0
	}
	weightStr, found := pod.Annotations[apis.BlkioWeightAnnotationKey]
	if !found || weightStr == "" {
		return 0
	}
	weight, err := strconv.ParseInt(weightStr, 10, 64)
	if err != nil {
		klog.Warningf("Invalid blkio-weight annotation value '%s' for pod %s/%s: %v", weightStr, pod.Namespace, pod.Name, err)
		return 0
	}
	if weight <= 0 {
		klog.Warningf("Invalid blkio-weight annotation value '%s' for pod %s/%s: must be positive", weightStr, pod.Namespace, pod.Name)
		return 0
	}
	return weight
}
