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

// Package annotations provides the trusted Node annotation topology source.
package annotations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

const (
	// AnnotationKey is the trusted Node annotation containing device inventory.
	AnnotationKey = "topology.volcano.sh/device-inventory"
	// APIVersion is the first supported annotation inventory schema.
	APIVersion = "topology.volcano.sh/v1alpha1"
)

// Config configures a Node annotation topology provider.
type Config struct {
	// ProviderID identifies the configured authoritative topology source.
	ProviderID string
	// IdentityNamespace scopes source-owned device and domain values.
	IdentityNamespace string
	// Now supplies the observation time for a valid ClearFacts result. Nil uses
	// time.Now.
	Now func() time.Time
}

// Provider reads a trusted device inventory annotation from a Kubernetes Node.
type Provider struct {
	providerID        string
	identityNamespace string
	now               func() time.Time
}

var _ provider.NodeTopologyProvider = (*Provider)(nil)

// New creates a Node annotation topology provider.
func New(config Config) (*Provider, error) {
	if config.ProviderID == "" {
		return nil, fmt.Errorf("provider ID is required")
	}
	if config.IdentityNamespace == "" {
		return nil, fmt.Errorf("identity namespace is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Provider{
		providerID:        config.ProviderID,
		identityNamespace: config.IdentityNamespace,
		now:               config.Now,
	}, nil
}

// ID identifies this configured topology source.
func (source *Provider) ID() string {
	return source.providerID
}

// SyncNode parses the Node's complete inventory annotation into one validated
// provider update. A missing annotation is a valid ClearFacts result.
func (source *Provider) SyncNode(ctx context.Context, node *corev1.Node) (provider.ProviderNodeUpdate, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return provider.ProviderNodeUpdate{}, err
		}
	}
	if err := provider.ValidateNodeMetadata(node); err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidNode, err)
	}

	rawInventory, found := node.Annotations[AnnotationKey]
	if !found || rawInventory == "" {
		return source.clearFacts(node)
	}

	inventory, err := decodeInventory(rawInventory)
	if err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorMalformedPayload, err)
	}
	if inventory.APIVersion != APIVersion {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorUnsupportedSchema, fmt.Errorf("unsupported inventory apiVersion %q", inventory.APIVersion))
	}
	if inventory.Generation == 0 {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, fmt.Errorf("inventory generation must be positive"))
	}
	if inventory.ObservedAt.IsZero() {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, fmt.Errorf("inventory observation time is required"))
	}
	if inventory.FreshUntil != nil && (inventory.FreshUntil.IsZero() || inventory.FreshUntil.Before(inventory.ObservedAt)) {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, fmt.Errorf("inventory freshness deadline must not precede observation time"))
	}
	if len(inventory.Fabrics) > 0 {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorUnsupportedFeature, fmt.Errorf("cross-Node fabric annotations require SchedulerCache member resolution"))
	}

	facts, err := source.normalizeFacts(node, inventory)
	if err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}
	update := provider.ProviderNodeUpdate{
		ProviderID:          source.ID(),
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    inventory.Generation,
		ObservedAt:          inventory.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}
	if err := update.Validate(); err != nil {
		return provider.ProviderNodeUpdate{}, provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}
	return update, nil
}

// clearFacts proves that the provider inspected a Node with no inventory
// annotation and therefore has no facts to publish for it.
func (source *Provider) clearFacts(node *corev1.Node) (provider.ProviderNodeUpdate, error) {
	update := provider.ProviderNodeUpdate{
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

// normalizeFacts validates source-local identities and builds canonical device
// and domain IDs using the configured provider identity and surrounding Node.
func (source *Provider) normalizeFacts(node *corev1.Node, inventory inventory) (api.NodeTopologyFacts, error) {
	devicesByID := make(map[string]inventoryDevice, len(inventory.Devices))
	for _, device := range inventory.Devices {
		if device.ID == "" {
			return api.NodeTopologyFacts{}, fmt.Errorf("device ID is required")
		}
		if device.ResourceName == "" {
			return api.NodeTopologyFacts{}, fmt.Errorf("device %q has no resource name", device.ID)
		}
		if device.LocalDomain == "" {
			return api.NodeTopologyFacts{}, fmt.Errorf("device %q has no local domain", device.ID)
		}
		if device.Health != api.DeviceHealthy && device.Health != api.DeviceUnhealthy {
			return api.NodeTopologyFacts{}, fmt.Errorf("device %q has unsupported health %q", device.ID, device.Health)
		}
		if _, exists := devicesByID[device.ID]; exists {
			return api.NodeTopologyFacts{}, fmt.Errorf("duplicate device %q", device.ID)
		}
		devicesByID[device.ID] = device
	}

	domainsByID := make(map[string]inventoryLocalDomain, len(inventory.LocalDomains))
	domainByDevice := make(map[string]string, len(inventory.Devices))
	for _, domain := range inventory.LocalDomains {
		if domain.ID == "" {
			return api.NodeTopologyFacts{}, fmt.Errorf("local domain ID is required")
		}
		if _, exists := domainsByID[domain.ID]; exists {
			return api.NodeTopologyFacts{}, fmt.Errorf("duplicate local domain %q", domain.ID)
		}
		if len(domain.Devices) == 0 {
			return api.NodeTopologyFacts{}, fmt.Errorf("local domain %q has no devices", domain.ID)
		}
		domainDevices := make(map[string]struct{}, len(domain.Devices))
		for _, deviceID := range domain.Devices {
			if deviceID == "" {
				return api.NodeTopologyFacts{}, fmt.Errorf("local domain %q contains an empty device ID", domain.ID)
			}
			if _, exists := devicesByID[deviceID]; !exists {
				return api.NodeTopologyFacts{}, fmt.Errorf("local domain %q references unknown device %q", domain.ID, deviceID)
			}
			if _, exists := domainDevices[deviceID]; exists {
				return api.NodeTopologyFacts{}, fmt.Errorf("local domain %q contains duplicate device %q", domain.ID, deviceID)
			}
			domainDevices[deviceID] = struct{}{}
			if previousDomain, exists := domainByDevice[deviceID]; exists {
				return api.NodeTopologyFacts{}, fmt.Errorf("device %q belongs to both local domains %q and %q", deviceID, previousDomain, domain.ID)
			}
			domainByDevice[deviceID] = domain.ID
		}
		domainsByID[domain.ID] = domain
	}

	for deviceID, device := range devicesByID {
		domainID, exists := domainByDevice[deviceID]
		if !exists {
			return api.NodeTopologyFacts{}, fmt.Errorf("device %q is missing from local domain %q", deviceID, device.LocalDomain)
		}
		if domainID != device.LocalDomain {
			return api.NodeTopologyFacts{}, fmt.Errorf("device %q declares local domain %q but belongs to %q", deviceID, device.LocalDomain, domainID)
		}
	}

	domainValues := make([]string, 0, len(domainsByID))
	for domainID := range domainsByID {
		domainValues = append(domainValues, domainID)
	}
	sort.Strings(domainValues)

	domainIDs := make(map[string]api.DeviceDomainID, len(domainValues))
	localDomains := make([]api.DeviceDomain, 0, len(domainValues))
	for _, domainValue := range domainValues {
		domain := domainsByID[domainValue]
		deviceValues := append([]string(nil), domain.Devices...)
		sort.Strings(deviceValues)
		resourceName := corev1.ResourceName("")
		deviceIDs := make([]api.DeviceID, 0, len(deviceValues))
		for _, deviceValue := range deviceValues {
			device := devicesByID[deviceValue]
			if resourceName == "" {
				resourceName = device.ResourceName
			} else if resourceName != device.ResourceName {
				return api.NodeTopologyFacts{}, fmt.Errorf("local domain %q contains more than one resource type", domainValue)
			}
			deviceIDs = append(deviceIDs, source.deviceID(deviceValue))
		}
		domainID := source.domainID(node, domainValue)
		domainIDs[domainValue] = domainID
		localDomains = append(localDomains, api.DeviceDomain{
			ID:           domainID,
			NodeName:     node.Name,
			NodeUID:      node.UID,
			ResourceName: resourceName,
			DeviceIDs:    deviceIDs,
		})
	}

	deviceValues := make([]string, 0, len(devicesByID))
	for deviceID := range devicesByID {
		deviceValues = append(deviceValues, deviceID)
	}
	sort.Strings(deviceValues)

	healthyDevicesByResource := make(map[corev1.ResourceName]int)
	devices := make([]api.TopologyDevice, 0, len(deviceValues))
	for _, deviceValue := range deviceValues {
		device := devicesByID[deviceValue]
		if _, exists := healthyDevicesByResource[device.ResourceName]; !exists {
			healthyDevicesByResource[device.ResourceName] = 0
		}
		if device.Health == api.DeviceHealthy {
			healthyDevicesByResource[device.ResourceName]++
		}
		devices = append(devices, api.TopologyDevice{
			ID:            source.deviceID(deviceValue),
			ResourceName:  device.ResourceName,
			NodeName:      node.Name,
			NodeUID:       node.UID,
			LocalDomainID: domainIDs[device.LocalDomain],
			Health:        device.Health,
		})
	}
	for resourceName, healthyDeviceCount := range healthyDevicesByResource {
		allocatableQuantity := node.Status.Allocatable[resourceName]
		if allocatable := allocatableQuantity.Value(); allocatable != int64(healthyDeviceCount) {
			return api.NodeTopologyFacts{}, fmt.Errorf("Node allocatable %q is %d, but inventory has %d healthy devices", resourceName, allocatable, healthyDeviceCount)
		}
	}

	freshUntil := time.Time{}
	if inventory.FreshUntil != nil {
		freshUntil = *inventory.FreshUntil
	}
	return api.NodeTopologyFacts{
		ProviderID:          source.ID(),
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    inventory.Generation,
		ObservedAt:          inventory.ObservedAt,
		FreshUntil:          freshUntil,
		Devices:             devices,
		LocalDomains:        localDomains,
	}, nil
}

// deviceID builds the canonical identity for one source-owned device value.
func (source *Provider) deviceID(value string) api.DeviceID {
	return api.DeviceID{ProviderID: source.ID(), Namespace: source.identityNamespace, Value: value}
}

// domainID builds the canonical identity for a Node-local source domain value.
func (source *Provider) domainID(node *corev1.Node, value string) api.DeviceDomainID {
	return api.DeviceDomainID{ProviderID: source.ID(), Namespace: source.identityNamespace, NodeUID: node.UID, Value: value}
}

// decodeInventory strictly decodes exactly one versioned inventory JSON value.
func decodeInventory(raw string) (inventory, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded inventory
	if err := decoder.Decode(&decoded); err != nil {
		return inventory{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return inventory{}, fmt.Errorf("inventory contains more than one JSON value")
		}
		return inventory{}, err
	}
	return decoded, nil
}

type inventory struct {
	APIVersion   string                 `json:"apiVersion"`
	Generation   uint64                 `json:"generation"`
	ObservedAt   time.Time              `json:"observedAt"`
	FreshUntil   *time.Time             `json:"freshUntil,omitempty"`
	Devices      []inventoryDevice      `json:"devices"`
	LocalDomains []inventoryLocalDomain `json:"localDomains"`
	Fabrics      []inventoryFabric      `json:"fabrics"`
}

type inventoryDevice struct {
	ID           string              `json:"id"`
	ResourceName corev1.ResourceName `json:"resourceName"`
	LocalDomain  string              `json:"localDomain"`
	Health       api.DeviceHealth    `json:"health"`
}

type inventoryLocalDomain struct {
	ID      string   `json:"id"`
	Devices []string `json:"devices"`
}

type inventoryFabric struct {
	ID           string                  `json:"id"`
	OwnerNode    string                  `json:"ownerNode"`
	Members      []inventoryFabricMember `json:"members"`
	Interconnect string                  `json:"interconnect,omitempty"`
}

type inventoryFabricMember struct {
	Node         string   `json:"node"`
	LocalDomains []string `json:"localDomains"`
}
