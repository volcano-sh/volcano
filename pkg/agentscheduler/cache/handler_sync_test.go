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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	kcache "k8s.io/client-go/tools/cache"
)

type mockHandlerRegistration struct {
	synced bool
}

func (m *mockHandlerRegistration) HasSynced() bool {
	return m.synced
}

func (m *mockHandlerRegistration) HasSyncedChecker() kcache.DoneChecker {
	ch := make(chan struct{})
	if m.synced {
		close(ch)
	}
	return &mockHandlerDoneChecker{name: "mockHandlerRegistration", ch: ch}
}

type mockHandlerDoneChecker struct {
	name string
	ch   chan struct{}
}

func (m *mockHandlerDoneChecker) Name() string {
	return m.name
}

func (m *mockHandlerDoneChecker) Done() <-chan struct{} {
	return m.ch
}

func TestWaitForHandlerSync_AllHandlersSynced(t *testing.T) {
	sc := &SchedulerCache{
		registeredHandlers: map[string]kcache.ResourceEventHandlerRegistration{
			"handler1": &mockHandlerRegistration{synced: true},
			"handler2": &mockHandlerRegistration{synced: true},
		},
		resourceSyncTimeout: 5 * time.Second,
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	incompleteHandlers, err := sc.waitForHandlerSync(stopCh)

	assert.NoError(t, err)
	assert.Empty(t, incompleteHandlers)
}

func TestWaitForHandlerSync_TimeoutReturnsSortedIncompleteHandlers(t *testing.T) {
	sc := &SchedulerCache{
		registeredHandlers: map[string]kcache.ResourceEventHandlerRegistration{
			"pod":   &mockHandlerRegistration{synced: false},
			"node":  &mockHandlerRegistration{synced: false},
			"queue": &mockHandlerRegistration{synced: true},
		},
		resourceSyncTimeout: 300 * time.Millisecond,
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	incompleteHandlers, err := sc.waitForHandlerSync(stopCh)

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, []string{"node", "pod"}, incompleteHandlers)
}

func TestWaitForHandlerSync_StopChanClosedBeforeSync(t *testing.T) {
	sc := &SchedulerCache{
		registeredHandlers: map[string]kcache.ResourceEventHandlerRegistration{
			"pod": &mockHandlerRegistration{synced: false},
		},
		resourceSyncTimeout: 5 * time.Second,
	}

	stopCh := make(chan struct{})
	close(stopCh)

	incompleteHandlers, err := sc.waitForHandlerSync(stopCh)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []string{"pod"}, incompleteHandlers)
}
