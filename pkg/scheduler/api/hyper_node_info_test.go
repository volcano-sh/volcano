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
	"fmt"
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
	s0 := BuildHyperNode("s0", 1, topologyv1alpha1.MemberTypeNode, []string{"node-0", "node-1"}, selector)
	s1 := BuildHyperNode("s1", 1, topologyv1alpha1.MemberTypeNode, []string{"node-2", "node-3"}, selector)
	s2 := BuildHyperNode("s2", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s1", "s0"}, selector)
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
	s0 := BuildHyperNode("s0", 1, topologyv1alpha1.MemberTypeNode, []string{"node-0", "node-1"}, selector)
	s1 := BuildHyperNode("s1", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s0", "s3"}, selector)
	s11 := BuildHyperNode("s1", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s0"}, selector)
	s2 := BuildHyperNode("s2", 3, topologyv1alpha1.MemberTypeHyperNode, []string{"s1"}, selector)
	s3 := BuildHyperNode("s3", 4, topologyv1alpha1.MemberTypeHyperNode, []string{"s2"}, selector)
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
			fmt.Println("begin updatexxx")
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
	s0 := BuildHyperNode("s0", 1, topologyv1alpha1.MemberTypeNode, []string{"node-0", "node-1"}, selector)
	s1 := BuildHyperNode("s1", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s0"}, selector)
	s2 := BuildHyperNode("s2", 3, topologyv1alpha1.MemberTypeHyperNode, []string{"s0"}, selector)
	s22 := BuildHyperNode("s2", 3, topologyv1alpha1.MemberTypeHyperNode, []string{"s1"}, selector)
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
