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
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	adapterTestProviderID = "adapter-test-provider"
	adapterTestNamespace  = "adapter.test"
)

var adapterTestResource = corev1.ResourceName("nvidia.com/gpu")

func TestNewRegistry(t *testing.T) {
	tests := []struct {
		name      string
		adapters  []AllocationAdapter
		wantError string
	}{
		{
			name: "valid exact and observation-only adapters",
			adapters: []AllocationAdapter{
				newTestAllocationAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace, hardTopologyCapabilities()),
				newTestAllocationAdapter("other-provider", "amd.com/gpu", "other.test", AdapterCapabilities{}),
			},
		},
		{
			name:      "nil adapter",
			adapters:  []AllocationAdapter{nil},
			wantError: "must not be nil",
		},
		{
			name: "missing contract provider",
			adapters: []AllocationAdapter{
				newTestAllocationAdapter("", adapterTestResource, adapterTestNamespace, hardTopologyCapabilities()),
			},
			wantError: "requires a provider ID",
		},
		{
			name: "duplicate resource owner",
			adapters: []AllocationAdapter{
				newTestAllocationAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace, hardTopologyCapabilities()),
				newTestAllocationAdapter("other-provider", adapterTestResource, "other.test", hardTopologyCapabilities()),
			},
			wantError: "assigned to both allocation adapters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewRegistry(test.adapters)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("NewRegistry() error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}
			registered, found := registry.AdapterFor(adapterTestResource)
			if !found || registered == nil {
				t.Fatal("AdapterFor() = nil, false, want configured adapter")
			}
			contract, found := registry.IdentityContractFor(adapterTestResource)
			if !found || !contract.Matches(adapterTestProviderID, adapterTestResource, adapterTestNamespace) {
				t.Fatalf("IdentityContractFor() = (%#v, %v), want configured contract", contract, found)
			}
		})
	}
}

func TestRegistryHardTopologyAdapterRequiresMatchingCompleteContract(t *testing.T) {
	hardAdapter := newTestAllocationAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace, hardTopologyCapabilities())
	registry, err := NewRegistry([]AllocationAdapter{hardAdapter})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	if got, found := registry.HardTopologyAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace); !found || got != hardAdapter {
		t.Fatalf("HardTopologyAdapter() = (%T, %v), want configured hard adapter", got, found)
	}
	if got, found := registry.HardTopologyAdapter(adapterTestProviderID, adapterTestResource, "other.test"); found || got != nil {
		t.Fatalf("HardTopologyAdapter() = (%T, %v) for mismatched namespace, want nil, false", got, found)
	}

	observationOnly := newTestAllocationAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace, AdapterCapabilities{ExactDeviceIDs: true})
	registry, err = NewRegistry([]AllocationAdapter{observationOnly})
	if err != nil {
		t.Fatalf("NewRegistry() observation-only error = %v", err)
	}
	if _, found := registry.AdapterFor(adapterTestResource); !found {
		t.Fatal("AdapterFor() did not return observation-only adapter")
	}
	if got, found := registry.HardTopologyAdapter(adapterTestProviderID, adapterTestResource, adapterTestNamespace); found || got != nil {
		t.Fatalf("HardTopologyAdapter() = (%T, %v) for incomplete capabilities, want nil, false", got, found)
	}
}

func TestAdapterCapabilitiesSupportsHardTopology(t *testing.T) {
	if !hardTopologyCapabilities().SupportsHardTopology() {
		t.Fatal("complete capabilities do not support hard topology")
	}
	incomplete := hardTopologyCapabilities()
	incomplete.Recovery = false
	if incomplete.SupportsHardTopology() {
		t.Fatal("capabilities without recovery support hard topology, want false")
	}
}

func TestError(t *testing.T) {
	cause := errors.New("backend conflict")
	err := NewError(ErrorReservationConflict, cause)
	if err.Reason != ErrorReservationConflict {
		t.Fatalf("Reason = %q, want %q", err.Reason, ErrorReservationConflict)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, %v) = false, want true", err, cause)
	}
}

type testAllocationAdapter struct {
	capabilities AdapterCapabilities
	contract     DeviceIdentityContract
}

func newTestAllocationAdapter(providerID string, resourceName corev1.ResourceName, namespace string, capabilities AdapterCapabilities) *testAllocationAdapter {
	return &testAllocationAdapter{
		capabilities: capabilities,
		contract: DeviceIdentityContract{
			ProviderID:   providerID,
			ResourceName: resourceName,
			Namespace:    namespace,
		},
	}
}

func hardTopologyCapabilities() AdapterCapabilities {
	return AdapterCapabilities{
		ExactDeviceIDs:        true,
		AtomicReservation:     true,
		ImmutableBindHandoff:  true,
		AllocationObservation: true,
		ReleaseObservation:    true,
		Recovery:              true,
	}
}

func (adapter *testAllocationAdapter) Capabilities() AdapterCapabilities {
	return adapter.capabilities
}

func (adapter *testAllocationAdapter) IdentityContract() DeviceIdentityContract {
	return adapter.contract
}

func (adapter *testAllocationAdapter) ValidateTopology(context.Context, api.NodeTopologyFacts) error {
	return nil
}

func (adapter *testAllocationAdapter) Reserve(context.Context, api.TopologyPlacementPlan) (AdapterReservation, error) {
	return AdapterReservation{}, nil
}

func (adapter *testAllocationAdapter) PrepareBind(context.Context, AdapterReservation, api.TopologyTaskPlacement) (AllocationHandoff, error) {
	return AllocationHandoff{}, nil
}

func (adapter *testAllocationAdapter) Confirm(context.Context, AdapterReservation) (AllocationObservation, error) {
	return AllocationObservation{}, nil
}

func (adapter *testAllocationAdapter) Release(context.Context, AdapterReservation) error {
	return nil
}

func (adapter *testAllocationAdapter) Recover(context.Context) ([]AdapterReservationState, error) {
	return nil, nil
}
