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

package util

import (
	v1 "k8s.io/api/core/v1"
)

// IsNodeUnschedulable checks if a node is marked as unschedulable
func IsNodeUnschedulable(node *v1.Node) bool {
	if node == nil {
		return true
	}
	return node.Spec.Unschedulable
}

// IsNodeNotReady checks if a node is in NotReady state
func IsNodeNotReady(node *v1.Node) bool {
	if node == nil {
		return true
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status != v1.ConditionTrue
		}
	}
	// If no Ready condition found, consider it as NotReady
	return true
}

// IsNodeSchedulingDisabled checks if a node is either unschedulable or not ready
func IsNodeSchedulingDisabled(node *v1.Node) bool {
	return IsNodeUnschedulable(node) || IsNodeNotReady(node)
}
