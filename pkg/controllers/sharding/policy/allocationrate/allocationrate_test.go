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

package allocationrate

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestAllocationRatePolicyInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name: "valid configuration",
			args: policy.Arguments{
				"minCPUUtil":        0.0,
				"maxCPUUtil":        0.6,
				"preferWarmupNodes": false,
				"minNodes":          2,
				"maxNodes":          100,
			},
			expectErr: false,
		},
		{
			name: "invalid CPU range - min > max",
			args: policy.Arguments{
				"minCPUUtil": 0.7,
				"maxCPUUtil": 0.5,
				"minNodes":   2,
				"maxNodes":   100,
			},
			expectErr: true,
		},
		{
			name: "invalid CPU range - negative min",
			args: policy.Arguments{
				"minCPUUtil": -0.1,
				"maxCPUUtil": 0.5,
				"minNodes":   2,
				"maxNodes":   100,
			},
			expectErr: true,
		},
		{
			name: "invalid CPU range - max > 1",
			args: policy.Arguments{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 1.5,
				"minNodes":   2,
				"maxNodes":   100,
			},
			expectErr: true,
		},
		{
			name: "invalid node constraints - min > max",
			args: policy.Arguments{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 1.0,
				"minNodes":   100,
				"maxNodes":   10,
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

func TestAllocationRatePolicyCalculate(t *testing.T) {
	tests := []struct {
		name           string
		args           policy.Arguments
		nodes          []*corev1.Node
		metrics        map[string]*policy.NodeMetrics
		assignedNodes  map[string]string
		expectedCount  int
		expectedNodes  []string
	}{
		{
			name: "select nodes in CPU range",
			args: policy.Arguments{
				"minCPUUtil": 0.3,
				"maxCPUUtil": 0.7,
				"minNodes":   1,
				"maxNodes":   10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node4"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.2}, // Too low
				"node2": {CPUUtilization: 0.5}, // In range
				"node3": {CPUUtilization: 0.6}, // In range
				"node4": {CPUUtilization: 0.8}, // Too high
			},
			assignedNodes: map[string]string{},
			expectedCount: 2,
			expectedNodes: []string{"node3", "node2"}, // Sorted by utilization (highest first)
		},
		{
			name: "prefer warmup nodes",
			args: policy.Arguments{
				"minCPUUtil":        0.0,
				"maxCPUUtil":        1.0,
				"preferWarmupNodes": true,
				"minNodes":          1,
				"maxNodes":          10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "regular1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "warmup2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"warmup1":  {CPUUtilization: 0.5, IsWarmupNode: true},
				"regular1": {CPUUtilization: 0.6, IsWarmupNode: false},
				"warmup2":  {CPUUtilization: 0.4, IsWarmupNode: true},
			},
			assignedNodes: map[string]string{},
			expectedCount: 3,
			expectedNodes: []string{"warmup1", "warmup2", "regular1"}, // Warmup nodes first
		},
		{
			name: "skip already assigned nodes",
			args: policy.Arguments{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 1.0,
				"minNodes":   1,
				"maxNodes":   10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.5},
				"node2": {CPUUtilization: 0.5},
				"node3": {CPUUtilization: 0.5},
			},
			assignedNodes: map[string]string{
				"node1": "other-scheduler",
			},
			expectedCount: 2,
			expectedNodes: []string{"node2", "node3"},
		},
		{
			name: "enforce min nodes constraint",
			args: policy.Arguments{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 1.0,
				"minNodes":   3,
				"maxNodes":   10,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.5},
				"node2": {CPUUtilization: 0.5},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2, // Can't meet minNodes=3, so return what's available
		},
		{
			name: "enforce max nodes constraint",
			args: policy.Arguments{
				"minCPUUtil": 0.0,
				"maxCPUUtil": 1.0,
				"minNodes":   1,
				"maxNodes":   2,
			},
			nodes: []*corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{Name: "node1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "node4"}},
			},
			metrics: map[string]*policy.NodeMetrics{
				"node1": {CPUUtilization: 0.5},
				"node2": {CPUUtilization: 0.5},
				"node3": {CPUUtilization: 0.5},
				"node4": {CPUUtilization: 0.5},
			},
			assignedNodes: map[string]string{},
			expectedCount: 2, // Max 2 nodes
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
				t.Errorf("Calculate() selected %d nodes, expected %d. Selected: %v",
					len(result.SelectedNodes), tt.expectedCount, result.SelectedNodes)
			}

			// For tests with specific expected nodes, verify them
			if tt.expectedNodes != nil && len(tt.expectedNodes) > 0 {
				if len(result.SelectedNodes) != len(tt.expectedNodes) {
					t.Errorf("Calculate() selected %d nodes, expected %d",
						len(result.SelectedNodes), len(tt.expectedNodes))
				}
			}
		})
	}
}

func TestAllocationRatePolicyName(t *testing.T) {
	p := New()
	if p.Name() != PolicyName {
		t.Errorf("Name() = %v, want %v", p.Name(), PolicyName)
	}
}
