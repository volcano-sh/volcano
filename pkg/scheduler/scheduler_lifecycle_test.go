/*
Copyright 2025 The Volcano Authors.

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"

	_ "volcano.sh/volcano/pkg/scheduler/actions"
)

// noopCache satisfies schedcache.Cache for lifecycle tests; only Run and
// SetMetricsConf are called by Scheduler.Run so the rest are left as no-ops
// via the embedded interface.
type noopCache struct {
	schedcache.Cache
}

func (n *noopCache) Run(_ <-chan struct{}) {}

func (n *noopCache) SetMetricsConf(_ map[string]string) {}

// lifecycleTestAction records whether Initialize and UnInitialize were called.
type lifecycleTestAction struct {
	name       string
	initDone   chan struct{}
	uninitDone chan struct{}
}

func (a *lifecycleTestAction) Name() string { return a.name }

func (a *lifecycleTestAction) Initialize() { close(a.initDone) }

func (a *lifecycleTestAction) Execute(_ *framework.Session) {}

func (a *lifecycleTestAction) UnInitialize() { close(a.uninitDone) }

func TestSchedulerActionLifecycle(t *testing.T) {
	// Ensure options.ServerOpts is non-nil so Run() doesn't panic on
	// the EnableCacheDumper check.
	if options.ServerOpts == nil {
		options.ServerOpts = options.NewServerOption()
	}

	action := &lifecycleTestAction{
		name:       "lifecycle-test",
		initDone:   make(chan struct{}),
		uninitDone: make(chan struct{}),
	}
	framework.RegisterAction(action)

	// Write a minimal scheduler config that references only our test action.
	confContent := "actions: \"lifecycle-test\"\ntiers: []\n"
	tmpFile, err := os.CreateTemp("", "scheduler-lifecycle-test-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	_, err = tmpFile.WriteString(confContent)
	assert.NoError(t, err)
	assert.NoError(t, tmpFile.Close())

	sched := &Scheduler{
		schedulerConf:  tmpFile.Name(),
		schedulePeriod: time.Hour, // prevent runOnce from firing during the test
		cache:          &noopCache{},
	}

	stopCh := make(chan struct{})
	sched.Run(stopCh)

	// Initialize must be called synchronously before Run returns.
	select {
	case <-action.initDone:
	case <-time.After(time.Second):
		t.Fatal("Initialize was not called after scheduler startup")
	}

	close(stopCh)

	// UnInitialize must be called after stopCh is closed.
	select {
	case <-action.uninitDone:
	case <-time.After(time.Second):
		t.Fatal("UnInitialize was not called after scheduler shutdown")
	}
}
