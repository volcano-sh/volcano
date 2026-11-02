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

package rescheduling

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildNodeUtilization(name string, cpu, memory float64, unschedulable bool) *NodeUtilization {
	node := util.BuildNode(name, api.BuildResourceList("4", "8Gi"), make(map[string]string))
	node.Spec.Unschedulable = unschedulable
	return &NodeUtilization{
		nodeInfo: node,
		utilization: map[v1.ResourceName]float64{
			v1.ResourceCPU:    cpu,
			v1.ResourceMemory: memory,
		},
	}
}

func TestLowNodeUtilizationConfParse(t *testing.T) {
	defaultThresholds := map[string]float64{"cpu": 100, "memory": 100, "pods": 100}

	tests := []struct {
		name                     string
		configs                  map[string]interface{}
		expectedThresholds       map[string]float64
		expectedTargetThresholds map[string]float64
	}{
		{
			name:                     "empty config keeps defaults",
			configs:                  nil,
			expectedThresholds:       defaultThresholds,
			expectedTargetThresholds: defaultThresholds,
		},
		{
			name: "cpu, memory and pods are set",
			configs: map[string]interface{}{
				"thresholds":       map[interface{}]interface{}{"cpu": 20, "memory": 30, "pods": 40},
				"targetThresholds": map[interface{}]interface{}{"cpu": 50, "memory": 60, "pods": 70},
			},
			expectedThresholds:       map[string]float64{"cpu": 20, "memory": 30, "pods": 40},
			expectedTargetThresholds: map[string]float64{"cpu": 50, "memory": 60, "pods": 70},
		},
		{
			name: "partial config only overrides configured keys",
			configs: map[string]interface{}{
				"thresholds": map[interface{}]interface{}{"cpu": 20},
			},
			expectedThresholds:       map[string]float64{"cpu": 20, "memory": 100, "pods": 100},
			expectedTargetThresholds: defaultThresholds,
		},
		{
			name: "float value is accepted",
			configs: map[string]interface{}{
				"thresholds":       map[interface{}]interface{}{"cpu": 50.5, "memory": 20},
				"targetThresholds": map[interface{}]interface{}{"pods": 60.5},
			},
			expectedThresholds:       map[string]float64{"cpu": 50.5, "memory": 20, "pods": 100},
			expectedTargetThresholds: map[string]float64{"cpu": 100, "memory": 100, "pods": 60.5},
		},
		{
			name: "invalid key and value types are skipped without panic",
			configs: map[string]interface{}{
				"thresholds":       map[interface{}]interface{}{"cpu": "50", "memory": 30, 1: 40},
				"targetThresholds": map[interface{}]interface{}{"memory": true, "cpu": 60},
			},
			expectedThresholds:       map[string]float64{"cpu": 100, "memory": 30, "pods": 100},
			expectedTargetThresholds: map[string]float64{"cpu": 60, "memory": 100, "pods": 100},
		},
		{
			name: "pod key is not applied",
			configs: map[string]interface{}{
				"thresholds":       map[interface{}]interface{}{"pod": 20},
				"targetThresholds": map[interface{}]interface{}{"pod": 50},
			},
			expectedThresholds:       defaultThresholds,
			expectedTargetThresholds: defaultThresholds,
		},
		{
			name: "thresholds of unexpected type keeps defaults",
			configs: map[string]interface{}{
				"thresholds": map[string]interface{}{"cpu": 20},
			},
			expectedThresholds:       defaultThresholds,
			expectedTargetThresholds: defaultThresholds,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conf := NewLowNodeUtilizationConf()
			conf.parse(test.configs)
			if !reflect.DeepEqual(conf.Thresholds, test.expectedThresholds) {
				t.Errorf("expected thresholds %v, but got %v", test.expectedThresholds, conf.Thresholds)
			}
			if !reflect.DeepEqual(conf.TargetThresholds, test.expectedTargetThresholds) {
				t.Errorf("expected targetThresholds %v, but got %v", test.expectedTargetThresholds, conf.TargetThresholds)
			}
		})
	}
}

func TestLowThresholdFilter(t *testing.T) {
	config := LowNodeUtilizationConf{
		Thresholds:       map[string]float64{"cpu": 20, "memory": 20, "pods": 20},
		TargetThresholds: map[string]float64{"cpu": 50, "memory": 50, "pods": 50},
	}

	tests := []struct {
		name     string
		usage    *NodeUtilization
		config   interface{}
		expected bool
	}{
		{
			name:     "all resources below thresholds",
			usage:    buildNodeUtilization("n1", 10, 10, false),
			config:   config,
			expected: true,
		},
		{
			name:     "cpu at threshold",
			usage:    buildNodeUtilization("n1", 20, 10, false),
			config:   config,
			expected: false,
		},
		{
			name:     "all resources above thresholds",
			usage:    buildNodeUtilization("n1", 30, 30, false),
			config:   config,
			expected: false,
		},
		{
			name:     "memory above threshold",
			usage:    buildNodeUtilization("n1", 10, 30, false),
			config:   config,
			expected: false,
		},
		{
			name:     "unschedulable node is excluded",
			usage:    buildNodeUtilization("n1", 10, 10, true),
			config:   config,
			expected: false,
		},
		{
			name:     "invalid config type",
			usage:    buildNodeUtilization("n1", 10, 10, false),
			config:   &config,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := lowThresholdFilter(test.usage, test.config); got != test.expected {
				t.Errorf("expected %v, but got %v", test.expected, got)
			}
		})
	}
}

func TestHighThresholdFilter(t *testing.T) {
	config := LowNodeUtilizationConf{
		Thresholds:       map[string]float64{"cpu": 20, "memory": 20, "pods": 20},
		TargetThresholds: map[string]float64{"cpu": 50, "memory": 50, "pods": 50},
	}

	tests := []struct {
		name     string
		usage    *NodeUtilization
		config   interface{}
		expected bool
	}{
		{
			name:     "all resources below target thresholds",
			usage:    buildNodeUtilization("n1", 30, 30, false),
			config:   config,
			expected: false,
		},
		{
			name:     "all resources at target thresholds",
			usage:    buildNodeUtilization("n1", 50, 50, false),
			config:   config,
			expected: false,
		},
		{
			name:     "cpu above target threshold",
			usage:    buildNodeUtilization("n1", 60, 30, false),
			config:   config,
			expected: true,
		},
		{
			name:     "memory above target threshold",
			usage:    buildNodeUtilization("n1", 30, 60, false),
			config:   config,
			expected: true,
		},
		{
			name:     "invalid config type",
			usage:    buildNodeUtilization("n1", 60, 60, false),
			config:   &config,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := highThresholdFilter(test.usage, test.config); got != test.expected {
				t.Errorf("expected %v, but got %v", test.expected, got)
			}
		})
	}
}
