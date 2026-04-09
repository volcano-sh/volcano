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

package warmup

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestWarmupPolicyInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name: "valid configuration",
			args: policy.Arguments{
				"warmupLabel":      "node.volcano.sh/warmup",
				"warmupLabelValue": "true",
				"minNodes":         1,
				"maxNodes":         100,
				"allowNonWarmup":   true,
			},
			expectErr: false,
		},
		{
			name: "empty warmup label",
			args: policy.Arguments{
				"warmupLabel": "",
				"minNodes":    1,
				"maxNodes":    100,
			},
			expectErr: true,
		},
		{
			name: "invalid node constraints",
			args: policy.Arguments{
				"warmupLabel": "node.volcano.sh/warmup",
				"minNodes":    100,
				"maxNodes":    10,
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

func TestWarmupPolicyCalculate(t *testing.T) {
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
			name: "prioritize warmup nodes",
			args: policy.Arguments{
				"minNodes":       1,
				"maxNodes":       10,
				"allowNonWarmup": true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.5, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.3, IsWarmupNode: false},
				"warmup2":  {CPUUtilization: 0.4, IsWarmupNode: true},
				"regular2": {CPUUtilization: 0.2, IsWarmupNode: false},
			},
			assignedNodes: map[string]string{},
			expectedCount: 4,
			// Warmup sorted by lowest util first, then non-warmup sorted by lowest util first
			expectedNodes: []string{"warmup2", "warmup1", "regular2", "regular1"},
			description:   "Should select all nodes with warmup nodes first",
		},
		{
			name: "only warmup nodes when available",
			args: policy.Arguments{
				"minNodes":       2,
				"maxNodes":       2,
				"allowNonWarmup": true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.5, IsWarmupNode: true},
				"warmup2":  {CPUUtilization: 0.4, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.3, IsWarmupNode: false},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"warmup2", "warmup1"},
			description:   "Should select only warmup nodes when enough available",
		},
		{
			name: "fallback to non-warmup nodes",
			args: policy.Arguments{
				"minNodes":       3,
				"maxNodes":       10,
				"allowNonWarmup": true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.5, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.3, IsWarmupNode: false},
				"regular2": {CPUUtilization: 0.2, IsWarmupNode: false},
			},
			assignedNodes: map[string]string{},
			expectedCount: 3,
			expectedNodes: []string{"warmup1", "regular2", "regular1"},
			description:   "Should include non-warmup nodes to meet minNodes",
		},
		{
			name: "no fallback to non-warmup nodes",
			args: policy.Arguments{
				"minNodes":       3,
				"maxNodes":       10,
				"allowNonWarmup": false,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.5, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.3, IsWarmupNode: false},
				"regular2": {CPUUtilization: 0.2, IsWarmupNode: false},
			},
			assignedNodes: map[string]string{},
			expectedCount: 1,
			expectedNodes: []string{"warmup1"},
			description:   "Should not include non-warmup nodes when allowNonWarmup=false",
		},
		{
			name: "sort by lowest utilization within warmup group",
			args: policy.Arguments{
				"minNodes":       2,
				"maxNodes":       2,
				"allowNonWarmup": false,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup3"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1": {CPUUtilization: 0.6, IsWarmupNode: true},
				"warmup2": {CPUUtilization: 0.3, IsWarmupNode: true},
				"warmup3": {CPUUtilization: 0.5, IsWarmupNode: true},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"warmup2", "warmup3"},
			description:   "Should select warmup nodes with lowest utilization",
		},
		{
			name: "skip already assigned nodes",
			args: policy.Arguments{
				"minNodes":       1,
				"maxNodes":       10,
				"allowNonWarmup": true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1": {CPUUtilization: 0.5, IsWarmupNode: true},
				"warmup2": {CPUUtilization: 0.4, IsWarmupNode: true},
			},
			assignedNodes: map[string]string{
				"warmup1": "other-scheduler",
			},
			expectedCount: 1,
			expectedNodes: []string{"warmup2"},
			description:   "Should skip already assigned nodes",
		},
		{
			name: "custom warmup label",
			args: policy.Arguments{
				"warmupLabel":      "node.example.com/pool",
				"warmupLabelValue": "warm",
				"minNodes":         1,
				"maxNodes":         10,
				"allowNonWarmup":   true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.3, Labels: map[string]string{"node.example.com/pool": "warm"}},
				"node2": {CPUUtilization: 0.2, Labels: map[string]string{"node.example.com/pool": "cold"}},
				"node3": {CPUUtilization: 0.4, Labels: map[string]string{"node.example.com/pool": "warm"}},
			},
			assignedNodes: map[string]string{},
			expectedCount: 3,
			// node1 and node3 match the custom label, node2 does not
			// warmup sorted by lowest util first (node1, node3), then non-warmup (node2)
			expectedNodes: []string{"node1", "node3", "node2"},
			description:   "Should classify warmup nodes using configured label instead of IsWarmupNode",
		},
		{
			name: "enforce max nodes constraint",
			args: policy.Arguments{
				"minNodes":       1,
				"maxNodes":       2,
				"allowNonWarmup": true,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup4"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1": {CPUUtilization: 0.4, IsWarmupNode: true},
				"warmup2": {CPUUtilization: 0.3, IsWarmupNode: true},
				"warmup3": {CPUUtilization: 0.5, IsWarmupNode: true},
				"warmup4": {CPUUtilization: 0.6, IsWarmupNode: true},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"warmup2", "warmup1"},
			description:   "Should respect maxNodes constraint",
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

func TestWarmupPolicyName(t *testing.T) {
	p := New()
	if p.Name() != PolicyName {
		t.Errorf("Name() = %v, want %v", p.Name(), PolicyName)
	}
}
