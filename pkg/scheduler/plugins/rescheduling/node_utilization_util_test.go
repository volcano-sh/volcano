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
	"math"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	eps = 1e-8
)

func TestGroupNodesByUtilization(t *testing.T) {
	config := LowNodeUtilizationConf{
		Thresholds:       map[string]float64{"cpu": 20, "memory": 20, "pods": 20},
		TargetThresholds: map[string]float64{"cpu": 50, "memory": 50, "pods": 50},
	}

	tests := []struct {
		name              string
		nodes             []*NodeUtilization
		expectedLowNodes  []string
		expectedHighNodes []string
	}{
		{
			name:              "no nodes",
			nodes:             []*NodeUtilization{},
			expectedLowNodes:  []string{},
			expectedHighNodes: []string{},
		},
		{
			name: "nodes are grouped into low and high",
			nodes: []*NodeUtilization{
				buildNodeUtilization("low", 10, 10, false),
				buildNodeUtilization("medium", 30, 30, false),
				buildNodeUtilization("high", 60, 30, false),
				buildNodeUtilization("unschedulable", 10, 10, true),
			},
			expectedLowNodes:  []string{"low"},
			expectedHighNodes: []string{"high"},
		},
		{
			name: "all nodes are low",
			nodes: []*NodeUtilization{
				buildNodeUtilization("low1", 10, 10, false),
				buildNodeUtilization("low2", 5, 15, false),
			},
			expectedLowNodes:  []string{"low1", "low2"},
			expectedHighNodes: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lowNodes, highNodes := groupNodesByUtilization(test.nodes, lowThresholdFilter, highThresholdFilter, config)
			if got := nodeNames(lowNodes); !reflect.DeepEqual(got, test.expectedLowNodes) {
				t.Errorf("expected low nodes %v, but got %v", test.expectedLowNodes, got)
			}
			if got := nodeNames(highNodes); !reflect.DeepEqual(got, test.expectedHighNodes) {
				t.Errorf("expected high nodes %v, but got %v", test.expectedHighNodes, got)
			}
		})
	}
}

func nodeNames(nodes []*NodeUtilization) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.nodeInfo.Name)
	}
	return names
}

func TestConvertPercentAndQuantity(t *testing.T) {
	nodeCapacity := api.BuildResourceList("4", "8Gi")

	tests := []struct {
		name             string
		resourceName     v1.ResourceName
		percent          float64
		expectedQuantity string
	}{
		{
			name:             "cpu 25 percent",
			resourceName:     v1.ResourceCPU,
			percent:          25,
			expectedQuantity: "1",
		},
		{
			name:             "cpu fractional percent",
			resourceName:     v1.ResourceCPU,
			percent:          12.5,
			expectedQuantity: "500m",
		},
		{
			name:             "memory 50 percent",
			resourceName:     v1.ResourceMemory,
			percent:          50,
			expectedQuantity: "4Gi",
		},
		{
			name:             "memory 0 percent",
			resourceName:     v1.ResourceMemory,
			percent:          0,
			expectedQuantity: "0",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			quantity := convertPercentToQuan(test.resourceName, test.percent, nodeCapacity)
			expected := resource.MustParse(test.expectedQuantity)
			if quantity.Cmp(expected) != 0 {
				t.Errorf("expected quantity %s, but got %s", expected.String(), quantity.String())
			}
			percent := convertQuanToPercent(test.resourceName, quantity, nodeCapacity)
			if math.Abs(percent-test.percent) > eps {
				t.Errorf("expected percent %v, but got %v", test.percent, percent)
			}
		})
	}
}
