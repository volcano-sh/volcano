/*
Copyright 2018 The Kubernetes Authors.

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

package cache

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
)

// responsibleForPod returns true if the pod has asked to be scheduled by the given scheduler.
func responsibleForPod(pod *v1.Pod, schedulerName string) bool {
	return schedulerName == pod.Spec.SchedulerName
}

// convertNodeSelector converts node selector from string to *LabelSelector
func convertNodeSelector(selector string) metav1.LabelSelector {
	matchLabels := make(map[string]string)
	if selector != "" {
		s := strings.Split(selector, ":")
		key, value := strings.TrimSpace(s[0]), strings.TrimSpace(s[1])
		matchLabels[key] = value
	}
	labelSelector := metav1.LabelSelector{
		MatchLabels:      matchLabels,
		MatchExpressions: nil,
	}
	return labelSelector
}
