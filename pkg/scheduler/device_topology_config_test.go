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

package scheduler

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	_ "volcano.sh/volcano/pkg/scheduler/actions"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestConfigureDeviceTopologyLeavesAbsentConfigurationAsNoOp(t *testing.T) {
	scheduler := &Scheduler{}
	if err := scheduler.configureDeviceTopology(); err != nil {
		t.Fatalf("configureDeviceTopology() error = %v", err)
	}
	if !scheduler.deviceTopologyConfFrozen {
		t.Fatal("deviceTopologyConfFrozen = false, want startup-only configuration frozen")
	}
}

func TestConfigureDeviceTopologyInstallsAllocationAdapters(t *testing.T) {
	provider := conf.DeviceTopologyProviderConfiguration{
		ID:                "node-annotation-inventory",
		Type:              conf.DeviceTopologyProviderTypeAnnotation,
		IdentityNamespace: "nvidia-gpu-uuid-v1",
		Resources:         []corev1.ResourceName{"nvidia.com/gpu"},
	}
	tests := []struct {
		name     string
		adapters []conf.DeviceTopologyAdapterConfiguration
		wantErr  string
	}{
		{
			name:     "installs mock adapter",
			adapters: []conf.DeviceTopologyAdapterConfiguration{{Type: conf.DeviceTopologyAdapterTypeMock, Provider: provider.ID, Resource: "nvidia.com/gpu"}},
		},
		{
			name:     "rejects adapter for unknown provider",
			adapters: []conf.DeviceTopologyAdapterConfiguration{{Type: conf.DeviceTopologyAdapterTypeMock, Provider: "missing", Resource: "nvidia.com/gpu"}},
			wantErr:  "unknown provider",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := schedcache.NewDefaultMockSchedulerCache("device-topology-adapter-config")
			t.Cleanup(func() { schedcache.ShutdownMockSchedulerCache(cache) })
			scheduler := &Scheduler{
				cache:              cache,
				deviceTopologyConf: &conf.DeviceTopologyConfiguration{Providers: []conf.DeviceTopologyProviderConfiguration{provider}, Adapters: test.adapters},
			}

			err := scheduler.configureDeviceTopology()
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("configureDeviceTopology() error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("configureDeviceTopology() error = %v", err)
			}
			if !scheduler.deviceTopologyConfFrozen {
				t.Fatal("deviceTopologyConfFrozen = false, want startup-only configuration frozen")
			}
			if err := cache.ConfigureDeviceTopologyAdapters(nil); err == nil || !strings.Contains(err.Error(), "already configured") {
				t.Fatalf("second ConfigureDeviceTopologyAdapters() error = %v, want adapters already installed", err)
			}
		})
	}
}

func TestLoadSchedulerConfRejectsDeviceTopologyProviderChangesAfterStartup(t *testing.T) {
	configurationPath := filepath.Join(t.TempDir(), "scheduler.conf")
	if err := os.WriteFile(configurationPath, []byte(deviceTopologyProviderConfiguration("node-annotation-inventory", "enqueue")), 0o600); err != nil {
		t.Fatalf("write initial scheduler configuration: %v", err)
	}

	scheduler := &Scheduler{
		schedulerConf:      configurationPath,
		disableDefaultConf: true,
	}
	scheduler.loadSchedulerConf()
	initial := cloneDeviceTopologyConfiguration(scheduler.deviceTopologyConf)
	if initial == nil {
		t.Fatal("deviceTopologyConf = nil after initial configuration load")
	}

	scheduler.deviceTopologyConfFrozen = true
	if err := os.WriteFile(configurationPath, []byte(deviceTopologyProviderConfiguration("replacement-provider", "backfill")), 0o600); err != nil {
		t.Fatalf("write replacement scheduler configuration: %v", err)
	}
	scheduler.loadSchedulerConf()

	if !reflect.DeepEqual(scheduler.deviceTopologyConf, initial) {
		t.Fatalf("deviceTopologyConf = %#v, want unchanged %#v after startup", scheduler.deviceTopologyConf, initial)
	}
	if len(scheduler.actions) != 1 || scheduler.actions[0].Name() != "backfill" {
		t.Fatalf("actions = %#v, want dynamically reloaded backfill action", scheduler.actions)
	}
}

func deviceTopologyProviderConfiguration(providerID, action string) string {
	return `
actions: "` + action + `"
tiers: []
deviceTopology:
  providers:
  - id: ` + providerID + `
    type: annotation
    identityNamespace: nvidia-gpu-uuid-v1
    resources:
    - nvidia.com/gpu
  adapters:
  - type: mock
    provider: ` + providerID + `
    resource: nvidia.com/gpu
`
}
