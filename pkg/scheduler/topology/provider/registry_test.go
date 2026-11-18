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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func TestNewRegistry(t *testing.T) {
	otherResource := corev1.ResourceName("amd.com/gpu")
	tests := []struct {
		name          string
		registrations []Registration
		wantError     string
	}{
		{
			name: "valid registrations",
			registrations: []Registration{
				{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}},
				{ProviderID: "other-provider", IdentityNamespace: "other.example", ResourceNames: []corev1.ResourceName{otherResource}},
			},
		},
		{
			name:          "missing provider ID",
			registrations: []Registration{{IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}}},
			wantError:     "requires a provider ID",
		},
		{
			name:          "missing identity namespace",
			registrations: []Registration{{ProviderID: providerTestID, ResourceNames: []corev1.ResourceName{providerTestResource}}},
			wantError:     "requires an identity namespace",
		},
		{
			name:          "missing resources",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace}},
			wantError:     "requires at least one resource",
		},
		{
			name:          "empty resource",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{""}}},
			wantError:     "contains an empty resource name",
		},
		{
			name:          "repeated provider",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}}, {ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{otherResource}}},
			wantError:     "has more than one registration",
		},
		{
			name:          "repeated resource in registration",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource, providerTestResource}}},
			wantError:     "more than once",
		},
		{
			name:          "resource owned by two providers",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}}, {ProviderID: "other-provider", IdentityNamespace: "other.example", ResourceNames: []corev1.ResourceName{providerTestResource}}},
			wantError:     "assigned to both topology providers",
		},
		{
			name:          "negative max age",
			registrations: []Registration{{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}, MaxAge: -time.Second}},
			wantError:     "negative max age",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistry(test.registrations)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("NewRegistry() error = %v, want nil", err)
				}
				if registry == nil {
					t.Fatal("NewRegistry() = nil, want registry")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewRegistry() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestRegistryMaxAge(t *testing.T) {
	registry, err := NewRegistry([]Registration{
		{ProviderID: "default-age", IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}},
		{ProviderID: "custom-age", IdentityNamespace: "other.example", ResourceNames: []corev1.ResourceName{"amd.com/gpu"}, MaxAge: 10 * time.Minute},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	tests := []struct {
		providerID string
		wantAge    time.Duration
		wantFound  bool
	}{
		{providerID: "default-age", wantAge: DefaultMaxAge, wantFound: true},
		{providerID: "custom-age", wantAge: 10 * time.Minute, wantFound: true},
		{providerID: "unknown", wantAge: 0, wantFound: false},
	}
	for _, test := range tests {
		age, found := registry.MaxAge(test.providerID)
		if age != test.wantAge || found != test.wantFound {
			t.Errorf("MaxAge(%q) = %s, %t; want %s, %t", test.providerID, age, found, test.wantAge, test.wantFound)
		}
	}
}

func TestRegistryValidateOwnership(t *testing.T) {
	registry, err := NewRegistry([]Registration{
		{ProviderID: providerTestID, IdentityNamespace: providerTestNamespace, ResourceNames: []corev1.ResourceName{providerTestResource}},
		{ProviderID: "other-provider", IdentityNamespace: "other.example", ResourceNames: []corev1.ResourceName{"amd.com/gpu"}},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	tests := []struct {
		name      string
		mutate    func(*ProviderNodeUpdate)
		wantError string
	}{
		{
			name: "matching replace facts",
		},
		{
			name: "matching clear facts",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = ClearFacts
				update.SourceGeneration = 0
				update.FreshUntil = time.Time{}
				update.Facts = nil
			},
		},
		{
			name: "unconfigured provider",
			mutate: func(update *ProviderNodeUpdate) {
				update.ProviderID = "unknown-provider"
				update.Operation = ClearFacts
				update.SourceGeneration = 0
				update.FreshUntil = time.Time{}
				update.Facts = nil
			},
			wantError: "not configured",
		},
		{
			name: "resource belongs to another provider",
			mutate: func(update *ProviderNodeUpdate) {
				update.Facts.Devices[0].ResourceName = "amd.com/gpu"
				update.Facts.LocalDomains[0].ResourceName = "amd.com/gpu"
			},
			wantError: "belongs to configured provider",
		},
		{
			name: "identity namespace mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				setUpdateIdentityNamespace(update, "unexpected.example")
			},
			wantError: "uses identity namespace",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := testProviderNodeUpdate()
			if test.mutate != nil {
				test.mutate(&update)
			}

			err := registry.ValidateOwnership(update)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateOwnership() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ValidateOwnership() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func setUpdateIdentityNamespace(update *ProviderNodeUpdate, identityNamespace string) {
	update.Facts.Devices[0].ID.Namespace = identityNamespace
	update.Facts.Devices[0].LocalDomainID.Namespace = identityNamespace
	update.Facts.LocalDomains[0].ID.Namespace = identityNamespace
	update.Facts.LocalDomains[0].DeviceIDs[0].Namespace = identityNamespace
}
