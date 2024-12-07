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

package extension

import (
	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/agent/apis"
)

type QosLevel string

const (
	// QosLevelLC means the qos level Latency Critical.
	QosLevelLC QosLevel = "LC"
	// QosLevelHLS means the qos level Highly Latency Sensitive.
	QosLevelHLS QosLevel = "HLS"
	// QosLevelLS means the qos level Latency Sensitive.
	QosLevelLS QosLevel = "LS"
	// QosLevelBE means the qos level Best Effort.
	QosLevelBE QosLevel = "BE"
)

var qosLevelMap = map[QosLevel]int{
	QosLevelLC:  2,
	QosLevelHLS: 2,
	QosLevelLS:  1,
	QosLevelBE:  -1,
}

// GetQosLevel return OS qos level by QosLevel.
// If not specified, zero will be returned.
func GetQosLevel(pod *corev1.Pod) int {
	if pod == nil {
		return 0
	}

	qosLevel := pod.GetAnnotations()[apis.PodQosLevelKey]
	return qosLevelMap[QosLevel(qosLevel)]
}

// NormalizeQosLevel normalizes qos level, for memory and network qos, only 0 and -1 are supported now.
func NormalizeQosLevel(qosLevel int64) int64 {
	if qosLevel < 0 {
		return -1
	}
	return 0
}
