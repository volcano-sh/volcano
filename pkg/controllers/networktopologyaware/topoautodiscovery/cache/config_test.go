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
)

func TestNodeLabelBasedTopologiesValidate(t *testing.T) {
	testCases := []struct {
		name                     string
		nodeLabelBasedTopologies *NodeLabelBasedTopologies
		expectedErr              bool
	}{
		{
			name: "invalid topology name",
			nodeLabelBasedTopologies: &NodeLabelBasedTopologies{
				Topologies: []NodeLabelBasedTopology{
					{
						Name:          "InvalidName",
						TopoLevelKeys: []string{"volcano.io/tier1", "volcano.io/tier2"},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "invalid topology label key, include #",
			nodeLabelBasedTopologies: &NodeLabelBasedTopologies{
				Topologies: []NodeLabelBasedTopology{
					{
						Name:          "case1",
						TopoLevelKeys: []string{"volcano.io#/tier1", "volcano.io/Tier2"},
					},
				},
			},
			expectedErr: true,
		},
		{
			name: "valid topologies",
			nodeLabelBasedTopologies: &NodeLabelBasedTopologies{
				Topologies: []NodeLabelBasedTopology{
					{
						Name:          "case1",
						TopoLevelKeys: []string{"volcano.io/tier1", "volcano.io/Tier2"},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		actualErr := tc.nodeLabelBasedTopologies.Validate()
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name, actualErr)
	}
}
