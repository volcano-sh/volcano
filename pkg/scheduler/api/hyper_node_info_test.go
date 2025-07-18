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
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestHyperNodesInfo_UpdateHyperNode_Normal(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s2, s0, s1}

	tests := []struct {
		name                         string
		initialHyperNodes            []*topologyv1alpha1.HyperNode
		expectedHyperNodesInfo       map[string]string
		expectedRealNodesSet         map[string]sets.Set[string]
		expectedHyperNodesListByTier map[int]sets.Set[string]
		ready                        bool
	}{
		{
			name:              "update successfully",
			initialHyperNodes: initialHyperNodes,
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, Parent: s2",
				"s1": "Name: s1, Tier: 1, Parent: s2",
				"s2": "Name: s2, Tier: 2, Parent: ",
			},
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-2", "node-3"),
				"s2": sets.New[string]("node-0", "node-1", "node-2", "node-3"),
			},
			expectedHyperNodesListByTier: map[int]sets.Set[string]{
				1: sets.New[string]("s0", "s1"),
				2: sets.New[string]("s2"),
			},
			ready: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := NewHyperNodesInfo(nodeLister)

			for _, node := range tt.initialHyperNodes {
				err := hni.UpdateHyperNode(node)
				assert.NoError(t, err)
			}

			actualHyperNodes := hni.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.expectedHyperNodesListByTier, hni.hyperNodesSetByTier)
			assert.Equal(t, tt.expectedRealNodesSet, hni.realNodesSet)
			assert.Equal(t, tt.ready, hni.Ready())
		})
	}
}

func TestHyperNodesInfo_UpdateHyperNode_WithCycle(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil}})
	s1 := BuildHyperNode("s1", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, selector, nil}})
	s11 := BuildHyperNode("s1", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s2 := BuildHyperNode("s2", 3, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s3 := BuildHyperNode("s3", 4, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s2, s0, s1, s3}

	tests := []struct {
		name                   string
		initialHyperNodes      []*topologyv1alpha1.HyperNode
		correctHyperNode       *topologyv1alpha1.HyperNode
		expectedHyperNodesInfo map[string]string
		expectedRealNodesSet   map[string]sets.Set[string]
		expectError            bool
	}{
		{
			name:              "cycle dependency and finally corrected",
			initialHyperNodes: initialHyperNodes,
			correctHyperNode:  s11,
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-0", "node-1"),
				"s2": sets.New[string]("node-0", "node-1"),
				"s3": sets.New[string]("node-0", "node-1"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, Parent: s1",
				"s1": "Name: s1, Tier: 2, Parent: s2",
				"s2": "Name: s2, Tier: 3, Parent: s3",
				"s3": "Name: s3, Tier: 4, Parent: ",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := NewHyperNodesInfo(nodeLister)

			errOccurred := false
			var focusedErr error
			for _, node := range tt.initialHyperNodes {
				err := hni.UpdateHyperNode(node)
				if err != nil {
					errOccurred = true
					focusedErr = err
				}
			}

			assert.Equal(t, false, hni.Ready())

			log.Println("begin update...")
			// update and resolve dependency.
			err := hni.UpdateHyperNode(s11)
			assert.NoError(t, err)
			actualHyperNodes := hni.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, tt.expectedRealNodesSet, hni.realNodesSet)
			assert.Equal(t, true, hni.Ready())

			if errOccurred && !tt.expectError {
				t.Errorf("expect no error but got err: %v", focusedErr)
			}
			if !errOccurred && tt.expectError {
				t.Errorf("expect error but got none")
			}
		})
	}
}

func TestHyperNodesInfo_UpdateHyperNode_MultipleParents(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{Name: "node-0", Type: topologyv1alpha1.MemberTypeNode, Selector: selector},
		{Name: "node-1", Type: topologyv1alpha1.MemberTypeNode, Selector: selector},
	})
	s1 := BuildHyperNode("s1", 2, []MemberConfig{
		{Name: "s0", Type: topologyv1alpha1.MemberTypeHyperNode, Selector: selector},
	})
	s2 := BuildHyperNode("s2", 3, []MemberConfig{
		{Name: "s0", Type: topologyv1alpha1.MemberTypeHyperNode, Selector: selector},
	})
	s22 := BuildHyperNode("s2", 3, []MemberConfig{
		{Name: "s1", Type: topologyv1alpha1.MemberTypeHyperNode, Selector: selector},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s0, s1, s2}

	tests := []struct {
		name                   string
		initialHyperNodes      []*topologyv1alpha1.HyperNode
		correctHyperNode       *topologyv1alpha1.HyperNode
		expectedRealNodesSet   map[string]sets.Set[string]
		expectedHyperNodesInfo map[string]string
		expectError            bool
	}{
		{
			name:              "multi parents",
			initialHyperNodes: initialHyperNodes,
			correctHyperNode:  s22,
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-0", "node-1"),
				"s2": sets.New[string]("node-0", "node-1"),
			},
			expectedHyperNodesInfo: map[string]string{
				"s0": "Name: s0, Tier: 1, Parent: s1",
				"s1": "Name: s1, Tier: 2, Parent: s2",
				"s2": "Name: s2, Tier: 3, Parent: ",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := NewHyperNodesInfo(nodeLister)

			errOccurred := false
			var focusedErr error
			for _, node := range tt.initialHyperNodes {
				err := hni.UpdateHyperNode(node)
				if err != nil {
					errOccurred = true
					focusedErr = err
				}
			}
			assert.Equal(t, false, hni.Ready())

			log.Println("begin update...")
			// update and resolve multi parents.
			err := hni.UpdateHyperNode(s22)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedRealNodesSet, hni.realNodesSet)
			actualHyperNodes := hni.HyperNodesInfo()
			assert.Equal(t, tt.expectedHyperNodesInfo, actualHyperNodes)
			assert.Equal(t, true, hni.Ready())

			if errOccurred && !tt.expectError {
				t.Errorf("expect no error but got err: %v", focusedErr)
			}
			if !errOccurred && tt.expectError {
				t.Errorf("expect error but got none")
			}
		})
	}
}

func TestHyperNodesInfo_GetRegexSelectorLeafHyperNodes(t *testing.T) {
	exactSelector := "exact"
	regexSelector := "regex"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s2 := BuildHyperNode("s2", 1, []MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
		{"node-5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s3 := BuildHyperNode("s3", 1, []MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s4 := BuildHyperNode("s4", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s5 := BuildHyperNode("s5", 2, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, regexSelector, nil},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	s6 := BuildHyperNode("s6", 3, []MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector, nil},
	})
	tests := []struct {
		name      string
		hyperNods []*topologyv1alpha1.HyperNode
		want      sets.Set[string]
	}{
		{
			name:      "get all leaf hyperNodes correctly",
			hyperNods: []*topologyv1alpha1.HyperNode{s0, s1, s2, s3, s4, s5, s6},
			want:      sets.New[string]("s1", "s3"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hni := NewHyperNodesInfo(nil)
			for _, hn := range tt.hyperNods {
				hni.hyperNodes[hn.Name] = NewHyperNodeInfo(hn)
			}
			assert.Equalf(t, tt.want, hni.GetRegexOrLabelMatchLeafHyperNodes(), "GetRegexSelcectorLeafHyperNodes()")
		})
	}
}

func TestHyperNodesInfo_GetLeafNodes(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s3 := BuildHyperNode("s3", 3, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s2, s3, s0, s1}

	tests := []struct {
		name              string
		hyperNodeName     string
		initialHyperNodes []*topologyv1alpha1.HyperNode
		expectedLeafNodes sets.Set[string]
	}{
		{
			name:              "Get correct leaf hyperNodes",
			hyperNodeName:     "s3",
			initialHyperNodes: initialHyperNodes,
			expectedLeafNodes: sets.New[string]("s0", "s1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := NewHyperNodesInfo(nodeLister)

			for _, node := range tt.initialHyperNodes {
				err := hni.UpdateHyperNode(node)
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedLeafNodes, hni.GetLeafNodes(tt.hyperNodeName))
		})
	}
}

func TestHyperNodesInfo_NodeRegexOrLabelMatchHyperNode(t *testing.T) {
	exactSelector := "exact"
	regexSelector := "regex"
	labelSelector := "label"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector, nil},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"^prefix", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
		{"node-2", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s2 := BuildHyperNode("s2", 1, []MemberConfig{
		{"^not-match-prefix", topologyv1alpha1.MemberTypeHyperNode, regexSelector, nil},
		{"node-3", topologyv1alpha1.MemberTypeNode, regexSelector, nil},
	})
	s3 := BuildHyperNode("s3", 1, []MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeHyperNode, regexSelector, nil},
	})
	s4 := BuildHyperNode("s4", 1, []MemberConfig{
		{"node-5", topologyv1alpha1.MemberTypeNode, labelSelector, &metav1.LabelSelector{
			MatchLabels: map[string]string{"role": "worker"},
		}},
	})
	s5 := BuildHyperNode("s5", 1, []MemberConfig{
		{"node-5", topologyv1alpha1.MemberTypeNode, labelSelector, &metav1.LabelSelector{
			MatchLabels: map[string]string{"role": "invalid-role"},
		}},
	})
	s6 := BuildHyperNode("s6", 1, []MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, labelSelector, &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "role",
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{"master"},
				},
			},
		}},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s2, s3, s0, s1, s4, s5, s6}

	n5 := buildNode("node-5", map[string]string{"role": "worker"}, nil)
	n6 := buildNode("node-6", map[string]string{"role": "master"}, nil)
	initialNodes := []*corev1.Node{n5, n6}

	tests := []struct {
		name          string
		nodeName      string
		hyperNodeName string
		expectedMatch bool
		expectedErr   bool
	}{
		{
			name:          "match",
			nodeName:      "node-5",
			hyperNodeName: "s4",
			expectedMatch: true,
			expectedErr:   false,
		},
		{
			name:          "not match",
			nodeName:      "node-0",
			hyperNodeName: "s0",
			expectedMatch: false,
			expectedErr:   false,
		},
		{
			name:          "not match",
			nodeName:      "node-1",
			hyperNodeName: "s0",
			expectedMatch: false,
			expectedErr:   false,
		},
		{
			name:          "match",
			nodeName:      "prefix-1",
			hyperNodeName: "s1",
			expectedMatch: true,
			expectedErr:   false,
		},
		{
			name:          "match",
			nodeName:      "node-2",
			hyperNodeName: "s1",
			expectedMatch: true,
			expectedErr:   false,
		},
		{
			name:          "not match",
			nodeName:      "not-match-prefix",
			hyperNodeName: "s2",
			expectedMatch: false,
			expectedErr:   false,
		},
		{
			name:          "not match",
			nodeName:      "node-4",
			hyperNodeName: "s3",
			expectedMatch: false,
			expectedErr:   false,
		},
		{
			name:          "not match",
			nodeName:      "node-5",
			hyperNodeName: "s5",
			expectedMatch: false,
			expectedErr:   false,
		},
		{
			name:          "match",
			nodeName:      "node-6",
			hyperNodeName: "s6",
			expectedMatch: true,
			expectedErr:   false,
		},
	}

	informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
	nodeInformer := informerFactory.Core().V1().Nodes()
	hni := NewHyperNodesInfo(nodeInformer.Lister())
	for _, node := range initialHyperNodes {
		err := hni.UpdateHyperNode(node)
		assert.NoError(t, err)
	}
	for _, node := range initialNodes {
		err := nodeInformer.Informer().GetStore().Add(node)
		assert.NoError(t, err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := hni.NodeRegexOrLabelMatchLeafHyperNode(tt.nodeName, tt.hyperNodeName)
			assert.Equal(t, tt.expectedMatch, match)
			assert.Equal(t, tt.expectedErr, err != nil)
		})
	}
}

func TestHyperNodesInfo_GetAncestors(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s3 := BuildHyperNode("s3", 3, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	s4 := BuildHyperNode("s4", 4, []MemberConfig{
		{"s3", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s0, s4, s1, s2, s3}

	tests := []struct {
		name              string
		hyperNodeName     string
		initialHyperNodes []*topologyv1alpha1.HyperNode
		expectedAncestors []string
	}{
		{
			name:              "Get ancestors for leaf node",
			hyperNodeName:     "s0",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: []string{"s0", "s2", "s3", "s4"},
		},
		{
			name:              "Get ancestors for intermediate node",
			hyperNodeName:     "s2",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: []string{"s2", "s3", "s4"},
		},
		{
			name:              "Get ancestors for root node",
			hyperNodeName:     "s4",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: []string{"s4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := NewHyperNodesInfo(nodeLister)

			for _, node := range tt.initialHyperNodes {
				err := hni.UpdateHyperNode(node)
				assert.NoError(t, err)
			}

			actualAncestors := hni.hyperNodes.GetAncestors(tt.hyperNodeName)
			assert.Equal(t, tt.expectedAncestors, actualAncestors)
		})
	}
}

func TestGetLCAHyperNode(t *testing.T) {
	selector := "exact"
	hnim := HyperNodeInfoMap{
		"s0": {parent: "s4", tier: 1, HyperNode: BuildHyperNode("s0", 1, []MemberConfig{
			{"node-0", topologyv1alpha1.MemberTypeNode, selector, nil},
			{"node-1", topologyv1alpha1.MemberTypeNode, selector, nil},
		})},
		"s1": {parent: "s4", tier: 1, HyperNode: BuildHyperNode("s1", 1, []MemberConfig{
			{"node-2", topologyv1alpha1.MemberTypeNode, selector, nil},
			{"node-3", topologyv1alpha1.MemberTypeNode, selector, nil},
		})},
		"s2": {parent: "s5", tier: 1, HyperNode: BuildHyperNode("s2", 1, []MemberConfig{
			{"node-4", topologyv1alpha1.MemberTypeNode, selector, nil},
			{"node-5", topologyv1alpha1.MemberTypeNode, selector, nil},
		})},
		"s3": {parent: "s5", tier: 1, HyperNode: BuildHyperNode("s3", 1, []MemberConfig{
			{"node-6", topologyv1alpha1.MemberTypeNode, selector, nil},
			{"node-7", topologyv1alpha1.MemberTypeNode, selector, nil},
		})},
		"s4": {parent: "s6", tier: 2, HyperNode: BuildHyperNode("s4", 2, []MemberConfig{
			{"s0", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
			{"s1", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		})},
		"s5": {parent: "s6", tier: 2, HyperNode: BuildHyperNode("s5", 2, []MemberConfig{
			{"s2", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
			{"s3", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		})},
		"s6": {parent: "", tier: 3, HyperNode: BuildHyperNode("s6", 3, []MemberConfig{
			{"s4", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
			{"s5", topologyv1alpha1.MemberTypeHyperNode, selector, nil},
		})},
		// s-orphan is an orphan hypernode that does not have parent
		"s-orphan": {parent: "", tier: 1, HyperNode: BuildHyperNode("s-orphan", 1, []MemberConfig{
			{"node-8", topologyv1alpha1.MemberTypeNode, selector, nil},
			{"node-9", topologyv1alpha1.MemberTypeNode, selector, nil},
		})},
	}

	tests := []struct {
		name         string
		hypernode    string
		jobHyperNode string
		expectedLCA  string
	}{
		{
			name:         "Sibling hypernode",
			hypernode:    "s0",
			jobHyperNode: "s1",
			expectedLCA:  "s4",
		},
		{
			name:         "No common ancestor",
			hypernode:    "s0",
			jobHyperNode: "s-orphan",
			expectedLCA:  "",
		},
		{
			name:         "One is ancestor of the other",
			hypernode:    "s4",
			jobHyperNode: "s0",
			expectedLCA:  "s4",
		},
		{
			name:         "Both are the same node",
			hypernode:    "node6",
			jobHyperNode: "node6",
			expectedLCA:  "node6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lca := hnim.GetLCAHyperNode(tt.hypernode, tt.jobHyperNode)
			assert.Equal(t, tt.expectedLCA, lca)
		})
	}
}

func TestGetMembers(t *testing.T) {
	type testCase struct {
		name     string
		selector topologyv1alpha1.MemberSelector
		nodes    []*corev1.Node
		expected sets.Set[string]
	}

	testCases := []testCase{
		{
			name: "Label match success with MatchLabels",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"role": "worker",
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string]("node1"),
		},
		{
			name: "Label match failure with MatchLabels",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"role": "invalid-role",
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string](),
		},
		{
			name: "Label selector is nil",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: nil,
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string](),
		},
		{
			name: "Label selector is not nil, but MatchLabels and MatchExpressions is empty",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: nil,
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string](),
		},
		{
			name: "MatchExpressions In operator match success",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "role",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"worker"},
						},
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string]("node1"),
		},
		{
			name: "MatchExpressions In operator match failure",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "role",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"invalid-role"},
						},
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string](),
		},
		{
			name: "MatchExpressions NotIn operator match success",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "role",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"master"},
						},
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"role": "master",
						},
					},
				},
			},
			expected: sets.New[string]("node1"),
		},
		{
			name: "MatchExpressions Exists operator match success",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "role",
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{},
					},
				},
			},
			expected: sets.New[string]("node1"),
		},
		{
			name: "MatchExpressions DoesNotExist operator match success",
			selector: topologyv1alpha1.MemberSelector{
				LabelMatch: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "role",
							Operator: metav1.LabelSelectorOpDoesNotExist,
						},
					},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"role": "worker",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "node2",
						Labels: map[string]string{},
					},
				},
			},
			expected: sets.New[string]("node2"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := GetMembers(tc.selector, tc.nodes)
			if !result.Equal(tc.expected) {
				t.Errorf("Test %s failed: Expected %v, but got %v", tc.name, tc.expected, result)
			}
		})
	}
}
