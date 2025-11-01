/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2024 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced test coverage for resource information handling

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
	"math"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewResource(t *testing.T) {
	tests := []struct {
		resourceList v1.ResourceList
		expected     *Resource
	}{
		{
			resourceList: map[v1.ResourceName]resource.Quantity{},
			expected:     &Resource{},
		},
		{
			resourceList: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:                      *resource.NewScaledQuantity(4, -3),
				v1.ResourceMemory:                   *resource.NewQuantity(2000, resource.BinarySI),
				"scalar.test/" + "scalar1":          *resource.NewQuantity(1, resource.DecimalSI),
				v1.ResourceHugePagesPrefix + "test": *resource.NewQuantity(2, resource.BinarySI),
				v1.ResourceEphemeralStorage:         *resource.NewQuantity(3, resource.BinarySI),
			},
			expected: &Resource{
				MilliCPU: 4,
				Memory:   2000,
				ScalarResources: map[v1.ResourceName]float64{
					"scalar.test/scalar1":       1000,
					"hugepages-test":            2000,
					v1.ResourceEphemeralStorage: 3000},
			},
		},
	}

	for _, test := range tests {
		r := NewResource(test.resourceList)
		if !equality.Semantic.DeepEqual(test.expected, r) {
			t.Errorf("expected: %#v, got: %#v", test.expected, r)
		}
	}
}

func TestResourceAddScalar(t *testing.T) {
	tests := []struct {
		resource       *Resource
		scalarName     v1.ResourceName
		scalarQuantity float64
		expected       *Resource
	}{
		{
			resource:       &Resource{},
			scalarName:     "scalar1",
			scalarQuantity: 100,
			expected: &Resource{
				ScalarResources: map[v1.ResourceName]float64{"scalar1": 100},
			},
		},
		{
			resource: &Resource{
				MilliCPU:        4000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2},
			},
			scalarName:     "scalar2",
			scalarQuantity: 200,
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2, "scalar2": 200},
			},
		},
	}

	for _, test := range tests {
		test.resource.AddScalar(test.scalarName, test.scalarQuantity)
		if !equality.Semantic.DeepEqual(test.expected, test.resource) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource)
		}
	}
}

func TestSetMaxResource(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  *Resource
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4, "hugepages-test": 5},
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4, "hugepages-test": 5},
			},
		},
	}

	for _, test := range tests {
		test.resource1.SetMaxResource(test.resource2)
		if !equality.Semantic.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}

func TestScaleResourcesWithRatios(t *testing.T) {
	tests := []struct {
		name         string
		inputRatio   map[string]float64
		defaultRatio float64
		resource     *Resource
		expected     *Resource
	}{
		{
			name: "scale with ratio",
			inputRatio: map[string]float64{
				"overcommit-factor": 1.2,
				"cpu":               1.5,
				"memory":            1.5,
				"ephemeral-storage": 1.2,
				"nvidia.com/gpu":    1.0,
			},
			resource: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"ephemeral-storage": 1000, "nvidia.com/gpu": 8},
			},
			defaultRatio: 1.2,
			expected: &Resource{
				MilliCPU:        6000,
				Memory:          3000,
				ScalarResources: map[v1.ResourceName]float64{"ephemeral-storage": 1200, "nvidia.com/gpu": 8},
			},
		},
		{
			name:       "scale with default ratio",
			inputRatio: map[string]float64{},
			resource: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"ephemeral-storage": 1000, "nvidia.com/gpu": 8},
			},
			defaultRatio: 1.5,
			expected: &Resource{
				MilliCPU:        6000,
				Memory:          3000,
				ScalarResources: map[v1.ResourceName]float64{"ephemeral-storage": 1500, "nvidia.com/gpu": 12},
			},
		},
	}

	for _, test := range tests {
		outputResource := test.resource.ScaleResourcesWithRatios(test.inputRatio, test.defaultRatio)
		if !equality.Semantic.DeepEqual(test.expected, outputResource) {
			t.Errorf("expected: %#v, got: %#v", test.expected, outputResource)
		}
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		resource     *Resource
		resourceName v1.ResourceName
		expected     bool
	}{
		{
			resource:     &Resource{},
			resourceName: "cpu",
			expected:     true,
		},
		{
			resource: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4, "hugepages-test": 5},
			},
			resourceName: "cpu",
			expected:     false,
		},
		{
			resource: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 5},
			},
			resourceName: "scalar.test/scalar1",
			expected:     true,
		},
	}

	for _, test := range tests {
		flag := test.resource.IsZero(test.resourceName)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
}

func TestAddResource(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  *Resource
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4, "hugepages-test": 5},
			},
			expected: &Resource{
				MilliCPU:        8000,
				Memory:          6000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 5, "hugepages-test": 7},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1},
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4, "hugepages-test": 5},
			},
			expected: &Resource{
				MilliCPU:        8000,
				Memory:          6000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 5, "hugepages-test": 5},
			},
		},
	}

	for _, test := range tests {
		test.resource1.Add(test.resource2)
		if !equality.Semantic.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}

func TestSubResource(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  *Resource
	}{
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 500, "hugepages-test": 1000},
			},
			expected: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 500, "hugepages-test": 1000},
			},
		},
	}

	for _, test := range tests {
		test.resource1.Sub(test.resource2)
		if !equality.Semantic.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}

func TestDiff(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1         *Resource
		resource2         *Resource
		expectedIncreased *Resource
		expectedDecreased *Resource
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expectedIncreased: &Resource{
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
			expectedDecreased: &Resource{
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
		{
			resource1: &Resource{
				MilliCPU: 1000,
				Memory:   2000,
			},
			resource2: &Resource{},
			expectedIncreased: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
			expectedDecreased: &Resource{
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU: 1000,
				Memory:   2000,
			},
			expectedIncreased: &Resource{
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
			expectedDecreased: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			resource2: &Resource{
				MilliCPU: 2000,
				Memory:   1000,
			},
			expectedIncreased: &Resource{
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			expectedDecreased: &Resource{
				MilliCPU:        1000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
		{
			resource1: &Resource{
				MilliCPU: 2000,
				Memory:   1000,
			},
			resource2: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			expectedIncreased: &Resource{
				MilliCPU:        1000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
			expectedDecreased: &Resource{
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 3000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			expectedIncreased: &Resource{
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 2000},
			},
			expectedDecreased: &Resource{
				MilliCPU:        1000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
	}

	testsForDefaultInfinity := []struct {
		resource1         *Resource
		resource2         *Resource
		expectedIncreased *Resource
		expectedDecreased *Resource
	}{
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			resource2: &Resource{
				MilliCPU: 2000,
				Memory:   1000,
			},
			expectedIncreased: &Resource{
				Memory:          1000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
			expectedDecreased: &Resource{
				MilliCPU:        1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": -1},
			},
		},
		{
			resource1: &Resource{
				MilliCPU: 2000,
				Memory:   1000,
			},
			resource2: &Resource{
				MilliCPU:        1000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			expectedIncreased: &Resource{
				MilliCPU:        1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": -1},
			},
			expectedDecreased: &Resource{
				Memory:          1000,
				ScalarResources: make(map[v1.ResourceName]float64, 0),
			},
		},
	}

	for _, test := range testsForDefaultZero {
		increased, decreased := test.resource1.Diff(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expectedIncreased, increased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedIncreased, increased)
		}
		if !equality.Semantic.DeepEqual(test.expectedDecreased, decreased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedDecreased, decreased)
		}
	}
	for _, test := range testsForDefaultInfinity {
		increased, decreased := test.resource1.Diff(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expectedIncreased, increased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedIncreased, increased)
		}
		if !equality.Semantic.DeepEqual(test.expectedDecreased, decreased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedDecreased, decreased)
		}
	}
}

func TestLess(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 5000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        9000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: false,
		},
	}

	testsForDefaultInfinity := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 3000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 1000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        1000,
				Memory:          1000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
	}

	for caseID, test := range testsForDefaultZero {
		flag := test.resource1.Less(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
	for caseID, test := range testsForDefaultInfinity {
		flag := test.resource1.Less(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
}

func TestLessEqual(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  true,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 5000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        9000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: false,
		},
	}

	testsForDefaultInfinity := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  true,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{
				MilliCPU: 4000,
				Memory:   2000,
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU: 4000,
				Memory:   2000,
			},
			expected: true,
		},
	}

	for _, test := range testsForDefaultZero {
		flag := test.resource1.LessEqual(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
	for caseID, test := range testsForDefaultInfinity {
		flag := test.resource1.LessEqual(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
}

func TestLessEqualWithDimensionAndResourcesName(t *testing.T) {
	tests := []struct {
		resource1             *Resource
		resource2             *Resource
		req                   *Resource
		expectedFlag          bool
		expectedResourceNames []string
	}{
		{
			resource1:             &Resource{},
			resource2:             &Resource{},
			req:                   nil,
			expectedFlag:          true,
			expectedResourceNames: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU: 5000,
				Memory:   4000,
			},
			resource2:             &Resource{},
			req:                   nil,
			expectedFlag:          false,
			expectedResourceNames: []string{"cpu", "memory"},
		},
		{
			resource1: &Resource{MilliCPU: 5000},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000},
			},
			req:                   &Resource{MilliCPU: 1000},
			expectedFlag:          false,
			expectedResourceNames: []string{"cpu"},
		},
		{
			resource1:             &Resource{MilliCPU: 3000, Memory: 3000},
			resource2:             &Resource{MilliCPU: 4000, Memory: 2000},
			req:                   &Resource{Memory: 1000},
			expectedFlag:          false,
			expectedResourceNames: []string{"memory"},
		},
		{
			resource1: &Resource{
				MilliCPU: 4,
				Memory:   4000,
			},
			resource2:             &Resource{},
			req:                   &Resource{},
			expectedFlag:          true,
			expectedResourceNames: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU: 4, Memory: 4000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 1},
			},
			resource2: &Resource{MilliCPU: 8, Memory: 8000},
			req: &Resource{
				MilliCPU: 4, Memory: 2000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 1},
			},
			expectedFlag:          false,
			expectedResourceNames: []string{"nvidia.com/gpu"},
		},
		{
			resource1: &Resource{
				MilliCPU: 10, Memory: 4000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 1},
			},
			resource2: &Resource{
				MilliCPU: 100, Memory: 8000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/A100": 1},
			},
			req: &Resource{
				MilliCPU: 10, Memory: 4000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 0, "nvidia.com/A100": 1, "scalar": 1},
			},
			expectedFlag:          true,
			expectedResourceNames: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU: 110, Memory: 4000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 1, "nvidia.com/A100": 1, "scalar": 1},
			},
			resource2: &Resource{
				MilliCPU: 100, Memory: 8000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/A100": 1, "scalar": 1},
			},
			req: &Resource{
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 0, "nvidia.com/A100": 1, "scalar": 1},
			},
			expectedFlag:          true,
			expectedResourceNames: []string{},
		},
	}

	for i, test := range tests {
		flag, resourceNames := test.resource1.LessEqualWithDimensionAndResourcesName(test.resource2, test.req)
		if !equality.Semantic.DeepEqual(test.expectedFlag, flag) {
			t.Errorf("Case %v: expected: %#v, got: %#v", i, test.expectedFlag, flag)
		}
		if !equality.Semantic.DeepEqual(test.expectedResourceNames, resourceNames) {
			t.Errorf("Case %v: expected: %#v, got: %#v", i, test.expectedResourceNames, resourceNames)
		}
	}
}

func TestLessPartly(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 2000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			expected: false,
		},
	}

	testsForDefaultInfinity := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			expected: true,
		},
	}

	for caseID, test := range testsForDefaultZero {
		flag := test.resource1.LessPartly(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
	for _, test := range testsForDefaultInfinity {
		flag := test.resource1.LessPartly(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
}

func TestLessEqualPartly(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  true,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  false,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 2000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			expected: true,
		},
	}

	testsForDefaultInfinity := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			expected: true,
		},
	}

	for _, test := range testsForDefaultZero {
		flag := test.resource1.LessEqualPartly(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
	for _, test := range testsForDefaultInfinity {
		flag := test.resource1.LessEqualPartly(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  bool
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0},
			},
			expected: true,
		},
		{
			resource1: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"hugepages-test": 2000},
			},
			expected: true,
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        2000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		flag := test.resource1.Equal(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
}

func TestMinDimensionResourceZero(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  *Resource
	}{
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 0},
			},
			expected: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 0},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        5000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 3000},
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 2000},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0},
			},
			expected: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 0},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU: math.MaxFloat64,
				Memory:   2000,
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 0},
			},
		},
	}

	for _, test := range tests {
		test.resource1.MinDimensionResource(test.resource2, Zero)
		if !equality.Semantic.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}

func TestMinDimensionResourceInfinity(t *testing.T) {
	tests := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  *Resource
	}{
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1, "hugepages-test": 2},
			},
			resource2: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0},
			},
			expected: &Resource{
				MilliCPU:        3000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 0, "hugepages-test": 2},
			},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU: math.MaxFloat64,
				Memory:   2000,
			},
			expected: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
		},
	}

	for _, test := range tests {
		test.resource1.MinDimensionResource(test.resource2, Infinity)
		if !equality.Semantic.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}

func TestResource_LessEqualResource(t *testing.T) {
	testsForDefaultZero := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  []string
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  []string{},
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  []string{"cpu", "memory", "scalar.test/scalar1", "hugepages-test"},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 5000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: []string{"scalar.test/scalar1"},
		},
		{
			resource1: &Resource{
				MilliCPU:        9000,
				Memory:          4000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{
				MilliCPU:        8000,
				Memory:          8000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 4000, "hugepages-test": 5000},
			},
			expected: []string{"cpu"},
		},
	}

	testsForDefaultInfinity := []struct {
		resource1 *Resource
		resource2 *Resource
		expected  []string
	}{
		{
			resource1: &Resource{},
			resource2: &Resource{},
			expected:  []string{},
		},
		{
			resource1: &Resource{},
			resource2: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			expected: []string{},
		},
		{
			resource1: &Resource{
				MilliCPU:        4000,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
			resource2: &Resource{},
			expected:  []string{"cpu", "memory"},
		},
	}

	for _, test := range testsForDefaultZero {
		_, reason := test.resource1.LessEqualWithResourcesName(test.resource2, Zero)
		sort.Strings(test.expected)
		sort.Strings(reason)
		if !equality.Semantic.DeepEqual(test.expected, reason) {
			t.Errorf("expected: %#v, got: %#v", test.expected, reason)
		}
	}
	for caseID, test := range testsForDefaultInfinity {
		_, reason := test.resource1.LessEqualWithResourcesName(test.resource2, Infinity)
		sort.Strings(test.expected)
		sort.Strings(reason)
		if !equality.Semantic.DeepEqual(test.expected, reason) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, reason)
		}
	}
}
