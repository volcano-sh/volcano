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

package router

import (
	"testing"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
)

func TestForEachAdmissionAllowsEmptyEnabledAdmission(t *testing.T) {
	called := false
	err := ForEachAdmission(&options.Config{EnabledAdmission: ""}, func(service *AdmissionService) error {
		called = true
		return nil
	})

	if err != nil {
		t.Fatalf("expected empty enabled admission to be allowed, got %v", err)
	}
	if called {
		t.Fatal("expected handler not to be called when no admissions are enabled")
	}
}

func TestForEachAdmissionSkipsEmptyEntries(t *testing.T) {
	admissionMutex.Lock()
	originalAdmissionMap := admissionMap
	admissionMap = map[string]*AdmissionService{
		"/jobs/validate": {Path: "/jobs/validate"},
	}
	admissionMutex.Unlock()
	defer func() {
		admissionMutex.Lock()
		admissionMap = originalAdmissionMap
		admissionMutex.Unlock()
	}()

	called := 0
	err := ForEachAdmission(&options.Config{EnabledAdmission: " /jobs/validate, "}, func(service *AdmissionService) error {
		called++
		return nil
	})

	if err != nil {
		t.Fatalf("expected empty admission list entry to be skipped, got %v", err)
	}
	if called != 1 {
		t.Fatalf("expected handler to be called once, got %d", called)
	}
}
