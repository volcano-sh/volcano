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

package provider

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// DefaultMaxAge is how long a provider's facts stay fresh after their
// observation time when its registration sets no MaxAge.
const DefaultMaxAge = 2 * time.Minute

// Registration configures one authoritative provider and identity namespace
// for its complete set of extended-resource topology facts.
type Registration struct {
	ProviderID        string
	IdentityNamespace string
	ResourceNames     []corev1.ResourceName
	// MaxAge bounds how long the provider's facts stay fresh after their
	// observation time, even when the source reports a later freshness
	// deadline. The provider must refresh its facts within this time. Zero
	// means DefaultMaxAge.
	MaxAge time.Duration
}

// Registry is an immutable resource-to-provider ownership map. It prevents
// SchedulerCache from combining topology facts from sources that could
// describe the same physical device.
type Registry struct {
	registrationsByProvider map[string]Registration
	registrationsByResource map[corev1.ResourceName]Registration
}

// NewRegistry validates and indexes authoritative topology provider
// registrations. Every resource has exactly one configured provider, and one
// provider uses exactly one identity namespace.
func NewRegistry(registrations []Registration) (*Registry, error) {
	registry := &Registry{
		registrationsByProvider: make(map[string]Registration, len(registrations)),
		registrationsByResource: make(map[corev1.ResourceName]Registration),
	}

	for _, registration := range registrations {
		if registration.ProviderID == "" {
			return nil, fmt.Errorf("topology provider registration requires a provider ID")
		}
		if registration.IdentityNamespace == "" {
			return nil, fmt.Errorf("topology provider registration %q requires an identity namespace", registration.ProviderID)
		}
		if len(registration.ResourceNames) == 0 {
			return nil, fmt.Errorf("topology provider registration %q requires at least one resource", registration.ProviderID)
		}
		if _, exists := registry.registrationsByProvider[registration.ProviderID]; exists {
			return nil, fmt.Errorf("topology provider %q has more than one registration", registration.ProviderID)
		}
		if registration.MaxAge < 0 {
			return nil, fmt.Errorf("topology provider registration %q has negative max age %s", registration.ProviderID, registration.MaxAge)
		}

		registration = cloneRegistration(registration)
		if registration.MaxAge == 0 {
			registration.MaxAge = DefaultMaxAge
		}
		registeredResources := make(map[corev1.ResourceName]struct{}, len(registration.ResourceNames))
		for _, resourceName := range registration.ResourceNames {
			if resourceName == "" {
				return nil, fmt.Errorf("topology provider registration %q contains an empty resource name", registration.ProviderID)
			}
			if _, exists := registeredResources[resourceName]; exists {
				return nil, fmt.Errorf("topology provider registration %q contains resource %q more than once", registration.ProviderID, resourceName)
			}
			registeredResources[resourceName] = struct{}{}
			if previous, exists := registry.registrationsByResource[resourceName]; exists {
				return nil, fmt.Errorf("resource %q is assigned to both topology providers %q and %q", resourceName, previous.ProviderID, registration.ProviderID)
			}
			registry.registrationsByResource[resourceName] = registration
		}
		registry.registrationsByProvider[registration.ProviderID] = registration
	}

	return registry, nil
}

// ValidateOwnership verifies that a valid provider update belongs to one
// configured provider and only describes resources in that provider's
// configured identity namespace. Callers must validate the update itself
// before calling this method.
func (registry *Registry) ValidateOwnership(update ProviderNodeUpdate) error {
	if registry == nil {
		return fmt.Errorf("no topology provider registry is configured")
	}

	registration, exists := registry.registrationsByProvider[update.ProviderID]
	if !exists {
		return fmt.Errorf("provider %q is not configured to publish topology facts", update.ProviderID)
	}
	if update.Operation == ClearFacts {
		return nil
	}
	if update.Facts == nil {
		return fmt.Errorf("ReplaceFacts update from provider %q has no topology facts", update.ProviderID)
	}

	for _, device := range update.Facts.Devices {
		if err := registry.validateResourceOwnership(registration, device.ResourceName, device.ID.Namespace); err != nil {
			return fmt.Errorf("device %q: %w", device.ID.Value, err)
		}
	}
	for _, domain := range update.Facts.LocalDomains {
		if err := registry.validateResourceOwnership(registration, domain.ResourceName, domain.ID.Namespace); err != nil {
			return fmt.Errorf("local domain %q: %w", domain.ID.Value, err)
		}
	}
	for _, fabric := range update.Facts.Fabrics {
		if fabric.ID.Namespace != registration.IdentityNamespace {
			return fmt.Errorf("fabric %q uses identity namespace %q, want configured namespace %q", fabric.ID.Value, fabric.ID.Namespace, registration.IdentityNamespace)
		}
	}

	return nil
}

// MaxAge returns how long the facts of a configured provider stay fresh after
// their observation time.
func (registry *Registry) MaxAge(providerID string) (time.Duration, bool) {
	if registry == nil {
		return 0, false
	}
	registration, exists := registry.registrationsByProvider[providerID]
	return registration.MaxAge, exists
}

func (registry *Registry) validateResourceOwnership(registration Registration, resourceName corev1.ResourceName, identityNamespace string) error {
	resourceRegistration, exists := registry.registrationsByResource[resourceName]
	if !exists {
		return fmt.Errorf("resource %q has no configured topology provider", resourceName)
	}
	if resourceRegistration.ProviderID != registration.ProviderID {
		return fmt.Errorf("resource %q belongs to configured provider %q, not provider %q", resourceName, resourceRegistration.ProviderID, registration.ProviderID)
	}
	if identityNamespace != registration.IdentityNamespace {
		return fmt.Errorf("resource %q uses identity namespace %q, want configured namespace %q", resourceName, identityNamespace, registration.IdentityNamespace)
	}
	return nil
}

func cloneRegistration(registration Registration) Registration {
	clone := registration
	clone.ResourceNames = append([]corev1.ResourceName(nil), registration.ResourceNames...)
	return clone
}
