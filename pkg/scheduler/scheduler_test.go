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
	// not relevant for this test
}

func (f *lifecycleTestAction) UnInitialize() {
	f.callOrder = append(f.callOrder, "uninitialize")
}

func TestScheduler_ActionLifecycle(t *testing.T) {
	testAction := &lifecycleTestAction{}
	scheduler := &Scheduler{
		actions: []framework.Action{testAction},
	}
	scheduler.initActions()
	scheduler.cleanupActions()
	expected := []string{"initialize", "uninitialize"}
	if !reflect.DeepEqual(testAction.callOrder, expected) {
		t.Fatalf("unexpected lifecycle order: got %v, want %v",
			testAction.callOrder, expected)
	}
}
