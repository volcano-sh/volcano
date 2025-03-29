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

package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func newMemberSpec(memberType topologyv1alpha1.MemberType, memberName string) topologyv1alpha1.MemberSpec {
	return topologyv1alpha1.MemberSpec{
		Type: memberType,
		Selector: topologyv1alpha1.MemberSelector{
			ExactMatch: &topologyv1alpha1.ExactMatch{
				Name: memberName,
			},
		},
	}
}

func TestAppendHyperNodeMember(t *testing.T) {
	testCases := []struct {
		name              string
		hyperNode         *topologyv1alpha1.HyperNode
		memberName        string
		memberType        topologyv1alpha1.MemberType
		expectedHyperNode *topologyv1alpha1.HyperNode
	}{
		{
			name: "append node member",
			hyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "default"),
					},
				},
			},
			memberName: "node1",
			memberType: topologyv1alpha1.MemberTypeNode,
			expectedHyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "default"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
					},
				},
			},
		},
		{
			name: "append hyperNode member",
			hyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "default"),
					},
				},
			},
			memberName: "hypernode1",
			memberType: topologyv1alpha1.MemberTypeHyperNode,
			expectedHyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "default"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode1"),
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		AppendHyperNodeMember(tc.hyperNode, tc.memberName, tc.memberType)
		assert.Equal(t, tc.expectedHyperNode, tc.hyperNode, tc.name)
	}
}

func TestSortHyperNodeMembers(t *testing.T) {
	testCases := []struct {
		name              string
		hyperNode         *topologyv1alpha1.HyperNode
		expectedHyperNode *topologyv1alpha1.HyperNode
	}{
		{
			name: "sort node memberTypes",
			hyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node3"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node4"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node2"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node5"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
					},
				},
			},
			expectedHyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node2"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node3"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node4"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node5"),
					},
				},
			},
		},
		{
			name: "sort hypernode memberTypes",
			hyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode3"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode4"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode2"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode5"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode1"),
					},
				},
			},
			expectedHyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode1"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode2"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode3"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode4"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode5"),
					},
				},
			},
		},
		{
			name: "sort mix memberTypes",
			hyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node3"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode2"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node2"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode1"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
					},
				},
			},
			expectedHyperNode: &topologyv1alpha1.HyperNode{
				Spec: topologyv1alpha1.HyperNodeSpec{
					Members: []topologyv1alpha1.MemberSpec{
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node2"),
						newMemberSpec(topologyv1alpha1.MemberTypeNode, "node3"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode1"),
						newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "hypernode2"),
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		SortHyperNodeMembers(tc.hyperNode)
		assert.Equal(t, tc.expectedHyperNode, tc.hyperNode, tc.name)
	}
}

func TestParseDesiredHyperNodesFromNode(t *testing.T) {
	testCases := []struct {
		name                      string
		node                      *corev1.Node
		topologyName              string
		topology                  []string
		desiredHyperNodes         map[string]*topologyv1alpha1.HyperNode
		expectedDesiredHyperNodes map[string]*topologyv1alpha1.HyperNode
	}{
		{
			name: "case1 complete-topology",
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"key1": "value1",
						"key2": "value2",
						"key3": "value3",
					},
				},
			},
			topologyName:      "case1",
			topology:          []string{"key1", "key2", "key3"},
			desiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{},
			expectedDesiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{
				"case1-t1-value1-5fd8854b9c": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t1-value1-5fd8854b9c",
						Labels: map[string]string{
							"key1":                 "value1",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 1,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
						},
					},
				},
				"case1-t2-value2-5f784ccd44": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t2-value2-5f784ccd44",
						Labels: map[string]string{
							"key2":                 "value2",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 2,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "case1-t1-value1-5fd8854b9c"),
						},
					},
				},
				"case1-t3-value3-5f94d9985f": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t3-value3-5f94d9985f",
						Labels: map[string]string{
							"key3":                 "value3",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 3,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "case1-t2-value2-5f784ccd44"),
						},
					},
				},
			},
		},
		{
			name: "case2 missing-tier1-label",
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"key2": "value2",
						"key3": "value3",
					},
				},
			},
			topologyName:      "case1",
			topology:          []string{"key1", "key2", "key3"},
			desiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{},
			expectedDesiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{
				"case1-t2-value2-5f784ccd44": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t2-value2-5f784ccd44",
						Labels: map[string]string{
							"key2":                 "value2",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 2,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
						},
					},
				},
				"case1-t3-value3-5f94d9985f": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t3-value3-5f94d9985f",
						Labels: map[string]string{
							"key3":                 "value3",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 3,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeHyperNode, "case1-t2-value2-5f784ccd44"),
						},
					},
				},
			},
		},

		{
			name: "case3 missing-tier2-label",
			node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "node1",
					Labels: map[string]string{
						"key1": "value1",
						"key3": "value3",
					},
				},
			},
			topologyName:      "case1",
			topology:          []string{"key1", "key2", "key3"},
			desiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{},
			expectedDesiredHyperNodes: map[string]*topologyv1alpha1.HyperNode{
				"case1-t1-value1-5fd8854b9c": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t1-value1-5fd8854b9c",
						Labels: map[string]string{
							"key1":                 "value1",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 1,
						Members: []topologyv1alpha1.MemberSpec{
							newMemberSpec(topologyv1alpha1.MemberTypeNode, "node1"),
						},
					},
				},

				"case1-t3-value3-5f94d9985f": {
					ObjectMeta: v1.ObjectMeta{
						Name: "case1-t3-value3-5f94d9985f",
						Labels: map[string]string{
							"key3":                 "value3",
							LabelBasedTopologyName: "case1",
						},
					},
					Spec: topologyv1alpha1.HyperNodeSpec{
						Tier: 3,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		actualHyperNode := make(map[string]*topologyv1alpha1.HyperNode)
		ParseDesiredHyperNodesFromNode(tc.node, tc.topologyName, tc.topology, actualHyperNode)
		assert.Equal(t, tc.expectedDesiredHyperNodes, actualHyperNode, tc.name)
	}
}

func TestGetHyperNodeName(t *testing.T) {
	testCases := []struct {
		name         string
		topologyName string
		labelValue   string
		tier         int
		expected     string
	}{
		{
			name:         "labelValue with uppercase letter",
			topologyName: "case1",
			labelValue:   "Value",
			tier:         1,
			expected:     "case1-t1-value-7955599494",
		},

		{
			name:         "labelValue with consecutive hyphens",
			topologyName: "case2",
			labelValue:   "value--value",
			tier:         2,
			expected:     "case2-t2-value-value-5f756b4d85",
		},
	}

	for _, tc := range testCases {
		actualHyperNode := GetHyperNodeName(tc.topologyName, tc.labelValue, tc.tier)
		assert.Equal(t, tc.expected, actualHyperNode, tc.name)
	}
}
