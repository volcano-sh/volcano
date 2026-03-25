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

package policy

import "k8s.io/klog/v2"

// GetInt retrieves an integer value from arguments
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

// GetBool retrieves a boolean value from arguments
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
		klog.Warningf("Could not parse argument: %v for key %s to bool", argv, key)
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
