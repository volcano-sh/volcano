/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added generic argument parsing support with automatic type conversion
- Enhanced argument handling to support multiple data types
- Added utility functions for reading action arguments from scheduler configuration

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
	"strconv"

	"github.com/mitchellh/mapstructure"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

// Arguments map
type Arguments map[string]interface{}

// GetInt get the integer value from string
func (a Arguments) GetInt(ptr *int, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok {
		return
	}

	value, ok := argv.(int)
	if !ok {
		klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
		return
	}

	*ptr = value
}

// GetFloat64 get the float64 value from string
func (a Arguments) GetFloat64(ptr *float64, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok {
		return
	}

	value, ok := argv.(float64)
	if !ok {
		if intVal, ok := argv.(int); ok {
			*ptr = float64(intVal)
			return
		}
		klog.Warningf("Could not parse argument: %v for key %s to float64", argv, key)
		return
	}

	*ptr = value
}

// GetBool get the bool value from string
func (a Arguments) GetBool(ptr *bool, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok {
		return
	}

	value, ok := argv.(bool)
	if !ok {
		if stringValue, ok := argv.(string); ok {
			parsed, err := strconv.ParseBool(stringValue)
			if err != nil {
				klog.Warningf("Could not parse argument: %v for key %s to bool", argv, key)
				return
			}
			*ptr = parsed
			return
		}
		klog.Warningf("Could not parse argument: %v for key %s to bool", argv, key)
		return
	}

	*ptr = value
}

// GetString get the string value from string
func (a Arguments) GetString(ptr *string, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok {
		return
	}

	value, ok := argv.(string)
	if !ok {
		klog.Warningf("Could not parse argument: %v for key %s to string", argv, key)
		return
	}

	*ptr = value
}

// Get can automatically convert parameters according to the passed generic T type.
// If the parameter conversion is successful, it returns the converted parameter.
// If the parameter does not exist, it returns false.
// If the conversion fails, it logs a warning and returns false, so that the
// caller keeps its own default instead of the scheduler exiting on a
// misconfigured argument.
func Get[T any](a Arguments, key string) (T, bool) {
	var result T
	argv, ok := a[key]
	if !ok {
		return result, false
	}

	err := mapstructure.Decode(argv, &result)
	if err != nil {
		klog.Warningf("Could not parse argument: %v for key %s to type %T: %v, the default value is used instead",
			argv, key, result, err)
		var zero T
		return zero, false
	}

	return result, true
}

// GetArgOfActionFromConf return argument of action reading from configuration of schedule
func GetArgOfActionFromConf(configurations []conf.Configuration, actionName string) Arguments {
	for _, c := range configurations {
		if c.Name == actionName {
			return c.Arguments
		}
	}

	return nil
}
