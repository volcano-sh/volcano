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

// Package mock provides deterministic topology updates for unit and integration tests.
package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

// Provider is an in-memory Node topology source for tests. Fixtures are
// validated when installed and returned as independent copies.
type Provider struct {
	providerID string
	now        func() time.Time

	mutex   sync.RWMutex
	updates map[types.UID]provider.ProviderNodeUpdate
	errors  map[types.UID]error
}

var _ provider.NodeTopologyProvider = (*Provider)(nil)

// New creates a deterministic mock provider. A nil clock uses time.Now.
func New(providerID string, now func() time.Time) (*Provider, error) {
	if providerID == "" {
		return nil, fmt.Errorf("provider ID is required")
	}
	if now == nil {
		now = time.Now
	}
	return &Provider{
		providerID: providerID,
		now:        now,
		updates:    make(map[types.UID]provider.ProviderNodeUpdate),
		errors:     make(map[types.UID]error),
	}, nil
}

// ID identifies this configured mock source.
func (source *Provider) ID() string {
	return source.providerID
}

// SetNodeUpdate installs a validated result for a Node UID. The update metadata
// is preserved so cache tests can model delayed or stale source delivery.
func (source *Provider) SetNodeUpdate(update provider.ProviderNodeUpdate) error {
	if update.ProviderID != source.ID() {
		return fmt.Errorf("update provider ID %q does not match mock provider %q", update.ProviderID, source.ID())
	}
	if err := update.Validate(); err != nil {
		return fmt.Errorf("validate Node update: %w", err)
	}
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.updates[update.NodeUID] = update.Clone()
	delete(source.errors, update.NodeUID)
	return nil
}

// SetNodeError makes SyncNode return an injected source error for a Node UID.
func (source *Provider) SetNodeError(nodeUID types.UID, err error) error {
	if nodeUID == "" {
		return fmt.Errorf("Node UID is required")
	}
	if err == nil {
		return fmt.Errorf("source error is required")
	}
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.errors[nodeUID] = err
	delete(source.updates, nodeUID)
	return nil
}

// RemoveNode removes a Node fixture or injected error. A later SyncNode for the
// UID returns a valid ClearFacts result using the supplied Node metadata.
func (source *Provider) RemoveNode(nodeUID types.UID) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	delete(source.updates, nodeUID)
	delete(source.errors, nodeUID)
}

// SyncNode returns the configured fixture for a Node, an injected source
// error, or ClearFacts when no fixture is installed.
func (source *Provider) SyncNode(ctx context.Context, node *corev1.Node) (provider.ProviderNodeUpdate, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return provider.ProviderNodeUpdate{}, err
		}
	}
	if err := provider.ValidateNodeMetadata(node); err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidNode, err)
	}

	source.mutex.RLock()
	err := source.errors[node.UID]
	update, found := source.updates[node.UID]
	source.mutex.RUnlock()
	if err != nil {
		return provider.ProviderNodeUpdate{}, err
	}
	if found {
		return update.Clone(), nil
	}

	update = provider.ProviderNodeUpdate{
		ProviderID:          source.ID(),
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		ObservedAt:          source.now(),
		Operation:           provider.ClearFacts,
	}
	if err := update.Validate(); err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}
	return update, nil
}
