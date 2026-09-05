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

package hypernode

import (
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

// GetMembers resolves a HyperNode member selector against Kubernetes nodes.
func GetMembers(selector topologyv1alpha1.MemberSelector, nodes []*corev1.Node) sets.Set[string] {
	members := sets.New[string]()
	if selector.ExactMatch != nil {
		if selector.ExactMatch.Name == "" {
			return members
		}
		members.Insert(selector.ExactMatch.Name)
	}

	if selector.RegexMatch != nil {
		pattern := selector.RegexMatch.Pattern
		reg, err := regexp.Compile(pattern)
		if err != nil {
			klog.ErrorS(err, "Failed to compile regular expression", "pattern", pattern)
			return sets.Set[string]{}
		}
		for _, node := range nodes {
			if reg.MatchString(node.Name) {
				members.Insert(node.Name)
			}
		}
	}

	if selector.LabelMatch != nil {
		if len(selector.LabelMatch.MatchLabels) == 0 && len(selector.LabelMatch.MatchExpressions) == 0 {
			return members
		}
		labelSelector, err := metav1.LabelSelectorAsSelector(selector.LabelMatch)
		if err != nil {
			klog.ErrorS(err, "Failed to convert labelMatch to labelSelector", "labelMatch", selector.LabelMatch)
			return sets.Set[string]{}
		}
		for _, node := range nodes {
			if labelSelector.Matches(labels.Set(node.Labels)) {
				members.Insert(node.Name)
			}
		}
	}

	return members
}
