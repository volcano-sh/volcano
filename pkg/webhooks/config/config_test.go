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

package config

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	validAdmissionConf = `resourceGroups:
- resourceGroup: management
  object:
    key: namespace
    value:
    - mng-ns-1
  schedulerName: default-scheduler
`
	invalidAdmissionConf = "resourceGroups: [\n"
)

func resGroupNames(conf *AdmissionConfiguration) []string {
	conf.Lock()
	defer conf.Unlock()
	var names []string
	for _, group := range conf.ResGroupsConfig {
		names = append(names, group.ResourceGroup)
	}
	return names
}

func TestLoadAdmissionConfEmptyPath(t *testing.T) {
	if conf := LoadAdmissionConf(""); conf != nil {
		t.Errorf("expect nil configuration for empty path, got %v", conf)
	}
}

func TestLoadAdmissionConfReloadAfterInitialFailure(t *testing.T) {
	admissionConf.ResGroupsConfig = nil
	t.Cleanup(func() { admissionConf.ResGroupsConfig = nil })

	confPath := filepath.Join(t.TempDir(), "volcano-admission.conf")
	writeConf := func(content string) {
		if err := os.WriteFile(confPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write admission configuration: %v", err)
		}
	}

	// The webhook manager keeps the configuration returned at startup, so it
	// must stay usable even if the first load fails.
	writeConf(invalidAdmissionConf)
	conf := LoadAdmissionConf(confPath)
	if conf == nil {
		t.Fatalf("expect non-nil configuration after failed initial load")
	}
	if names := resGroupNames(conf); len(names) != 0 {
		t.Fatalf("expect no resource groups after failed initial load, got %v", names)
	}

	writeConf(validAdmissionConf)
	LoadAdmissionConf(confPath)
	if names := resGroupNames(conf); len(names) != 1 || names[0] != "management" {
		t.Fatalf("expect reload to update startup configuration, got %v", names)
	}

	writeConf(invalidAdmissionConf)
	if reloaded := LoadAdmissionConf(confPath); reloaded != conf {
		t.Fatalf("expect failed reload to return the shared configuration")
	}
	if names := resGroupNames(conf); len(names) != 1 || names[0] != "management" {
		t.Fatalf("expect failed reload to keep previous configuration, got %v", names)
	}
}

func TestLoadAdmissionConfMissingFile(t *testing.T) {
	admissionConf.ResGroupsConfig = nil
	t.Cleanup(func() { admissionConf.ResGroupsConfig = nil })

	conf := LoadAdmissionConf(filepath.Join(t.TempDir(), "missing.conf"))
	if conf == nil {
		t.Fatalf("expect non-nil configuration when file is missing")
	}
	if names := resGroupNames(conf); len(names) != 0 {
		t.Errorf("expect no resource groups when file is missing, got %v", names)
	}
}
