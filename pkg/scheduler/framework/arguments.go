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
	"strings"

	"github.com/mitchellh/mapstructure"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

// Arguments map
type Arguments map[string]interface{}

// GetInt get the integer value from arguments.
func (a Arguments) GetInt(ptr *int, key string) bool {
	if ptr == nil {
		return false
	}

	argv, ok := a[key]
	if !ok {
		return false
	}

	switch value := argv.(type) {
	case int:
		*ptr = value
		return true
	case int64:
		if int64(int(value)) != value {
			klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
			return false
		}
		*ptr = int(value)
		return true
	case float64:
		if value != float64(int(value)) {
			klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
			return false
		}
		*ptr = int(value)
		return true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
			return false
		}
		*ptr = parsed
		return true
	default:
		klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
		return false
	}
}

// GetFloat64 get the float64 value from arguments.
func (a Arguments) GetFloat64(ptr *float64, key string) bool {
	if ptr == nil {
		return false
	}

	argv, ok := a[key]
	if !ok {
		return false
	}

	switch value := argv.(type) {
	case float64:
		*ptr = value
		return true
	case int:
		*ptr = float64(value)
		return true
	case int64:
		*ptr = float64(value)
		return true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			klog.Warningf("Could not parse argument: %v for key %s to float64", argv, key)
			return false
		}
		*ptr = parsed
		return true
	default:
		klog.Warningf("Could not parse argument: %v for key %s to float64", argv, key)
		return false
	}
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

// GetArguments returns a nested Arguments value from arguments.
func (a Arguments) GetArguments(key string) (Arguments, bool) {
	argv, ok := a[key]
	if !ok {
		return nil, false
	}

	switch value := argv.(type) {
	case Arguments:
		return value, true
	case map[string]interface{}:
		return Arguments(value), true
	case map[interface{}]interface{}:
		result := Arguments{}
		for k, v := range value {
			keyStr, ok := k.(string)
			if !ok {
				klog.Warningf("Could not parse argument: %v for key %s to Arguments", argv, key)
				return nil, false
			}
			result[keyStr] = v
		}
		return result, true
	default:
		klog.Warningf("Could not parse argument: %v for key %s to Arguments", argv, key)
		return nil, false
	}
}

// GetQuantity returns a Kubernetes resource.Quantity from arguments.
func (a Arguments) GetQuantity(key string) (resource.Quantity, bool) {
	argv, ok := a[key]
	if !ok {
		return resource.Quantity{}, false
	}

	switch value := argv.(type) {
	case string:
		quantity, err := resource.ParseQuantity(normalizeQuantitySuffix(strings.TrimSpace(value)))
		if err != nil {
			klog.Warningf("Could not parse argument: %v for key %s to resource quantity", argv, key)
			return resource.Quantity{}, false
		}
		return quantity, true
	case int:
		return *resource.NewQuantity(int64(value), resource.DecimalSI), true
	case int64:
		return *resource.NewQuantity(value, resource.DecimalSI), true
	case float64:
		return *resource.NewMilliQuantity(int64(value*1000), resource.DecimalSI), true
	default:
		klog.Warningf("Could not parse argument: %v for key %s to resource quantity", argv, key)
		return resource.Quantity{}, false
	}
}

func normalizeQuantitySuffix(value string) string {
	replacements := map[string]string{
		"ki": "Ki",
		"mi": "Mi",
		"gi": "Gi",
		"ti": "Ti",
		"pi": "Pi",
		"ei": "Ei",
	}
	lowerValue := strings.ToLower(value)
	for lowerSuffix, canonicalSuffix := range replacements {
		if strings.HasSuffix(lowerValue, lowerSuffix) {
			return value[:len(value)-len(lowerSuffix)] + canonicalSuffix
		}
	}
	return value
}

// Get can automatically convert parameters according to the passed generic T type.
// If the parameter conversion is successful, it returns the converted parameter.
// If the parameter does not exist, it returns false.
// If the conversion fails, it will terminate the program with a fatal error.
func Get[T any](a Arguments, key string) (T, bool) {
	var result T
	argv, ok := a[key]
	if !ok {
		return result, false
	}

	err := mapstructure.Decode(argv, &result)
	if err != nil {
		klog.Fatalf("Could not parse argument for key %s to type %T: %v", key, result, err)
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
