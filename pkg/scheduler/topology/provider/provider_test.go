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
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	providerTestID        = "test-provider"
	providerTestNamespace = "test.example"
)

var (
	providerTestResource   = corev1.ResourceName("nvidia.com/gpu")
	providerTestObservedAt = time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
)

func TestProviderNodeUpdateValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*ProviderNodeUpdate)
		wantError string
	}{
		{
			name: "valid replace facts",
		},
		{
			name: "valid clear facts",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = ClearFacts
				update.SourceGeneration = 0
				update.FreshUntil = time.Time{}
				update.Facts = nil
			},
		},
		{
			name: "missing provider ID",
			mutate: func(update *ProviderNodeUpdate) {
				update.ProviderID = ""
			},
			wantError: "provider ID is required",
		},
		{
			name: "missing Node identity",
			mutate: func(update *ProviderNodeUpdate) {
				update.NodeUID = ""
			},
			wantError: "Node name and UID are required",
		},
		{
			name: "missing Node resource version",
			mutate: func(update *ProviderNodeUpdate) {
				update.NodeResourceVersion = ""
			},
			wantError: "Node resource version is required",
		},
		{
			name: "missing observation time",
			mutate: func(update *ProviderNodeUpdate) {
				update.ObservedAt = time.Time{}
			},
			wantError: "observation time is required",
		},
		{
			name: "invalid freshness deadline",
			mutate: func(update *ProviderNodeUpdate) {
				update.FreshUntil = update.ObservedAt.Add(-time.Second)
				update.Facts.FreshUntil = update.FreshUntil
			},
			wantError: "freshness deadline precedes observation time",
		},
		{
			name: "unknown operation",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = "Unknown"
			},
			wantError: "unsupported provider update operation",
		},
		{
			name: "replace facts missing facts",
			mutate: func(update *ProviderNodeUpdate) {
				update.Facts = nil
			},
			wantError: "ReplaceFacts requires topology facts",
		},
		{
			name: "replace facts has zero generation",
			mutate: func(update *ProviderNodeUpdate) {
				update.SourceGeneration = 0
				update.Facts.SourceGeneration = 0
			},
			wantError: "ReplaceFacts source generation must be positive",
		},
		{
			name: "replace facts contain invalid topology",
			mutate: func(update *ProviderNodeUpdate) {
				update.Facts.Devices[0].Health = api.DeviceHealth("Unknown")
			},
			wantError: "invalid topology facts: device",
		},
		{
			name: "replace facts provider ID mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.ProviderID = "other-provider"
			},
			wantError: "topology facts provider ID does not match update",
		},
		{
			name: "replace facts identity mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.NodeUID = types.UID("different-node")
			},
			wantError: "topology facts Node identity does not match update",
		},
		{
			name: "replace facts resource version mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.NodeResourceVersion = "2"
			},
			wantError: "topology facts Node resource version does not match update",
		},
		{
			name: "replace facts source generation mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.SourceGeneration++
			},
			wantError: "topology facts source generation does not match update",
		},
		{
			name: "replace facts observation time mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.ObservedAt = update.ObservedAt.Add(time.Second)
			},
			wantError: "topology facts observation time does not match update",
		},
		{
			name: "replace facts freshness deadline mismatch",
			mutate: func(update *ProviderNodeUpdate) {
				update.FreshUntil = update.FreshUntil.Add(time.Second)
			},
			wantError: "topology facts freshness deadline does not match update",
		},
		{
			name: "clear facts has a generation",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = ClearFacts
				update.Facts = nil
				update.FreshUntil = time.Time{}
			},
			wantError: "ClearFacts source generation must be zero",
		},
		{
			name: "clear facts includes facts",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = ClearFacts
				update.SourceGeneration = 0
				update.FreshUntil = time.Time{}
			},
			wantError: "ClearFacts must not include topology facts",
		},
		{
			name: "clear facts has a freshness deadline",
			mutate: func(update *ProviderNodeUpdate) {
				update.Operation = ClearFacts
				update.SourceGeneration = 0
				update.Facts = nil
			},
			wantError: "ClearFacts must not include a freshness deadline",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			update := testProviderNodeUpdate()
			if test.mutate != nil {
				test.mutate(&update)
			}

			err := update.Validate()
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestProviderNodeUpdateClone(t *testing.T) {
	update := testProviderNodeUpdate()
	clone := update.Clone()

	update.Facts.Devices[0].ID.Value = "mutated-device"
	update.Facts.LocalDomains[0].DeviceIDs[0].Value = "mutated-device"

	if clone.Facts == nil || clone.Facts.Devices[0].ID.Value != "device-a" {
		t.Fatalf("Clone() facts = %#v, want independent device facts", clone.Facts)
	}
	if clone.Facts.LocalDomains[0].DeviceIDs[0].Value != "device-a" {
		t.Fatalf("Clone() domains = %#v, want independent domain members", clone.Facts.LocalDomains)
	}
}

func TestProviderNodeUpdateCloneClearFacts(t *testing.T) {
	update := testProviderNodeUpdate()
	update.Operation = ClearFacts
	update.SourceGeneration = 0
	update.FreshUntil = time.Time{}
	update.Facts = nil

	clone := update.Clone()
	if clone.Facts != nil {
		t.Fatalf("Clone() facts = %#v, want nil for ClearFacts", clone.Facts)
	}
	if clone.Operation != ClearFacts || clone.SourceGeneration != 0 || !clone.FreshUntil.IsZero() {
		t.Fatalf("Clone() = %#v, want an unchanged ClearFacts update", clone)
	}
}

func testProviderNodeUpdate() ProviderNodeUpdate {
	nodeUID := types.UID("node-a-uid")
	observedAt := providerTestObservedAt
	freshUntil := observedAt.Add(2 * time.Minute)
	domainID := api.DeviceDomainID{
		ProviderID: providerTestID,
		Namespace:  providerTestNamespace,
		NodeUID:    nodeUID,
		Value:      "domain-a",
	}
	deviceID := api.DeviceID{
		ProviderID: providerTestID,
		Namespace:  providerTestNamespace,
		Value:      "device-a",
	}
	facts := api.NodeTopologyFacts{
		ProviderID:          providerTestID,
		NodeName:            "node-a",
		NodeUID:             nodeUID,
		NodeResourceVersion: "1",
		SourceGeneration:    1,
		ObservedAt:          observedAt,
		FreshUntil:          freshUntil,
		Devices: []api.TopologyDevice{{
			ID:            deviceID,
			ResourceName:  providerTestResource,
			NodeName:      "node-a",
			NodeUID:       nodeUID,
			LocalDomainID: domainID,
			Health:        api.DeviceHealthy,
		}},
		LocalDomains: []api.DeviceDomain{{
			ID:           domainID,
			NodeName:     "node-a",
			NodeUID:      nodeUID,
			ResourceName: providerTestResource,
			DeviceIDs:    []api.DeviceID{deviceID},
		}},
	}
	return ProviderNodeUpdate{
		ProviderID:          providerTestID,
		NodeName:            facts.NodeName,
		NodeUID:             facts.NodeUID,
		NodeResourceVersion: facts.NodeResourceVersion,
		SourceGeneration:    facts.SourceGeneration,
		ObservedAt:          facts.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           ReplaceFacts,
		Facts:               &facts,
	}
}
