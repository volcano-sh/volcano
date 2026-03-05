/*
Copyright 2026 The Kubernetes Authors.
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
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

type lifecycleTestAction struct {
	callOrder []string
}

func (f *lifecycleTestAction) Name() string {
	return "lifecycle-test"
}

func (f *lifecycleTestAction) Initialize() {
	f.callOrder = append(f.callOrder, "initialize")
}

func (f *lifecycleTestAction) Execute(ssn *framework.Session) {
	f.callOrder = append(f.callOrder, "execute")
}

func (f *lifecycleTestAction) UnInitialize() {
	f.callOrder = append(f.callOrder, "uninitialize")
}

func TestScheduler_ActionLifecycle(t *testing.T) {
	scheduler := &Scheduler{}
	testAction := &lifecycleTestAction{}

	scheduler.executeAction(testAction, nil)

	expected := []string{"initialize", "execute", "uninitialize"}

	if !reflect.DeepEqual(testAction.callOrder, expected) {
		t.Fatalf("unexpected lifecycle order: got %v, want %v",
			testAction.callOrder, expected)
	}
}
