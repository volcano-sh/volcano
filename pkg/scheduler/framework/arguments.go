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
	"strconv"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/conf"
)

// Arguments map
type Arguments map[string]string

// GetInt get the integer value from string
func (a Arguments) GetInt(ptr *int, key string) {
	if ptr == nil {
		return
	}

	argv, ok := a[key]
	if !ok || argv == "" {
		return
	}

	value, err := strconv.Atoi(argv)
	if err != nil {
		klog.Warningf("Could not parse argument: %s for key %s, with err %v", argv, key, err)
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
	if !ok || len(argv) == 0 {
		return
	}

	value, err := strconv.ParseFloat(argv, 64)
	if err != nil {
		klog.Warningf("Could not parse argument: %s for key %s, with err %v", argv, key, err)
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
	if !ok || argv == "" {
		return
	}

	value, err := strconv.ParseBool(argv)
	if err != nil {
		klog.Warningf("Could not parse argument: %s for key %s, with err %v", argv, key, err)
		return
	}

	*ptr = value
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
