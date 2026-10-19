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

package mock

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

const (
	mockTestProviderID = "mock-provider"
	mockTestNamespace  = "mock.example"
)

var mockTestNow = time.Date(2026, time.August, 4, 13, 0, 0, 0, time.UTC)

func TestProviderSyncNodeReturnsIndependentFixture(t *testing.T) {
	source := newTestProvider(t)
	if err := source.SetNodeUpdate(testNodeUpdate()); err != nil {
		t.Fatalf("SetNodeUpdate() error = %v", err)
	}

	update, err := source.SyncNode(context.Background(), testNode())
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if update.Facts == nil {
		t.Fatal("SyncNode() facts = nil, want fixture facts")
	}
	update.Facts.Devices[0].ID.Value = "mutated-device"

	again, err := source.SyncNode(context.Background(), testNode())
	if err != nil {
		t.Fatalf("second SyncNode() error = %v", err)
	}
	if again.Facts == nil || again.Facts.Devices[0].ID.Value != "device-a" {
		t.Fatalf("second SyncNode() facts = %#v, want independent fixture", again.Facts)
	}
}

func TestProviderSyncNodeReturnsClearFactsWithoutFixture(t *testing.T) {
	source := newTestProvider(t)
	update, err := source.SyncNode(context.Background(), testNode())
	if err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	if update.Operation != provider.ClearFacts || update.Facts != nil || update.SourceGeneration != 0 {
		t.Fatalf("SyncNode() update = %#v, want ClearFacts", update)
	}
	if !update.ObservedAt.Equal(mockTestNow) {
		t.Fatalf("ObservedAt = %s, want %s", update.ObservedAt, mockTestNow)
	}
}

func TestProviderFixturesValidateAndCanInjectErrors(t *testing.T) {
	source := newTestProvider(t)

	wrongProvider := testNodeUpdate()
	wrongProvider.ProviderID = "different-provider"
	if err := source.SetNodeUpdate(wrongProvider); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("SetNodeUpdate() error = %v, want provider mismatch", err)
	}

	invalid := testNodeUpdate()
	invalid.Facts = nil
	if err := source.SetNodeUpdate(invalid); err == nil || !strings.Contains(err.Error(), "requires topology facts") {
		t.Fatalf("SetNodeUpdate() error = %v, want validation failure", err)
	}

	injected := errors.New("temporary source failure")
	if err := source.SetNodeError(testNode().UID, injected); err != nil {
		t.Fatalf("SetNodeError() error = %v", err)
	}
	_, err := source.SyncNode(context.Background(), testNode())
	if !errors.Is(err, injected) {
		t.Fatalf("SyncNode() error = %v, want injected error", err)
	}

	source.RemoveNode(testNode().UID)
	update, err := source.SyncNode(context.Background(), testNode())
	if err != nil || update.Operation != provider.ClearFacts {
		t.Fatalf("SyncNode() after RemoveNode = (%#v, %v), want ClearFacts", update, err)
	}
}

func TestProviderSyncNodeRejectsCanceledContextAndInvalidNode(t *testing.T) {
	source := newTestProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.SyncNode(ctx, testNode()); !errors.Is(err, context.Canceled) {
		t.Fatalf("SyncNode() error = %v, want context.Canceled", err)
	}

	_, err := source.SyncNode(context.Background(), &corev1.Node{})
	var sourceError *provider.SourceError
	if !errors.As(err, &sourceError) || sourceError.Reason != provider.SourceErrorInvalidNode {
		t.Fatalf("SyncNode() error = %v, want InvalidNode source error", err)
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	source, err := New(mockTestProviderID, func() time.Time {
		return mockTestNow
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return source
}

func testNode() *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:            "node-a",
		UID:             types.UID("node-a-uid"),
		ResourceVersion: "17",
	}}
}

func testNodeUpdate() provider.ProviderNodeUpdate {
	node := testNode()
	observedAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	freshUntil := observedAt.Add(5 * time.Minute)
	domainID := api.DeviceDomainID{ProviderID: mockTestProviderID, Namespace: mockTestNamespace, NodeUID: node.UID, Value: "domain-a"}
	deviceID := api.DeviceID{ProviderID: mockTestProviderID, Namespace: mockTestNamespace, Value: "device-a"}
	facts := api.NodeTopologyFacts{
		ProviderID:          mockTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    1,
		ObservedAt:          observedAt,
		FreshUntil:          freshUntil,
		Devices: []api.TopologyDevice{{
			ID:            deviceID,
			ResourceName:  corev1.ResourceName("nvidia.com/gpu"),
			NodeName:      node.Name,
			NodeUID:       node.UID,
			LocalDomainID: domainID,
			Health:        api.DeviceHealthy,
		}},
		LocalDomains: []api.DeviceDomain{{
			ID:           domainID,
			NodeName:     node.Name,
			NodeUID:      node.UID,
			ResourceName: corev1.ResourceName("nvidia.com/gpu"),
			DeviceIDs:    []api.DeviceID{deviceID},
		}},
	}
	return provider.ProviderNodeUpdate{
		ProviderID:          mockTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    facts.SourceGeneration,
		ObservedAt:          facts.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}
}
