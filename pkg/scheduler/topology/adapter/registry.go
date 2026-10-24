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

package adapter

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// Registry is an immutable resource-to-allocation-adapter map. It prevents two
// backends from independently owning exact allocation for the same resource.
type Registry struct {
	adaptersByResource  map[corev1.ResourceName]AllocationAdapter
	contractsByResource map[corev1.ResourceName]DeviceIdentityContract
}

// NewRegistry validates and indexes allocation adapters. Each configured
// extended resource has exactly one allocation owner.
func NewRegistry(adapters []AllocationAdapter) (*Registry, error) {
	registry := &Registry{
		adaptersByResource:  make(map[corev1.ResourceName]AllocationAdapter, len(adapters)),
		contractsByResource: make(map[corev1.ResourceName]DeviceIdentityContract, len(adapters)),
	}
	for _, allocationAdapter := range adapters {
		if allocationAdapter == nil {
			return nil, fmt.Errorf("device topology allocation adapter must not be nil")
		}
		contract := allocationAdapter.IdentityContract()
		if err := contract.Validate(); err != nil {
			return nil, err
		}
		if previous, exists := registry.contractsByResource[contract.ResourceName]; exists {
			return nil, fmt.Errorf("resource %q is assigned to both allocation adapters for providers %q and %q", contract.ResourceName, previous.ProviderID, contract.ProviderID)
		}
		registry.adaptersByResource[contract.ResourceName] = allocationAdapter
		registry.contractsByResource[contract.ResourceName] = contract
	}
	return registry, nil
}

// AdapterFor returns the configured allocation adapter for one extended
// resource. Callers must use HardTopologyAdapter before relying on it for hard
// device-topology enforcement.
func (registry *Registry) AdapterFor(resourceName corev1.ResourceName) (AllocationAdapter, bool) {
	if registry == nil {
		return nil, false
	}
	allocationAdapter, exists := registry.adaptersByResource[resourceName]
	return allocationAdapter, exists
}

// IdentityContractFor returns the immutable device-ID contract configured for
// one resource.
func (registry *Registry) IdentityContractFor(resourceName corev1.ResourceName) (DeviceIdentityContract, bool) {
	if registry == nil {
		return DeviceIdentityContract{}, false
	}
	contract, exists := registry.contractsByResource[resourceName]
	return contract, exists
}

// HardTopologyAdapter returns an adapter only when its identity contract
// matches the provider facts and it can enforce every hard-mode guarantee.
func (registry *Registry) HardTopologyAdapter(providerID string, resourceName corev1.ResourceName, identityNamespace string) (AllocationAdapter, bool) {
	allocationAdapter, exists := registry.AdapterFor(resourceName)
	if !exists {
		return nil, false
	}
	contract, exists := registry.IdentityContractFor(resourceName)
	if !exists || !contract.Matches(providerID, resourceName, identityNamespace) || !allocationAdapter.Capabilities().SupportsHardTopology() {
		return nil, false
	}
	return allocationAdapter, true
}
