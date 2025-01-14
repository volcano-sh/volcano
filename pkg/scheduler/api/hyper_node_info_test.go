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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestHyperNodesInfo_UpdateHyperNode_Normal(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, selector},
		{"node-3", topologyv1alpha1.MemberTypeNode, selector},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector},
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector},
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
		{"node-0", topologyv1alpha1.MemberTypeNode, selector},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector}})
	s1 := BuildHyperNode("s1", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, selector}})
	s11 := BuildHyperNode("s1", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	s2 := BuildHyperNode("s2", 3, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	s3 := BuildHyperNode("s3", 4, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector},
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
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, regexSelector},
		{"node-3", topologyv1alpha1.MemberTypeNode, regexSelector},
	})
	s2 := BuildHyperNode("s2", 1, []MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeNode, regexSelector},
		{"node-5", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
	})
	s3 := BuildHyperNode("s3", 1, []MemberConfig{
		{"node-6", topologyv1alpha1.MemberTypeNode, regexSelector},
		{"node-7", topologyv1alpha1.MemberTypeNode, exactSelector},
	})
	s4 := BuildHyperNode("s4", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
	})
	s5 := BuildHyperNode("s5", 2, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, regexSelector},
		{"s3", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
	})
	s6 := BuildHyperNode("s6", 3, []MemberConfig{
		{"s4", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
		{"s5", topologyv1alpha1.MemberTypeHyperNode, exactSelector},
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
			assert.Equalf(t, tt.want, hni.GetRegexSelectorLeafHyperNodes(), "GetRegexSelcectorLeafHyperNodes()")
		})
	}
}

func TestHyperNodesInfo_GetLeafNodes(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-2", topologyv1alpha1.MemberTypeNode, selector},
		{"node-3", topologyv1alpha1.MemberTypeNode, selector},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector},
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	s3 := BuildHyperNode("s3", 3, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector},
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

func TestHyperNodesInfo_NodeRegexMatchHyperNode(t *testing.T) {
	exactSelector := "exact"
	regexSelector := "regex"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, exactSelector},
		{"node-1", topologyv1alpha1.MemberTypeNode, exactSelector},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"^prefix", topologyv1alpha1.MemberTypeNode, regexSelector},
		{"node-2", topologyv1alpha1.MemberTypeNode, regexSelector},
	})
	s2 := BuildHyperNode("s2", 1, []MemberConfig{
		{"^not-match-prefix", topologyv1alpha1.MemberTypeHyperNode, regexSelector},
		{"node-3", topologyv1alpha1.MemberTypeNode, regexSelector},
	})
	s3 := BuildHyperNode("s3", 1, []MemberConfig{
		{"node-4", topologyv1alpha1.MemberTypeHyperNode, regexSelector},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s2, s3, s0, s1}

	tests := []struct {
		name          string
		nodeName      string
		hyperNodeName string
		expectedMatch bool
		expectedErr   bool
	}{
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
	}

	informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
	nodeLister := informerFactory.Core().V1().Nodes().Lister()
	hni := NewHyperNodesInfo(nodeLister)
	for _, node := range initialHyperNodes {
		err := hni.UpdateHyperNode(node)
		assert.NoError(t, err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := hni.NodeRegexMatchLeafHyperNode(tt.nodeName, tt.hyperNodeName)
			assert.Equal(t, tt.expectedMatch, match)
			assert.Equal(t, tt.expectedErr, err != nil)
		})
	}
}

func TestHyperNodesInfo_GetAncestors(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector},
	})
	s1 := BuildHyperNode("s1", 1, []MemberConfig{
		{"node-0", topologyv1alpha1.MemberTypeNode, selector},
		{"node-1", topologyv1alpha1.MemberTypeNode, selector},
	})
	s2 := BuildHyperNode("s2", 2, []MemberConfig{
		{"s0", topologyv1alpha1.MemberTypeHyperNode, selector},
		{"s1", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	s3 := BuildHyperNode("s3", 3, []MemberConfig{
		{"s2", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	s4 := BuildHyperNode("s4", 4, []MemberConfig{
		{"s3", topologyv1alpha1.MemberTypeHyperNode, selector},
	})
	initialHyperNodes := []*topologyv1alpha1.HyperNode{s0, s4, s1, s2, s3}

	tests := []struct {
		name              string
		hyperNodeName     string
		initialHyperNodes []*topologyv1alpha1.HyperNode
		expectedAncestors sets.Set[string]
	}{
		{
			name:              "Get ancestors for leaf node",
			hyperNodeName:     "s0",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: sets.New[string]("s0", "s2", "s3", "s4"),
		},
		{
			name:              "Get ancestors for intermediate node",
			hyperNodeName:     "s2",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: sets.New[string]("s2", "s3", "s4"),
		},
		{
			name:              "Get ancestors for root node",
			hyperNodeName:     "s4",
			initialHyperNodes: initialHyperNodes,
			expectedAncestors: sets.New[string]("s4"),
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

			actualAncestors := hni.GetAncestors(tt.hyperNodeName)
			assert.Equal(t, tt.expectedAncestors, actualAncestors)
		})
	}
}
