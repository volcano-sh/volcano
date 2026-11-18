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

package cache

import (
	"strings"
	"testing"
	"time"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
	"volcano.sh/volcano/pkg/scheduler/topology/provider/mock"
)

func TestSchedulerCacheDeviceTopologyAdapterConfigurationIsStartupOnly(t *testing.T) {
	cache := &SchedulerCache{}
	registry, err := adapter.NewRegistry([]adapter.AllocationAdapter{newTopologyReservationTestAdapter("nvidia.com/gpu")})
	if err != nil {
		t.Fatalf("adapter.NewRegistry() error = %v", err)
	}

	if err := cache.ConfigureDeviceTopologyAdapters(registry); err != nil {
		t.Fatalf("ConfigureDeviceTopologyAdapters() error = %v", err)
	}
	if cache.deviceTopologyAdapterRegistry != registry {
		t.Fatal("deviceTopologyAdapterRegistry was not installed")
	}
	if err := cache.ConfigureDeviceTopologyAdapters(registry); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("second ConfigureDeviceTopologyAdapters() error = %v, want already configured", err)
	}

	frozen := &SchedulerCache{}
	frozen.freezeDeviceTopologyProviders()
	if err := frozen.ConfigureDeviceTopologyAdapters(registry); err == nil || !strings.Contains(err.Error(), "after SchedulerCache starts") {
		t.Fatalf("frozen ConfigureDeviceTopologyAdapters() error = %v, want startup-only failure", err)
	}
}

func TestSchedulerCacheSyncNodeAppliesConfiguredDeviceTopologyProvider(t *testing.T) {
	cache := NewDefaultMockSchedulerCache("device-topology-provider-sync")
	t.Cleanup(func() { ShutdownMockSchedulerCache(cache) })

	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	if err := cache.nodeInformer.Informer().GetIndexer().Add(node); err != nil {
		t.Fatalf("add Node to informer = %v", err)
	}
	source, err := mock.New(topologyCacheTestProviderID, func() time.Time { return topologyCacheTestObservedAt })
	if err != nil {
		t.Fatalf("mock.New() error = %v", err)
	}
	if err := source.SetNodeUpdate(topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("SetNodeUpdate() error = %v", err)
	}
	if err := cache.ConfigureDeviceTopologyProviders(newTopologyTestProviderRegistry(t), []provider.NodeTopologyProvider{source}); err != nil {
		t.Fatalf("ConfigureDeviceTopologyProviders() error = %v", err)
	}

	if err := cache.SyncNode(node.Name); err != nil {
		t.Fatalf("SyncNode() error = %v", err)
	}
	snapshot := cache.DeviceTopologySnapshot()
	facts, found := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !found || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-a" {
		t.Fatalf("Facts() = (%#v, %t), want configured provider facts", facts, found)
	}
}

func TestSchedulerCacheDeviceTopologyProviderConfigurationIsStartupOnly(t *testing.T) {
	cache := &SchedulerCache{}
	registry := newTopologyTestProviderRegistry(t)
	source, err := mock.New(topologyCacheTestProviderID, nil)
	if err != nil {
		t.Fatalf("mock.New() error = %v", err)
	}

	if err := cache.ConfigureDeviceTopologyProviders(registry, []provider.NodeTopologyProvider{source}); err != nil {
		t.Fatalf("ConfigureDeviceTopologyProviders() error = %v", err)
	}
	if err := cache.ConfigureDeviceTopologyProviders(registry, []provider.NodeTopologyProvider{source}); err == nil || !strings.Contains(err.Error(), "already configured") {
		t.Fatalf("second ConfigureDeviceTopologyProviders() error = %v, want already configured", err)
	}
	cache.freezeDeviceTopologyProviders()
	if err := cache.ConfigureDeviceTopologyProviders(registry, []provider.NodeTopologyProvider{source}); err == nil || !strings.Contains(err.Error(), "after SchedulerCache starts") {
		t.Fatalf("frozen ConfigureDeviceTopologyProviders() error = %v, want startup-only failure", err)
	}
}
