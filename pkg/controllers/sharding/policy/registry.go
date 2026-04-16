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

import (
	"fmt"
	"sync"
)

var (
	registry      = make(map[string]PolicyBuilder)
	registryMutex sync.RWMutex
)

// RegisterPolicy registers a policy builder with the given name
func RegisterPolicy(name string, builder PolicyBuilder) {
	registryMutex.Lock()
	defer registryMutex.Unlock()

	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("policy %s already registered", name))
	}

	registry[name] = builder
}

// GetPolicy retrieves a policy builder by name
func GetPolicy(name string) (PolicyBuilder, error) {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	builder, exists := registry[name]
	if !exists {
		return nil, fmt.Errorf("policy %s not found", name)
	}

	return builder, nil
}

// ListPolicies returns a list of all registered policy names
func ListPolicies() []string {
	registryMutex.RLock()
	defer registryMutex.RUnlock()

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}

	return names
}
