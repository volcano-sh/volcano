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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestBuildHyperNode(t *testing.T) {
	tests := []struct {
		name          string
		hyperNodeName string
		tier          int
		members       []MemberConfig
		want          *topologyv1alpha1.HyperNode
	}{
		{
			name:          "build leaf hyperNode",
			hyperNodeName: "s0",
			tier:          1,
			members: []MemberConfig{
				{"node-1", topologyv1alpha1.MemberTypeNode, "regex", nil},
				{"node-2", topologyv1alpha1.MemberTypeNode, "exact", nil},
			},
			want: &topologyv1alpha1.HyperNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: "s0",
				},
				Spec: topologyv1alpha1.HyperNodeSpec{
					Tier: 1,
					Members: []topologyv1alpha1.MemberSpec{
						{Type: topologyv1alpha1.MemberTypeNode, Selector: topologyv1alpha1.MemberSelector{RegexMatch: &topologyv1alpha1.RegexMatch{Pattern: "node-1"}}},
						{Type: topologyv1alpha1.MemberTypeNode, Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node-2"}}},
					},
				},
			},
		},
		{
			name:          "build non-leaf hyperNode",
			hyperNodeName: "s4",
			tier:          2,
			members: []MemberConfig{
				{"s0", topologyv1alpha1.MemberTypeHyperNode, "exact", nil},
				{"s1", topologyv1alpha1.MemberTypeHyperNode, "exact", nil},
			},
			want: &topologyv1alpha1.HyperNode{
				ObjectMeta: metav1.ObjectMeta{
					Name: "s4",
				},
				Spec: topologyv1alpha1.HyperNodeSpec{
					Tier: 2,
					Members: []topologyv1alpha1.MemberSpec{
						{Type: topologyv1alpha1.MemberTypeHyperNode, Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "s0"}}},
						{Type: topologyv1alpha1.MemberTypeHyperNode, Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "s1"}}},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, BuildHyperNode(tt.hyperNodeName, tt.tier, tt.members), "BuildHyperNode(%v, %v, %v)", tt.hyperNodeName, tt.tier, tt.members)
		})
	}
}
