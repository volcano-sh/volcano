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
	"testing"

	_ "volcano.sh/volcano/pkg/scheduler/actions"
)

func TestConfigureDeviceTopologyProvidersLeavesAbsentConfigurationAsNoOp(t *testing.T) {
	scheduler := &Scheduler{}
	if err := scheduler.configureDeviceTopologyProviders(); err != nil {
		t.Fatalf("configureDeviceTopologyProviders() error = %v", err)
	}
	if !scheduler.deviceTopologyConfFrozen {
		t.Fatal("deviceTopologyConfFrozen = false, want startup-only configuration frozen")
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
`
}
