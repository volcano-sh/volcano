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

package policy

import "k8s.io/klog/v2"

// GetInt retrieves an integer value from arguments. YAML/JSON decoded numbers
// arrive as float64; Go map literals in tests pass int directly.
func (a Arguments) GetInt(ptr *int, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok {
		return
	}

	switch value := argv.(type) {
	case int:
		*ptr = value
	case float64:
		*ptr = int(value)
	default:
		klog.Warningf("Could not parse argument: %v for key %s to int", argv, key)
	}
}

// GetFloat64 retrieves a float64 value from arguments
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
		// Try to convert from int
		if intVal, ok := argv.(int); ok {
			*ptr = float64(intVal)
			return
		}
		klog.Warningf("Could not parse argument: %v for key %s to float64", argv, key)
		return
	}

	*ptr = value
}

// GetString retrieves a string value from arguments
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
