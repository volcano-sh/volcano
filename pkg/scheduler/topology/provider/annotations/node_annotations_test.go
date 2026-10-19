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

package annotations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

const (
	annotationTestProviderID = "annotation-provider"
	annotationTestNamespace  = "inventory.example"
)

var annotationTestNow = time.Date(2026, time.August, 4, 12, 30, 0, 0, time.UTC)

func TestNew(t *testing.T) {
	tests := []struct {
		name              string
		config            Config
		wantError         bool
		wantObservationAt time.Time
	}{
		{
			name: "requires provider ID",
			config: Config{
				IdentityNamespace: annotationTestNamespace,
			},
			wantError: true,
		},
		{
			name: "requires identity namespace",
			config: Config{
				ProviderID: annotationTestProviderID,
			},
			wantError: true,
		},
		{
			name: "uses the default clock",
			config: Config{
				ProviderID:        annotationTestProviderID,
				IdentityNamespace: annotationTestNamespace,
			},
		},
		{
			name: "preserves explicit configuration",
			config: Config{
				ProviderID:        annotationTestProviderID,
				IdentityNamespace: annotationTestNamespace,
				Now: func() time.Time {
					return annotationTestNow
				},
			},
			wantObservationAt: annotationTestNow,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := New(test.config)
			if test.wantError {
				if err == nil {
					t.Fatal("New() error = nil, want error")
				}
				if source != nil {
					t.Fatalf("New() source = %#v, want nil", source)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if source.ID() != annotationTestProviderID {
				t.Fatalf("ID() = %q, want %q", source.ID(), annotationTestProviderID)
			}
			if source.identityNamespace != annotationTestNamespace {
				t.Fatalf("identityNamespace = %q, want %q", source.identityNamespace, annotationTestNamespace)
			}
			if source.now == nil {
				t.Fatal("now = nil, want default or configured clock")
			}
			if !test.wantObservationAt.IsZero() && !source.now().Equal(test.wantObservationAt) {
				t.Fatalf("now() = %s, want %s", source.now(), test.wantObservationAt)
			}
		})
	}
}

func TestProviderSyncNodeNormalizesValidInventory(t *testing.T) {
	source := newTestProvider(t)
	node := testNode()
	node.Annotations = map[string]string{AnnotationKey: validInventory}

	update, err := source.SyncNode(context.Background(), node)
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if err := update.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if update.Operation != provider.ReplaceFacts {
		t.Fatalf("Operation = %q, want %q", update.Operation, provider.ReplaceFacts)
	}
	if update.ProviderID != annotationTestProviderID || update.NodeName != node.Name || update.NodeUID != node.UID || update.NodeResourceVersion != node.ResourceVersion {
		t.Fatalf("update metadata = %#v, want Node metadata from %q", update, node.Name)
	}
	if update.SourceGeneration != 42 {
		t.Fatalf("SourceGeneration = %d, want 42", update.SourceGeneration)
	}
	wantObservedAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	wantFreshUntil := wantObservedAt.Add(5 * time.Minute)
	if !update.ObservedAt.Equal(wantObservedAt) || !update.FreshUntil.Equal(wantFreshUntil) {
		t.Fatalf("update times = (%s, %s), want (%s, %s)", update.ObservedAt, update.FreshUntil, wantObservedAt, wantFreshUntil)
	}
	if update.Facts == nil {
		t.Fatal("Facts = nil, want normalized facts")
	}
	facts := update.Facts
	if len(facts.Devices) != 2 || facts.Devices[0].ID.Value != "nvidia://GPU-a" || facts.Devices[1].ID.Value != "nvidia://GPU-b" {
		t.Fatalf("Devices = %#v, want stable device ID order", facts.Devices)
	}
	if facts.Devices[0].ID.ProviderID != annotationTestProviderID || facts.Devices[0].ID.Namespace != annotationTestNamespace {
		t.Fatalf("device identity = %#v, want configured provider and namespace", facts.Devices[0].ID)
	}
	if len(facts.LocalDomains) != 2 || facts.LocalDomains[0].ID.Value != "nvlink-a" || facts.LocalDomains[1].ID.Value != "nvlink-b" {
		t.Fatalf("LocalDomains = %#v, want stable domain ID order", facts.LocalDomains)
	}
	if facts.LocalDomains[0].ID.NodeUID != node.UID || facts.Devices[0].LocalDomainID != facts.LocalDomains[0].ID {
		t.Fatalf("canonical Node-local identities = (%#v, %#v), want Node UID %q and matching domain", facts.LocalDomains[0].ID, facts.Devices[0].LocalDomainID, node.UID)
	}
	if facts.Devices[1].Health != api.DeviceUnhealthy {
		t.Fatalf("unhealthy device health = %q, want %q", facts.Devices[1].Health, api.DeviceUnhealthy)
	}
}

func TestProviderSyncNodeReturnsClearFactsForMissingAnnotation(t *testing.T) {
	source := newTestProvider(t)
	update, err := source.SyncNode(context.Background(), testNode())
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if update.Operation != provider.ClearFacts || update.Facts != nil || update.SourceGeneration != 0 || !update.FreshUntil.IsZero() {
		t.Fatalf("update = %#v, want ClearFacts without topology metadata", update)
	}
	if !update.ObservedAt.Equal(annotationTestNow) {
		t.Fatalf("ObservedAt = %s, want %s", update.ObservedAt, annotationTestNow)
	}
	if err := update.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProviderSyncNodeReturnsClearFactsForEmptyAnnotation(t *testing.T) {
	source := newTestProvider(t)
	node := testNode()
	node.Annotations = map[string]string{AnnotationKey: ""}

	update, err := source.SyncNode(context.Background(), node)
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if update.Operation != provider.ClearFacts || update.Facts != nil || update.SourceGeneration != 0 || !update.FreshUntil.IsZero() {
		t.Fatalf("update = %#v, want ClearFacts without topology metadata", update)
	}
	if !update.ObservedAt.Equal(annotationTestNow) {
		t.Fatalf("ObservedAt = %s, want %s", update.ObservedAt, annotationTestNow)
	}
}

func TestProviderSyncNodeRejectsInvalidInventory(t *testing.T) {
	tests := []struct {
		name       string
		inventory  string
		mutateNode func(*corev1.Node)
		wantReason provider.SourceErrorReason
	}{
		{
			name:       "malformed JSON",
			inventory:  `{"apiVersion":`,
			wantReason: provider.SourceErrorMalformedPayload,
		},
		{
			name:       "unknown field",
			inventory:  strings.Replace(validInventory, `"fabrics": []`, `"unknown": true, "fabrics": []`, 1),
			wantReason: provider.SourceErrorMalformedPayload,
		},
		{
			name:       "unsupported schema",
			inventory:  strings.Replace(validInventory, APIVersion, "topology.volcano.sh/v1beta1", 1),
			wantReason: provider.SourceErrorUnsupportedSchema,
		},
		{
			name:       "duplicate device",
			inventory:  strings.Replace(validInventory, `"devices": [`, `"devices": [{"id":"nvidia://GPU-a","resourceName":"nvidia.com/gpu","localDomain":"nvlink-a","health":"Healthy"},`, 1),
			wantReason: provider.SourceErrorInvalidTopology,
		},
		{
			name:       "unknown domain member",
			inventory:  strings.Replace(validInventory, `"nvidia://GPU-b"]`, `"nvidia://GPU-missing"]`, 1),
			wantReason: provider.SourceErrorInvalidTopology,
		},
		{
			name:       "capacity mismatch",
			inventory:  validInventory,
			wantReason: provider.SourceErrorInvalidTopology,
			mutateNode: func(node *corev1.Node) {
				node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("2")
			},
		},
		{
			name:       "fabric requires cache resolution",
			inventory:  strings.Replace(validInventory, `"fabrics": []`, `"fabrics": [{"id":"fabric-a","ownerNode":"node-a","members":[{"node":"node-a","localDomains":["nvlink-a"]}]}]`, 1),
			wantReason: provider.SourceErrorUnsupportedFeature,
		},
		{
			name:      "incomplete Node identity",
			inventory: validInventory,
			mutateNode: func(node *corev1.Node) {
				node.UID = ""
			},
			wantReason: provider.SourceErrorInvalidNode,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := newTestProvider(t)
			node := testNode()
			node.Annotations = map[string]string{AnnotationKey: test.inventory}
			if test.mutateNode != nil {
				test.mutateNode(node)
			}

			update, err := source.SyncNode(context.Background(), node)
			if err == nil {
				t.Fatalf("SyncNode() update = %#v, want error", update)
			}
			assertSourceErrorReason(t, err, test.wantReason)
			if update != (provider.ProviderNodeUpdate{}) {
				t.Fatalf("SyncNode() update = %#v, want no partial update", update)
			}
		})
	}
}

func TestProviderSyncNodeRejectsInventoryInvariants(t *testing.T) {
	tests := []struct {
		name            string
		mutateInventory func(*inventory)
	}{
		{
			name: "requires positive generation",
			mutateInventory: func(fixture *inventory) {
				fixture.Generation = 0
			},
		},
		{
			name: "requires observation time",
			mutateInventory: func(fixture *inventory) {
				fixture.ObservedAt = time.Time{}
			},
		},
		{
			name: "rejects zero freshness deadline",
			mutateInventory: func(fixture *inventory) {
				freshUntil := time.Time{}
				fixture.FreshUntil = &freshUntil
			},
		},
		{
			name: "rejects freshness before observation",
			mutateInventory: func(fixture *inventory) {
				freshUntil := fixture.ObservedAt.Add(-time.Second)
				fixture.FreshUntil = &freshUntil
			},
		},
		{
			name: "requires device ID",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[0].ID = ""
			},
		},
		{
			name: "requires device resource name",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[0].ResourceName = ""
			},
		},
		{
			name: "requires device local domain",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[0].LocalDomain = ""
			},
		},
		{
			name: "rejects unknown device health",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[0].Health = api.DeviceHealth("Unknown")
			},
		},
		{
			name: "rejects duplicate devices",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices = append(fixture.Devices, fixture.Devices[0])
			},
		},
		{
			name: "requires local domain ID",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[0].ID = ""
			},
		},
		{
			name: "rejects duplicate local domains",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[1].ID = fixture.LocalDomains[0].ID
			},
		},
		{
			name: "requires local domain members",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[0].Devices = nil
			},
		},
		{
			name: "rejects empty local domain member",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[0].Devices = []string{""}
			},
		},
		{
			name: "rejects unknown local domain member",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[0].Devices = []string{"nvidia://GPU-missing"}
			},
		},
		{
			name: "rejects duplicate local domain member",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains[0].Devices = []string{"nvidia://GPU-b", "nvidia://GPU-b"}
			},
		},
		{
			name: "rejects device in two local domains",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains = append(fixture.LocalDomains, inventoryLocalDomain{ID: "nvlink-c", Devices: []string{"nvidia://GPU-a"}})
			},
		},
		{
			name: "requires every device in a local domain",
			mutateInventory: func(fixture *inventory) {
				fixture.LocalDomains = fixture.LocalDomains[:1]
			},
		},
		{
			name: "requires declared local domain to match membership",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[1].LocalDomain = "nvlink-b"
			},
		},
		{
			name: "rejects mixed resource types in one local domain",
			mutateInventory: func(fixture *inventory) {
				fixture.Devices[0].ResourceName = "example.com/npu"
				fixture.Devices[0].LocalDomain = "shared"
				fixture.Devices[1].LocalDomain = "shared"
				fixture.LocalDomains = []inventoryLocalDomain{{ID: "shared", Devices: []string{"nvidia://GPU-b", "nvidia://GPU-a"}}}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := validInventoryFixture()
			test.mutateInventory(&fixture)
			node := testNode()
			node.Annotations = map[string]string{AnnotationKey: mustEncodeInventory(t, fixture)}

			update, err := newTestProvider(t).SyncNode(context.Background(), node)
			if err == nil {
				t.Fatalf("SyncNode() update = %#v, want error", update)
			}
			assertSourceErrorReason(t, err, provider.SourceErrorInvalidTopology)
			if update != (provider.ProviderNodeUpdate{}) {
				t.Fatalf("SyncNode() update = %#v, want no partial update", update)
			}
		})
	}
}

func TestProviderSyncNodeRejectsStrictJSONPayloads(t *testing.T) {
	tests := []struct {
		name      string
		inventory string
	}{
		{
			name:      "trailing JSON value",
			inventory: mustEncodeInventory(t, validInventoryFixture()) + " {}",
		},
		{
			name:      "malformed timestamp",
			inventory: `{"apiVersion":"topology.volcano.sh/v1alpha1","generation":42,"observedAt":"not-a-timestamp","devices":[],"localDomains":[],"fabrics":[]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := testNode()
			node.Annotations = map[string]string{AnnotationKey: test.inventory}

			update, err := newTestProvider(t).SyncNode(context.Background(), node)
			if err == nil {
				t.Fatalf("SyncNode() update = %#v, want error", update)
			}
			assertSourceErrorReason(t, err, provider.SourceErrorMalformedPayload)
		})
	}
}

func TestProviderSyncNodeAcceptsInventoryWithoutFreshnessDeadline(t *testing.T) {
	fixture := validInventoryFixture()
	fixture.FreshUntil = nil
	node := testNode()
	node.Annotations = map[string]string{AnnotationKey: mustEncodeInventory(t, fixture)}

	update, err := newTestProvider(t).SyncNode(context.Background(), node)
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if !update.FreshUntil.IsZero() || !update.Facts.FreshUntil.IsZero() {
		t.Fatalf("FreshUntil = (%s, %s), want zero values", update.FreshUntil, update.Facts.FreshUntil)
	}
}

func TestProviderSyncNodeAcceptsSeparateResourceDomains(t *testing.T) {
	fixture := validInventoryFixture()
	fixture.Devices = []inventoryDevice{
		{ID: "nvidia://GPU-a", ResourceName: "nvidia.com/gpu", LocalDomain: "gpu-domain", Health: api.DeviceHealthy},
		{ID: "example://NPU-a", ResourceName: "example.com/npu", LocalDomain: "npu-domain", Health: api.DeviceHealthy},
	}
	fixture.LocalDomains = []inventoryLocalDomain{
		{ID: "npu-domain", Devices: []string{"example://NPU-a"}},
		{ID: "gpu-domain", Devices: []string{"nvidia://GPU-a"}},
	}
	node := testNodeWithAllocatable(corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"):  resource.MustParse("1"),
		corev1.ResourceName("example.com/npu"): resource.MustParse("1"),
	})
	node.Annotations = map[string]string{AnnotationKey: mustEncodeInventory(t, fixture)}

	update, err := newTestProvider(t).SyncNode(context.Background(), node)
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if len(update.Facts.LocalDomains) != 2 || update.Facts.LocalDomains[0].ResourceName != "nvidia.com/gpu" || update.Facts.LocalDomains[1].ResourceName != "example.com/npu" {
		t.Fatalf("LocalDomains = %#v, want independently normalized GPU and NPU domains", update.Facts.LocalDomains)
	}
}

func TestProviderSyncNodeSortsDeviceIDsWithinLocalDomain(t *testing.T) {
	fixture := validInventoryFixture()
	fixture.Devices[0].Health = api.DeviceHealthy
	fixture.Devices[0].LocalDomain = "nvlink-a"
	fixture.LocalDomains = []inventoryLocalDomain{{ID: "nvlink-a", Devices: []string{"nvidia://GPU-b", "nvidia://GPU-a"}}}
	node := testNodeWithAllocatable(corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
	})
	node.Annotations = map[string]string{AnnotationKey: mustEncodeInventory(t, fixture)}

	update, err := newTestProvider(t).SyncNode(context.Background(), node)
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	deviceIDs := update.Facts.LocalDomains[0].DeviceIDs
	if len(deviceIDs) != 2 || deviceIDs[0].Value != "nvidia://GPU-a" || deviceIDs[1].Value != "nvidia://GPU-b" {
		t.Fatalf("DeviceIDs = %#v, want stable source-value order", deviceIDs)
	}
}

func TestProviderSyncNodeHonorsCanceledContext(t *testing.T) {
	source := newTestProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := source.SyncNode(ctx, testNode())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SyncNode() error = %v, want context.Canceled", err)
	}
}

func TestProviderSyncNodeRejectsInvalidNodeMetadata(t *testing.T) {
	tests := []struct {
		name       string
		mutateNode func(*corev1.Node)
	}{
		{
			name: "nil Node",
			mutateNode: func(node *corev1.Node) {
				*node = corev1.Node{}
			},
		},
		{
			name: "missing name",
			mutateNode: func(node *corev1.Node) {
				node.Name = ""
			},
		},
		{
			name: "missing UID",
			mutateNode: func(node *corev1.Node) {
				node.UID = ""
			},
		},
		{
			name: "missing resource version",
			mutateNode: func(node *corev1.Node) {
				node.ResourceVersion = ""
			},
		},
	}

	source := newTestProvider(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.name == "nil Node" {
				update, err := source.SyncNode(context.Background(), nil)
				if err == nil {
					t.Fatalf("SyncNode() update = %#v, want error", update)
				}
				assertSourceErrorReason(t, err, provider.SourceErrorInvalidNode)
				return
			}

			node := testNode()
			test.mutateNode(node)
			update, err := source.SyncNode(context.Background(), node)
			if err == nil {
				t.Fatalf("SyncNode() update = %#v, want error", update)
			}
			assertSourceErrorReason(t, err, provider.SourceErrorInvalidNode)
		})
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	source, err := New(Config{
		ProviderID:        annotationTestProviderID,
		IdentityNamespace: annotationTestNamespace,
		Now: func() time.Time {
			return annotationTestNow
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return source
}

func testNode() *corev1.Node {
	return testNodeWithAllocatable(corev1.ResourceList{
		corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
	})
}

func testNodeWithAllocatable(allocatable corev1.ResourceList) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "node-a",
			UID:             types.UID("node-a-uid"),
			ResourceVersion: "17",
		},
		Status: corev1.NodeStatus{Allocatable: allocatable},
	}
}

func mustEncodeInventory(t *testing.T, fixture inventory) string {
	t.Helper()
	raw, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(raw)
}

func validInventoryFixture() inventory {
	observedAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(5 * time.Minute)
	return inventory{
		APIVersion: APIVersion,
		Generation: 42,
		ObservedAt: observedAt,
		FreshUntil: &freshUntil,
		Devices: []inventoryDevice{
			{ID: "nvidia://GPU-b", ResourceName: "nvidia.com/gpu", LocalDomain: "nvlink-b", Health: api.DeviceUnhealthy},
			{ID: "nvidia://GPU-a", ResourceName: "nvidia.com/gpu", LocalDomain: "nvlink-a", Health: api.DeviceHealthy},
		},
		LocalDomains: []inventoryLocalDomain{
			{ID: "nvlink-b", Devices: []string{"nvidia://GPU-b"}},
			{ID: "nvlink-a", Devices: []string{"nvidia://GPU-a"}},
		},
		Fabrics: []inventoryFabric{},
	}
}

func assertSourceErrorReason(t *testing.T, err error, want provider.SourceErrorReason) {
	t.Helper()
	var sourceError *provider.SourceError
	if !errors.As(err, &sourceError) {
		t.Fatalf("error %T = %v, want *provider.SourceError", err, err)
	}
	if sourceError.Reason != want {
		t.Fatalf("SourceError reason = %q, want %q", sourceError.Reason, want)
	}
}

const validInventory = `{
  "apiVersion": "topology.volcano.sh/v1alpha1",
  "generation": 42,
  "observedAt": "2026-08-04T12:00:00Z",
  "freshUntil": "2026-08-04T12:05:00Z",
  "devices": [
    {"id": "nvidia://GPU-b", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-b", "health": "Unhealthy"},
    {"id": "nvidia://GPU-a", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-a", "health": "Healthy"}
  ],
  "localDomains": [
    {"id": "nvlink-b", "devices": ["nvidia://GPU-b"]},
    {"id": "nvlink-a", "devices": ["nvidia://GPU-a"]}
  ],
  "fabrics": []
}`
