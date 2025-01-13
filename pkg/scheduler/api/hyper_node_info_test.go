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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	fakeclientset "k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestHyperNodesInfo_UpdateAncestors(t *testing.T) {
	selector := "exact"
	s0 := BuildHyperNode("s0", 1, topologyv1alpha1.MemberTypeNode, []string{"node-0", "node-1"}, selector)
	s1 := BuildHyperNode("s1", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s0", "s3"}, selector)
	s11 := BuildHyperNode("s1", 2, topologyv1alpha1.MemberTypeHyperNode, []string{"s0"}, selector)
	s2 := BuildHyperNode("s2", 3, topologyv1alpha1.MemberTypeHyperNode, []string{"s1"}, selector)
	s3 := BuildHyperNode("s3", 4, topologyv1alpha1.MemberTypeHyperNode, []string{"s2"}, selector)

	tests := []struct {
		name                 string
		hyperNodes           map[string]*HyperNodeInfo
		correctHyperNode     *topologyv1alpha1.HyperNode
		expectedRealNodesSet map[string]sets.Set[string]
		expectError          bool
	}{
		{
			name: "cycle dependency and finally corrected",
			hyperNodes: map[string]*HyperNodeInfo{
				"s0": NewHyperNodeInfo(s0, 1),
				"s1": NewHyperNodeInfo(s1, 2),
				"s2": NewHyperNodeInfo(s2, 3),
				"s3": NewHyperNodeInfo(s3, 4),
			},
			correctHyperNode: s11,
			expectedRealNodesSet: map[string]sets.Set[string]{
				"s0": sets.New[string]("node-0", "node-1"),
				"s1": sets.New[string]("node-0", "node-1"),
				"s2": sets.New[string]("node-0", "node-1"),
				"s3": sets.New[string]("node-0", "node-1"),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			informerFactory := informers.NewSharedInformerFactory(fakeclientset.NewClientset(), 0)
			nodeLister := informerFactory.Core().V1().Nodes().Lister()
			hni := &HyperNodesInfo{
				hyperNodes:   tt.hyperNodes,
				realNodesSet: make(map[string]sets.Set[string]),
				nodeLister:   nodeLister,
				parentMap:    make(map[string]string),
				ready:        new(atomic.Bool),
			}

			errOccurred := false
			var focusedErr error
			for node := range tt.hyperNodes {
				err := hni.updateAncestors(node)
				if err != nil {
					errOccurred = true
					focusedErr = err
				}
			}

			// update and resolve dependency.
			hni.hyperNodes["s1"] = NewHyperNodeInfo(s11, 2)
			err := hni.updateAncestors(s11.Name)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedRealNodesSet, hni.realNodesSet)

			if errOccurred && !tt.expectError {
				t.Errorf("expect no error but got err: %v", focusedErr)
			}
			if !errOccurred && tt.expectError {
				t.Errorf("expect error but got none")
			}
		})
	}
}
