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

// Package config turns scheduler configuration into trusted topology sources.
package config

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
	"volcano.sh/volcano/pkg/scheduler/topology/provider/annotations"
)

// ProviderRuntime contains the immutable provider ownership registry and the
// source instances that SchedulerCache invokes during Node synchronization.
type ProviderRuntime struct {
	Registry *provider.Registry
	Sources  []provider.NodeTopologyProvider
}

// NewProviderRuntime builds the topology sources selected by one optional
// deviceTopology configuration. A nil configuration disables topology source
// synchronization without changing normal scheduler behavior.
func NewProviderRuntime(configuration *conf.DeviceTopologyConfiguration) (*ProviderRuntime, error) {
	if configuration == nil {
		return &ProviderRuntime{}, nil
	}

	registrations := make([]provider.Registration, 0, len(configuration.Providers))
	sources := make([]provider.NodeTopologyProvider, 0, len(configuration.Providers))
	for _, configuredProvider := range configuration.Providers {
		registration := provider.Registration{
			ProviderID:        configuredProvider.ID,
			IdentityNamespace: configuredProvider.IdentityNamespace,
			ResourceNames:     append([]corev1.ResourceName(nil), configuredProvider.Resources...),
			MaxAge:            configuredProvider.MaxAge,
		}
		registrations = append(registrations, registration)

		source, err := newProviderSource(configuredProvider)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}

	registry, err := provider.NewRegistry(registrations)
	if err != nil {
		return nil, err
	}
	return &ProviderRuntime{Registry: registry, Sources: sources}, nil
}

// newProviderSource constructs the source-specific implementation for one
// scheduler configuration entry.
func newProviderSource(configuration conf.DeviceTopologyProviderConfiguration) (provider.NodeTopologyProvider, error) {
	switch configuration.Type {
	case conf.DeviceTopologyProviderTypeAnnotation:
		return annotations.New(annotations.Config{
			ProviderID:        configuration.ID,
			IdentityNamespace: configuration.IdentityNamespace,
		})
	default:
		return nil, fmt.Errorf("device topology provider %q has unsupported type %q", configuration.ID, configuration.Type)
	}
}
