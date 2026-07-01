/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added comprehensive test coverage for enhanced argument parsing functions

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

package framework

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

type GetIntTestCases struct {
	arg         Arguments
	key         string
	baseValue   int
	expectValue int
}

func TestArgumentsGetInt(t *testing.T) {
	key1 := "intkey"

	cases := []GetIntTestCases{
		{
			arg: Arguments{
				key1: 15,
			},
			key:         key1,
			baseValue:   10,
			expectValue: 15,
		},
		{
			arg: Arguments{
				key1: "errorvalue",
			},
			key:         key1,
			baseValue:   11,
			expectValue: 11,
		},
		{
			arg: Arguments{
				key1: "15",
			},
			key:         key1,
			baseValue:   10,
			expectValue: 15,
		},
		{
			arg: Arguments{
				key1: int64(15),
			},
			key:         key1,
			baseValue:   10,
			expectValue: 15,
		},
		{
			arg: Arguments{
				key1: float64(15),
			},
			key:         key1,
			baseValue:   10,
			expectValue: 15,
		},
		{
			arg: Arguments{
				key1: 15.5,
			},
			key:         key1,
			baseValue:   10,
			expectValue: 10,
		},
		{
			arg: Arguments{
				key1: "",
			},
			key:         key1,
			baseValue:   0,
			expectValue: 0,
		},
	}

	for index, c := range cases {
		baseValue := c.baseValue
		c.arg.GetInt(nil, c.key)
		c.arg.GetInt(&baseValue, c.key)
		if baseValue != c.expectValue {
			t.Errorf("index %d, value should be %v, but not %v", index, c.expectValue, baseValue)
		}
	}
}

func TestArgumentsGetFloat64(t *testing.T) {
	key1 := "float64key"

	cases := []struct {
		name        string
		arg         Arguments
		key         string
		baseValue   float64
		expectValue float64
	}{
		{
			name: "key not exist",
			arg: Arguments{
				"anotherKey": "12",
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 1.2,
		},
		{
			name: "key exist",
			arg: Arguments{
				key1: 1.5,
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 1.5,
		},
		{
			name: "value of key invalid",
			arg: Arguments{
				key1: "errorValue",
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 1.2,
		},
		{
			name: "value of key null",
			arg: Arguments{
				key1: "",
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 1.2,
		},
		{
			name: "int value",
			arg: Arguments{
				key1: 15,
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 15,
		},
		{
			name: "int64 value",
			arg: Arguments{
				key1: int64(15),
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 15,
		},
		{
			name: "string value",
			arg: Arguments{
				key1: "15.5",
			},
			key:         key1,
			baseValue:   1.2,
			expectValue: 15.5,
		},
	}

	for index, c := range cases {
		baseValue := c.baseValue
		c.arg.GetFloat64(&baseValue, c.key)
		if baseValue != c.expectValue {
			t.Errorf("index %d, case %s, value should be %v, but not %v", index, c.name, c.expectValue, baseValue)
		}
	}
}

func TestArgumentsGetArguments(t *testing.T) {
	tests := []struct {
		name     string
		arg      Arguments
		key      string
		expected Arguments
		ok       bool
	}{
		{
			name: "framework arguments",
			arg: Arguments{
				"section": Arguments{"key": "value"},
			},
			key:      "section",
			expected: Arguments{"key": "value"},
			ok:       true,
		},
		{
			name: "string interface map",
			arg: Arguments{
				"section": map[string]interface{}{"key": "value"},
			},
			key:      "section",
			expected: Arguments{"key": "value"},
			ok:       true,
		},
		{
			name: "interface map with string keys",
			arg: Arguments{
				"section": map[interface{}]interface{}{"key": "value"},
			},
			key:      "section",
			expected: Arguments{"key": "value"},
			ok:       true,
		},
		{
			name: "interface map with non-string key",
			arg: Arguments{
				"section": map[interface{}]interface{}{1: "value"},
			},
			key: "section",
			ok:  false,
		},
		{
			name: "missing key",
			arg:  Arguments{},
			key:  "section",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.arg.GetArguments(tt.key)
			if ok != tt.ok {
				t.Fatalf("GetArguments() ok = %v, expected %v", ok, tt.ok)
			}
			if !equality.Semantic.DeepEqual(got, tt.expected) {
				t.Fatalf("GetArguments() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestArgumentsGetQuantity(t *testing.T) {
	tests := []struct {
		name     string
		arg      Arguments
		key      string
		expected resource.Quantity
		ok       bool
	}{
		{
			name:     "cpu milli string",
			arg:      Arguments{"quantity": "500m"},
			key:      "quantity",
			expected: resource.MustParse("500m"),
			ok:       true,
		},
		{
			name:     "memory lowercase binary suffix",
			arg:      Arguments{"quantity": "300mi"},
			key:      "quantity",
			expected: resource.MustParse("300Mi"),
			ok:       true,
		},
		{
			name:     "numeric value",
			arg:      Arguments{"quantity": 500},
			key:      "quantity",
			expected: resource.MustParse("500"),
			ok:       true,
		},
		{
			name: "invalid string",
			arg:  Arguments{"quantity": "bad"},
			key:  "quantity",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.arg.GetQuantity(tt.key)
			if ok != tt.ok {
				t.Fatalf("GetQuantity() ok = %v, expected %v", ok, tt.ok)
			}
			if ok && got.Cmp(tt.expected) != 0 {
				t.Fatalf("GetQuantity() = %v, expected %v", got.String(), tt.expected.String())
			}
		})
	}
}

func TestArgumentsGetBool(t *testing.T) {
	key1 := "boolkey"

	cases := []struct {
		name        string
		arg         Arguments
		key         string
		baseValue   bool
		expectValue bool
	}{
		{
			name: "key not exist",
			arg: Arguments{
				"anotherKey": true,
			},
			key:         key1,
			baseValue:   false,
			expectValue: false,
		},
		{
			name: "bool value",
			arg: Arguments{
				key1: true,
			},
			key:         key1,
			baseValue:   false,
			expectValue: true,
		},
		{
			name: "string true value",
			arg: Arguments{
				key1: "true",
			},
			key:         key1,
			baseValue:   false,
			expectValue: true,
		},
		{
			name: "string false value",
			arg: Arguments{
				key1: "false",
			},
			key:         key1,
			baseValue:   true,
			expectValue: false,
		},
		{
			name: "invalid value",
			arg: Arguments{
				key1: "not-a-bool",
			},
			key:         key1,
			baseValue:   true,
			expectValue: true,
		},
	}

	for index, c := range cases {
		baseValue := c.baseValue
		c.arg.GetBool(&baseValue, c.key)
		if baseValue != c.expectValue {
			t.Errorf("index %d, case %s, value should be %v, but not %v", index, c.name, c.expectValue, baseValue)
		}
	}
}

func TestGetArgOfActionFromConf(t *testing.T) {
	cases := []struct {
		name              string
		configurations    []conf.Configuration
		action            string
		expectedArguments Arguments
	}{
		{
			name: "action exist in configurations",
			configurations: []conf.Configuration{
				{
					Name: "enqueue",
					Arguments: map[string]interface{}{
						"overCommitFactor": "1.5",
					},
				},
				{
					Name: "allocate",
					Arguments: map[string]interface{}{
						"placeholde": "placeholde",
					},
				},
			},
			action: "enqueue",
			expectedArguments: map[string]interface{}{
				"overCommitFactor": "1.5",
			},
		},
		{
			name: "action not exist in configurations",
			configurations: []conf.Configuration{
				{
					Name: "enqueue",
					Arguments: map[string]interface{}{
						"overCommitFactor": "1.5",
					},
				},
			},
			action:            "allocate",
			expectedArguments: nil,
		},
	}

	for index, c := range cases {
		arg := GetArgOfActionFromConf(c.configurations, c.action)
		if false == equality.Semantic.DeepEqual(arg, c.expectedArguments) {
			t.Errorf("index %d, case %s,expected %v, but got %v", index, c.name, c.expectedArguments, arg)
		}
	}
}

func TestArgumentsGetString(t *testing.T) {
	key1 := "stringkey"

	cases := []struct {
		name        string
		arg         Arguments
		key         string
		baseValue   string
		expectValue string
	}{
		{
			name: "key not exist",
			arg: Arguments{
				"anotherKey": "test",
			},
			key:         key1,
			baseValue:   "test1",
			expectValue: "test1",
		},
		{
			name: "key exist",
			arg: Arguments{
				key1: "test1",
			},
			key:         key1,
			baseValue:   "test",
			expectValue: "test1",
		},
		{
			name: "value of key invalid",
			arg: Arguments{
				key1: 0,
			},
			key:         key1,
			baseValue:   "test",
			expectValue: "test",
		},
		{
			name: "value of key null",
			arg: Arguments{
				key1: nil,
			},
			key:         key1,
			baseValue:   "test",
			expectValue: "test",
		},
	}

	for index, c := range cases {
		baseValue := c.baseValue
		c.arg.GetString(&baseValue, c.key)
		if baseValue != c.expectValue {
			t.Errorf("index %d, case %s, value should be %v, but not %v", index, c.name, c.expectValue, baseValue)
		}
	}
}
