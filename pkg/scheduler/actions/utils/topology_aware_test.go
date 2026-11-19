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

package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateNumCandidates(t *testing.T) {
	limits := CandidateLimits{
		MinCandidateNodesPercentage: 10,
		MinCandidateNodesAbsolute:   1,
		MaxCandidateNodesAbsolute:   100,
	}

	tests := []struct {
		name       string
		numNodes   int
		want       int
		percentage int
	}{
		{name: "percentage below absolute minimum", numNodes: 5, want: 1},
		{name: "percentage above absolute minimum", numNodes: 50, want: 5},
		{name: "capped by max absolute", numNodes: 2000, want: 100},
		{name: "capped by num nodes", numNodes: 3, want: 3, percentage: 100},
		{name: "zero nodes", numNodes: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLimits := limits
			if tt.percentage > 0 {
				testLimits.MinCandidateNodesPercentage = tt.percentage
			}
			assert.Equal(t, tt.want, CalculateNumCandidates(tt.numNodes, testLimits))
		})
	}
}

func TestGetOffsetAndNumCandidates(t *testing.T) {
	limits := CandidateLimits{
		MinCandidateNodesPercentage: 10,
		MinCandidateNodesAbsolute:   1,
		MaxCandidateNodesAbsolute:   100,
	}

	t.Run("zero nodes", func(t *testing.T) {
		offset, numCandidates := GetOffsetAndNumCandidates(0, limits)
		assert.Equal(t, 0, offset)
		assert.Equal(t, 0, numCandidates)
	})

	t.Run("valid range", func(t *testing.T) {
		offset, numCandidates := GetOffsetAndNumCandidates(20, limits)
		assert.GreaterOrEqual(t, offset, 0)
		assert.Less(t, offset, 20)
		assert.Equal(t, 2, numCandidates)
	})
}
