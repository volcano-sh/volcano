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

package testutil

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

// MemberConfig describes a test HyperNode member (mirrors scheduler api test helpers).
type MemberConfig struct {
	Name          string
	Type          topologyv1alpha1.MemberType
	Selector      string
	LabelSelector *metav1.LabelSelector
}

// BuildHyperNode builds a HyperNode for tests.
func BuildHyperNode(name string, tier int, members []MemberConfig) *topologyv1alpha1.HyperNode {
	hn := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:    tier,
			Members: make([]topologyv1alpha1.MemberSpec, len(members)),
		},
	}

	for i, member := range members {
		memberSpec := topologyv1alpha1.MemberSpec{
			Type: member.Type,
		}
		switch member.Selector {
		case "exact":
			memberSpec.Selector.ExactMatch = &topologyv1alpha1.ExactMatch{Name: member.Name}
		case "regex":
			memberSpec.Selector.RegexMatch = &topologyv1alpha1.RegexMatch{Pattern: member.Name}
		case "label":
			memberSpec.Selector.LabelMatch = member.LabelSelector
		default:
			return nil
		}

		hn.Spec.Members[i] = memberSpec
	}

	return hn
}
