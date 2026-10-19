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
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// SourceErrorReason categorizes a source-specific failure while a provider
// examines a Node. The cache owns the corresponding provider and Node context.
type SourceErrorReason string

const (
	// SourceErrorInvalidNode means the provider could not examine the supplied
	// Node because its identity or delivery metadata was incomplete.
	SourceErrorInvalidNode SourceErrorReason = "InvalidNode"
	// SourceErrorMalformedPayload means the provider source could not be decoded.
	SourceErrorMalformedPayload SourceErrorReason = "MalformedPayload"
	// SourceErrorUnsupportedSchema means the payload uses an unsupported schema.
	SourceErrorUnsupportedSchema SourceErrorReason = "UnsupportedSchema"
	// SourceErrorInvalidTopology means decoded source facts are inconsistent.
	SourceErrorInvalidTopology SourceErrorReason = "InvalidTopology"
	// SourceErrorStaleGeneration means a source supplied an obsolete generation.
	SourceErrorStaleGeneration SourceErrorReason = "StaleGeneration"
	// SourceErrorGenerationReuseWithDifferentFacts means one source generation
	// was reused for different normalized topology facts.
	SourceErrorGenerationReuseWithDifferentFacts SourceErrorReason = "GenerationReuseWithDifferentFacts"
	// SourceErrorUnsupportedFeature means the source requests a topology feature
	// that the active provider implementation cannot safely materialize yet.
	SourceErrorUnsupportedFeature SourceErrorReason = "UnsupportedFeature"
	// SourceErrorUnauthorizedProvider means scheduler configuration did not
	// authorize the provider to publish the update's resource topology.
	SourceErrorUnauthorizedProvider SourceErrorReason = "UnauthorizedProvider"
)

// SourceError is a typed error returned when a provider cannot produce a valid
// update for a Node.
type SourceError struct {
	Reason SourceErrorReason
	Cause  error
}

// Error implements the error interface.
func (err *SourceError) Error() string {
	if err == nil {
		return ""
	}
	if err.Cause == nil {
		return string(err.Reason)
	}
	return fmt.Sprintf("%s: %v", err.Reason, err.Cause)
}

// Unwrap returns the source error's underlying cause.
func (err *SourceError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// NewSourceError creates a typed source error with an optional underlying cause.
func NewSourceError(reason SourceErrorReason, cause error) *SourceError {
	return &SourceError{Reason: reason, Cause: cause}
}

// ValidateNodeMetadata verifies that an informer-delivered Node contains the
// identity and delivery metadata required for a provider update.
func ValidateNodeMetadata(node *corev1.Node) error {
	if node == nil {
		return fmt.Errorf("Node is required")
	}
	if node.Name == "" || node.UID == "" {
		return fmt.Errorf("Node name and UID are required")
	}
	if node.ResourceVersion == "" {
		return fmt.Errorf("Node resource version is required")
	}
	return nil
}

// ProviderUpdateOperation identifies how a provider changes its complete fact
// set for one Node.
type ProviderUpdateOperation string

const (
	// ReplaceFacts replaces one provider's complete topology fact set for a Node.
	ReplaceFacts ProviderUpdateOperation = "ReplaceFacts"
	// ClearFacts removes one provider's topology fact set for a Node.
	ClearFacts ProviderUpdateOperation = "ClearFacts"
)

// ProviderNodeUpdate is one complete source result for a provider and Node.
// Facts is present only for ReplaceFacts and must agree with the update
// metadata. ClearFacts proves that the provider inspected the Node and found
// no topology inventory.
type ProviderNodeUpdate struct {
	ProviderID          string
	NodeName            string
	NodeUID             types.UID
	NodeResourceVersion string
	SourceGeneration    uint64
	ObservedAt          time.Time
	FreshUntil          time.Time
	Operation           ProviderUpdateOperation
	Facts               *api.NodeTopologyFacts
}

// Clone returns an independent copy of the update and its topology facts.
func (update ProviderNodeUpdate) Clone() ProviderNodeUpdate {
	clone := update
	if update.Facts != nil {
		facts := update.Facts.Clone()
		clone.Facts = &facts
	}
	return clone
}

// Validate verifies that an update is internally consistent. It does not
// validate current informer state, generation ordering, or configured freshness
// limits; those require scheduler cache state.
func (update ProviderNodeUpdate) Validate() error {
	if update.ProviderID == "" {
		return fmt.Errorf("provider ID is required")
	}
	if update.NodeName == "" || update.NodeUID == "" {
		return fmt.Errorf("Node name and UID are required")
	}
	if update.NodeResourceVersion == "" {
		return fmt.Errorf("Node resource version is required")
	}
	if update.ObservedAt.IsZero() {
		return fmt.Errorf("observation time is required")
	}
	if !update.FreshUntil.IsZero() && update.FreshUntil.Before(update.ObservedAt) {
		return fmt.Errorf("freshness deadline precedes observation time")
	}

	switch update.Operation {
	case ReplaceFacts:
		return update.validateReplaceFacts()
	case ClearFacts:
		if update.SourceGeneration != 0 {
			return fmt.Errorf("ClearFacts source generation must be zero")
		}
		if update.Facts != nil {
			return fmt.Errorf("ClearFacts must not include topology facts")
		}
		if !update.FreshUntil.IsZero() {
			return fmt.Errorf("ClearFacts must not include a freshness deadline")
		}
		return nil
	default:
		return fmt.Errorf("unsupported provider update operation %q", update.Operation)
	}
}

func (update ProviderNodeUpdate) validateReplaceFacts() error {
	if update.SourceGeneration == 0 {
		return fmt.Errorf("ReplaceFacts source generation must be positive")
	}
	if update.Facts == nil {
		return fmt.Errorf("ReplaceFacts requires topology facts")
	}
	if err := api.ValidateNodeTopologyFacts(*update.Facts); err != nil {
		return fmt.Errorf("invalid topology facts: %w", err)
	}

	facts := update.Facts
	if facts.ProviderID != update.ProviderID {
		return fmt.Errorf("topology facts provider ID does not match update")
	}
	if facts.NodeName != update.NodeName || facts.NodeUID != update.NodeUID {
		return fmt.Errorf("topology facts Node identity does not match update")
	}
	if facts.NodeResourceVersion != update.NodeResourceVersion {
		return fmt.Errorf("topology facts Node resource version does not match update")
	}
	if facts.SourceGeneration != update.SourceGeneration {
		return fmt.Errorf("topology facts source generation does not match update")
	}
	if !facts.ObservedAt.Equal(update.ObservedAt) {
		return fmt.Errorf("topology facts observation time does not match update")
	}
	if !facts.FreshUntil.Equal(update.FreshUntil) {
		return fmt.Errorf("topology facts freshness deadline does not match update")
	}
	return nil
}

// NodeTopologyProvider converts source-specific Node topology into a complete
// update for the scheduler cache.
type NodeTopologyProvider interface {
	// ID identifies the configured provider implementation.
	ID() string
	// SyncNode examines one Node and returns its complete current result.
	SyncNode(ctx context.Context, node *corev1.Node) (ProviderNodeUpdate, error)
}
