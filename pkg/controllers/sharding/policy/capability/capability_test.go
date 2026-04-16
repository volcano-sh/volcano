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

package capability

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestCapabilityPolicyInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name: "valid configuration",
			args: policy.Arguments{
				"maxCapacityPercent": 0.30,
				"minNodes":           1,
				"maxNodes":           100,
			},
			expectErr: false,
		},
		{
			name: "invalid maxCapacityPercent - zero",
			args: policy.Arguments{
				"maxCapacityPercent": 0.0,
				"minNodes":           1,
				"maxNodes":           100,
			},
			expectErr: true,
		},
		{
			name: "invalid maxCapacityPercent - greater than 1",
			args: policy.Arguments{
				"maxCapacityPercent": 1.5,
				"minNodes":           1,
				"maxNodes":           100,
			},
			expectErr: true,
		},
		{
			name: "invalid node constraints",
			args: policy.Arguments{
				"maxCapacityPercent": 0.30,
				"minNodes":           100,
				"maxNodes":           10,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			err := p.Initialize(tt.args)
			if (err != nil) != tt.expectErr {
				t.Errorf("Initialize() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func TestCapabilityPolicyCalculate(t *testing.T) {
	tests := []struct {
		name          string
		args          policy.Arguments
		nodes         []*corev1.Node
		metrics       map[string]*policy.NodeMetrics
		assignedNodes map[string]string
		expectedCount int
		expectedNodes []string
		description   string
	}{
		{
			name: "select nodes with 30% capacity headroom",
			args: policy.Arguments{
				"maxCapacityPercent": 0.30,
				"minNodes":           1,
				"maxNodes":           10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node4"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.5},  // 0.5 <= 0.7, eligible
				"node2": {CPUUtilization: 0.65}, // 0.65 <= 0.7, eligible
				"node3": {CPUUtilization: 0.69}, // 0.69 <= 0.7, eligible
				"node4": {CPUUtilization: 0.75}, // 0.75 > 0.7, not eligible
			},
			assignedNodes: map[string]string{},
			expectedCount: 3,
			expectedNodes: []string{"node1", "node2", "node3"},
			description:   "Nodes with utilization <= 0.70 (1.0 - 0.30) are eligible, sorted by lowest util",
		},
		{
			name: "sort by lowest utilization",
			args: policy.Arguments{
				"maxCapacityPercent": 0.50,
				"minNodes":           1,
				"maxNodes":           2,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.4},  // Eligible, lower util
				"node2": {CPUUtilization: 0.3},  // Eligible, lowest util
				"node3": {CPUUtilization: 0.45}, // Eligible, higher util
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"node2", "node1"},
			description:   "Should select node2 and node1 (lowest utilization first)",
		},
		{
			name: "prefer warmup nodes",
			args: policy.Arguments{
				"maxCapacityPercent": 0.50,
				"preferWarmupNodes":  true,
				"minNodes":           1,
				"maxNodes":           10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.4, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.3, IsWarmupNode: false},
				"warmup2":  {CPUUtilization: 0.45, IsWarmupNode: true},
			},
			assignedNodes: map[string]string{},
			expectedCount: 3,
			// Warmup nodes sorted by lowest util first, then non-warmup
			expectedNodes: []string{"warmup1", "warmup2", "regular1"},
			description:   "Warmup nodes should be prioritized",
		},
		{
			name: "skip already assigned nodes",
			args: policy.Arguments{
				"maxCapacityPercent": 0.50,
				"minNodes":           1,
				"maxNodes":           10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.3},
				"node2": {CPUUtilization: 0.2},
				"node3": {CPUUtilization: 0.4},
			},
			assignedNodes: map[string]string{
				"node1": "other-scheduler",
			},
			expectedCount: 2,
			expectedNodes: []string{"node2", "node3"},
			description:   "Should skip node1 as it's already assigned",
		},
		{
			name: "enforce max nodes constraint",
			args: policy.Arguments{
				"maxCapacityPercent": 0.50,
				"minNodes":           1,
				"maxNodes":           2,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node4"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.3},
				"node2": {CPUUtilization: 0.1},
				"node3": {CPUUtilization: 0.2},
				"node4": {CPUUtilization: 0.4},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"node2", "node3"},
			description:   "Should respect maxNodes=2, selecting lowest utilization nodes",
		},
		{
			name: "no eligible nodes",
			args: policy.Arguments{
				"maxCapacityPercent": 0.20,
				"minNodes":           1,
				"maxNodes":           10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.9},  // Too high
				"node2": {CPUUtilization: 0.95}, // Too high
			},
			assignedNodes: map[string]string{},
			expectedCount: 0,
			description:   "No nodes have sufficient headroom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			err := p.Initialize(tt.args)
			if err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}

			ctx := &policy.PolicyContext{
				SchedulerName: "test-scheduler",
				SchedulerType: "volcano",
				AllNodes:      tt.nodes,
				NodeMetrics:   tt.metrics,
				AssignedNodes: tt.assignedNodes,
			}

			result, err := p.Calculate(ctx)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}

			if len(result.SelectedNodes) != tt.expectedCount {
				t.Errorf("%s: Calculate() selected %d nodes, expected %d. Selected: %v",
					tt.description, len(result.SelectedNodes), tt.expectedCount, result.SelectedNodes)
			}

			// For tests with specific expected nodes, verify exact match (order matters)
			if tt.expectedNodes != nil && len(tt.expectedNodes) > 0 {
				if len(result.SelectedNodes) != len(tt.expectedNodes) {
					t.Errorf("Calculate() selected %d nodes, expected %d",
						len(result.SelectedNodes), len(tt.expectedNodes))
				} else {
					for i := 0; i < len(tt.expectedNodes); i++ {
						if result.SelectedNodes[i] != tt.expectedNodes[i] {
							t.Errorf("Calculate() node[%d] = %s, expected %s. Got: %v, Expected: %v",
								i, result.SelectedNodes[i], tt.expectedNodes[i],
								result.SelectedNodes, tt.expectedNodes)
							break
						}
					}
				}
			}
		})
	}
}

func TestCapabilityPolicyName(t *testing.T) {
	p := New()
	if p.Name() != PolicyName {
		t.Errorf("Name() = %v, want %v", p.Name(), PolicyName)
	}
}
