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

package config

import (
	"fmt"
	"slices"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	adaptermock "volcano.sh/volcano/pkg/scheduler/topology/adapter/mock"
)

// NewAdapterRegistry builds the allocation adapters selected by one optional
// deviceTopology configuration. Each adapter takes its provider ID and
// identity namespace from the provider it names. A nil registry means no
// adapter is configured, so hard placement is rejected.
func NewAdapterRegistry(configuration *conf.DeviceTopologyConfiguration) (*adapter.Registry, error) {
	if configuration == nil || len(configuration.Adapters) == 0 {
		return nil, nil
	}

	providers := make(map[string]conf.DeviceTopologyProviderConfiguration, len(configuration.Providers))
	for _, configuredProvider := range configuration.Providers {
		providers[configuredProvider.ID] = configuredProvider
	}
	adapters := make([]adapter.AllocationAdapter, 0, len(configuration.Adapters))
	for _, configuredAdapter := range configuration.Adapters {
		configuredProvider, exists := providers[configuredAdapter.Provider]
		if !exists {
			return nil, fmt.Errorf("device topology adapter for resource %q references unknown provider %q", configuredAdapter.Resource, configuredAdapter.Provider)
		}
		if !slices.Contains(configuredProvider.Resources, configuredAdapter.Resource) {
			return nil, fmt.Errorf("device topology provider %q does not publish adapter resource %q", configuredProvider.ID, configuredAdapter.Resource)
		}
		allocationAdapter, err := newAllocationAdapter(configuredAdapter, adapter.DeviceIdentityContract{
			ProviderID:   configuredProvider.ID,
			ResourceName: configuredAdapter.Resource,
			Namespace:    configuredProvider.IdentityNamespace,
		})
		if err != nil {
			return nil, err
		}
		adapters = append(adapters, allocationAdapter)
	}
	return adapter.NewRegistry(adapters)
}

// newAllocationAdapter constructs the type-specific adapter for one
// scheduler configuration entry.
func newAllocationAdapter(configuration conf.DeviceTopologyAdapterConfiguration, contract adapter.DeviceIdentityContract) (adapter.AllocationAdapter, error) {
	switch configuration.Type {
	case conf.DeviceTopologyAdapterTypeMock:
		klog.Warningf("Device topology adapter for resource %q is the mock adapter: selected device IDs are not enforced on Nodes", contract.ResourceName)
		return adaptermock.New(contract, nil)
	default:
		return nil, fmt.Errorf("device topology adapter for resource %q has unsupported type %q", configuration.Resource, configuration.Type)
	}
}
