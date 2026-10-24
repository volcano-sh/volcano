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

// Package adapter defines the trusted backend contract for exact
// device-topology allocation. It does not implement reservation or binding.
package adapter

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// AdapterCapabilities describes the guarantees that an allocation adapter can
// provide for a topology placement.
type AdapterCapabilities struct {
	ExactDeviceIDs        bool
	AtomicReservation     bool
	ImmutableBindHandoff  bool
	AllocationObservation bool
	ReleaseObservation    bool
	Recovery              bool
}

// SupportsHardTopology reports whether the adapter has every guarantee needed
// to enforce a hard device-topology policy safely.
func (capabilities AdapterCapabilities) SupportsHardTopology() bool {
	return capabilities.ExactDeviceIDs &&
		capabilities.AtomicReservation &&
		capabilities.ImmutableBindHandoff &&
		capabilities.AllocationObservation &&
		capabilities.ReleaseObservation &&
		capabilities.Recovery
}

// DeviceIdentityContract identifies the provider, resource, and canonical
// device-ID namespace that one allocation adapter can safely enforce.
type DeviceIdentityContract struct {
	ProviderID   string
	ResourceName corev1.ResourceName
	Namespace    string
}

// Validate verifies that the contract identifies exactly one device-ID scheme.
func (contract DeviceIdentityContract) Validate() error {
	if contract.ProviderID == "" {
		return fmt.Errorf("device identity contract requires a provider ID")
	}
	if contract.ResourceName == "" {
		return fmt.Errorf("device identity contract for provider %q requires a resource name", contract.ProviderID)
	}
	if contract.Namespace == "" {
		return fmt.Errorf("device identity contract for resource %q requires an identity namespace", contract.ResourceName)
	}
	return nil
}

// Matches reports whether provider facts for a resource use this adapter's
// declared canonical device-ID scheme.
func (contract DeviceIdentityContract) Matches(providerID string, resourceName corev1.ResourceName, namespace string) bool {
	return contract.ProviderID == providerID && contract.ResourceName == resourceName && contract.Namespace == namespace
}

// AdapterReservation is the backend's durable handle for one accepted plan.
type AdapterReservation struct {
	Token      string
	PlanDigest string
}

// AllocationHandoff carries a reservation's immutable selected-ID context to
// the bind or DRA path for one exact Pod and Node.
type AllocationHandoff struct {
	ReservationToken string
	PlanDigest       string
	PodUID           types.UID
	NodeName         string
}

// AdapterReservationLifecycleState is the reservation state returned during
// adapter recovery after a scheduler restart or leader transition.
type AdapterReservationLifecycleState string

const (
	// AdapterReservationHeld means the adapter reserved devices before bind handoff.
	AdapterReservationHeld AdapterReservationLifecycleState = "Held"
	// AdapterReservationBinding means the adapter is awaiting allocation confirmation.
	AdapterReservationBinding AdapterReservationLifecycleState = "Binding"
	// AdapterReservationAllocated means the adapter confirmed the final allocation.
	AdapterReservationAllocated AdapterReservationLifecycleState = "Allocated"
)

// AdapterReservationState describes an active backend reservation or
// allocation recovered after scheduler startup or leader promotion.
type AdapterReservationState struct {
	Reservation AdapterReservation
	Plan        api.TopologyPlacementPlan
	State       AdapterReservationLifecycleState
	ExpiresAt   time.Time
}

// AllocationObservationState identifies an authoritative allocation lifecycle
// event reported by an adapter.
type AllocationObservationState string

const (
	// AllocationObservationAllocated confirms that the backend assigned devices.
	AllocationObservationAllocated AllocationObservationState = "Allocated"
	// AllocationObservationReleased confirms that the backend released devices.
	AllocationObservationReleased AllocationObservationState = "Released"
)

// AllocationObservation is the backend's authoritative allocation or release
// report for one reservation and Pod.
type AllocationObservation struct {
	ReservationToken string
	PlanDigest       string
	PodUID           types.UID
	State            AllocationObservationState
	ObservedAt       time.Time
}

// ErrorReason categorizes a structured allocation-adapter failure.
type ErrorReason string

const (
	// ErrorUnsupportedExactSelection means the backend cannot honor selected IDs.
	ErrorUnsupportedExactSelection ErrorReason = "UnsupportedExactSelection"
	// ErrorReservationConflict means another allocation already holds an ID.
	ErrorReservationConflict ErrorReason = "ReservationConflict"
	// ErrorStaleInventory means adapter and topology inventory no longer agree.
	ErrorStaleInventory ErrorReason = "StaleInventory"
	// ErrorBackendFailure means the adapter encountered an otherwise unclassified backend error.
	ErrorBackendFailure ErrorReason = "BackendFailure"
)

// Error is a typed error returned by an allocation adapter.
type Error struct {
	Reason ErrorReason
	Cause  error
}

// Error implements the error interface.
func (err *Error) Error() string {
	if err == nil {
		return ""
	}
	if err.Cause == nil {
		return string(err.Reason)
	}
	return fmt.Sprintf("%s: %v", err.Reason, err.Cause)
}

// Unwrap returns the adapter error's underlying cause.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// NewError creates a typed allocation-adapter error with an optional cause.
func NewError(reason ErrorReason, cause error) *Error {
	return &Error{Reason: reason, Cause: cause}
}

// AllocationAdapter bridges a scheduler-selected topology plan to the backend
// that reserves and assigns actual devices. Implementations must treat plans
// and handoffs as immutable; scheduler cache reservation integration follows
// in a later change.
type AllocationAdapter interface {
	// Capabilities reports the guarantees this adapter provides.
	Capabilities() AdapterCapabilities
	// IdentityContract identifies the only provider-owned device-ID scheme this
	// adapter can safely enforce.
	IdentityContract() DeviceIdentityContract
	// ValidateTopology verifies that one provider fact set is understood by the
	// allocation backend before hard placement relies on it.
	ValidateTopology(ctx context.Context, facts api.NodeTopologyFacts) error
	// Reserve atomically holds every selected device in plan or returns an error
	// without partially reserving any device.
	Reserve(ctx context.Context, plan api.TopologyPlacementPlan) (AdapterReservation, error)
	// PrepareBind creates an immutable, Pod-specific handoff for one placement.
	PrepareBind(ctx context.Context, reservation AdapterReservation, placement api.TopologyTaskPlacement) (AllocationHandoff, error)
	// Confirm returns the backend's authoritative allocation or release result.
	Confirm(ctx context.Context, reservation AdapterReservation) (AllocationObservation, error)
	// Release removes a backend reservation that will not be used.
	Release(ctx context.Context, reservation AdapterReservation) error
	// Recover returns all active backend reservations and allocations so the
	// scheduler can reconstruct its availability ledger safely.
	Recover(ctx context.Context) ([]AdapterReservationState, error)
}
