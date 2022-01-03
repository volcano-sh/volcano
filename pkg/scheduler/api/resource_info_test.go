/*
Copyright 2019 The Kubernetes Authors.

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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
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
			},
			expected: &Resource{
				MilliCPU:        4,
				Memory:          2000,
				ScalarResources: map[v1.ResourceName]float64{"scalar.test/scalar1": 1000, "hugepages-test": 2000},
			},
		},
	}

	for _, test := range tests {
		r := NewResource(test.resourceList)
		if !reflect.DeepEqual(test.expected, r) {
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
		if !reflect.DeepEqual(test.expected, test.resource) {
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
		if !reflect.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
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
		if !reflect.DeepEqual(test.expected, flag) {
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
		if !reflect.DeepEqual(test.expected, test.resource1) {
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
		if !reflect.DeepEqual(test.expected, test.resource1) {
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
		if !reflect.DeepEqual(test.expectedIncreased, increased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedIncreased, increased)
		}
		if !reflect.DeepEqual(test.expectedDecreased, decreased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedDecreased, decreased)
		}
	}
	for _, test := range testsForDefaultInfinity {
		increased, decreased := test.resource1.Diff(test.resource2, Infinity)
		if !reflect.DeepEqual(test.expectedIncreased, increased) {
			t.Errorf("expected: %#v, got: %#v", test.expectedIncreased, increased)
		}
		if !reflect.DeepEqual(test.expectedDecreased, decreased) {
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
			expected: true,
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
		if !reflect.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
	for caseID, test := range testsForDefaultInfinity {
		flag := test.resource1.Less(test.resource2, Infinity)
		if !reflect.DeepEqual(test.expected, flag) {
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
	}

	for _, test := range testsForDefaultZero {
		flag := test.resource1.LessEqual(test.resource2, Zero)
		if !reflect.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
	for caseID, test := range testsForDefaultInfinity {
		flag := test.resource1.LessEqual(test.resource2, Infinity)
		if !reflect.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
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
			expected: false,
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
		if !reflect.DeepEqual(test.expected, flag) {
			t.Errorf("caseID %d expected: %#v, got: %#v", caseID, test.expected, flag)
		}
	}
	for _, test := range testsForDefaultInfinity {
		flag := test.resource1.LessPartly(test.resource2, Infinity)
		if !reflect.DeepEqual(test.expected, flag) {
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
		if !reflect.DeepEqual(test.expected, flag) {
			t.Errorf("expected: %#v, got: %#v", test.expected, flag)
		}
	}
	for _, test := range testsForDefaultInfinity {
		flag := test.resource1.LessEqualPartly(test.resource2, Infinity)
		if !reflect.DeepEqual(test.expected, flag) {
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
		if !reflect.DeepEqual(test.expected, flag) {
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
		if !reflect.DeepEqual(test.expected, test.resource1) {
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
		if !reflect.DeepEqual(test.expected, test.resource1) {
			t.Errorf("expected: %#v, got: %#v", test.expected, test.resource1)
		}
	}
}
