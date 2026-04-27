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
	"reflect"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

type lifecycleTestAction struct {
	name      string
	callOrder []string
}

func (f *lifecycleTestAction) Name() string {
	return f.name
}

func (f *lifecycleTestAction) Initialize() {
	f.callOrder = append(f.callOrder, "initialize")
}

func (f *lifecycleTestAction) Execute(ssn *framework.Session) {}

func (f *lifecycleTestAction) UnInitialize() {
	f.callOrder = append(f.callOrder, "uninitialize")
}

type mockWatcher struct {
	eventCh chan fsnotify.Event
	errCh   chan error
}

func (m *mockWatcher) Events() chan fsnotify.Event {
	return m.eventCh
}

func (m *mockWatcher) Errors() chan error {
	return m.errCh
}

func (m *mockWatcher) Close() {}

func writeTempConfig(t *testing.T, content string) string {
	file, err := os.CreateTemp("", "scheduler-conf-*.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	file.Close()

	return file.Name()
}

func initActions(actions []framework.Action) {
	for _, action := range actions {
		action.Initialize()
	}
}

func cleanupActions(actions []framework.Action) {
	for _, action := range actions {
		action.UnInitialize()
	}
}

func TestScheduler_ActionLifecycle(t *testing.T) {
	testAction := &lifecycleTestAction{name: "test"}

	actions := []framework.Action{testAction}

	initActions(actions)
	cleanupActions(actions)

	expected := []string{"initialize", "uninitialize"}

	if !reflect.DeepEqual(testAction.callOrder, expected) {
		t.Fatalf("unexpected lifecycle order: got %v, want %v",
			testAction.callOrder, expected)
	}
}

func TestLoadSchedulerConf_Basic(t *testing.T) {
	config := `
actions: "enqueue"
`

	path := writeTempConfig(t, config)
	defer os.Remove(path)

	s := &Scheduler{
		schedulerConf: path,
	}

	actions, _, _, _, err := s.loadSchedulerConf()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(actions) == 0 {
		t.Fatalf("expected actions to be loaded")
	}
}

func TestWatchSchedulerConf_Reload(t *testing.T) {
	initialConfig := `
actions: "enqueue"
`

	path := writeTempConfig(t, initialConfig)
	defer os.Remove(path)

	s := &Scheduler{
		schedulerConf: path,
	}

	watcher := &mockWatcher{
		eventCh: make(chan fsnotify.Event, 1),
		errCh:   make(chan error),
	}
	s.fileWatcher = watcher

	stopCh := make(chan struct{})

	go s.watchSchedulerConf(stopCh)

	watcher.eventCh <- fsnotify.Event{Op: fsnotify.Write}

	time.Sleep(100 * time.Millisecond)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(s.actions) == 0 {
		t.Fatalf("expected actions to be loaded after reload")
	}

	close(stopCh)
}
