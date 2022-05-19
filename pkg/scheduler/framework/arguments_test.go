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

package framework

import (
	"reflect"
	"testing"

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
	}

	for index, c := range cases {
		baseValue := c.baseValue
		c.arg.GetFloat64(&baseValue, c.key)
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
		if false == reflect.DeepEqual(arg, c.expectedArguments) {
			t.Errorf("index %d, case %s,expected %v, but got %v", index, c.name, c.expectedArguments, arg)
		}
	}
}
