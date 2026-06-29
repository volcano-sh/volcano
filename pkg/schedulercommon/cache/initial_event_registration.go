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
	"sync"
	"time"

	clientcache "k8s.io/client-go/tools/cache"
)

// initialEventHandlerRegistration is a thin adapter: HasSynced delegates to the tracker;
// HasSyncedChecker composes upstream's DoneChecker with tracker.HasSynced for callers that
// wait via client-go's WaitFor-style APIs.
type initialEventHandlerRegistration struct {
	upstream clientcache.ResourceEventHandlerRegistration
	tracker  *InitialEventAsyncHandlerTracker
}

// NewInitialEventHandlerRegistration returns a ResourceEventHandlerRegistration that exposes
// the same sync semantics as the given tracker for maps such as registeredHandlers.
//
// Preconditions (callers MUST satisfy):
//   - upstream is the handle returned by SharedInformer.AddEventHandler for this path.
//   - tracker was created with NewQueueHandlerTracker(upstream) — the same upstream instance.
//
// Violating the second rule breaks HasSynced / HasSyncedChecker: upstream sync and ObjectSet
// would refer to different handler registrations.
func NewInitialEventHandlerRegistration(upstream clientcache.ResourceEventHandlerRegistration, tracker *InitialEventAsyncHandlerTracker) clientcache.ResourceEventHandlerRegistration {
	return &initialEventHandlerRegistration{upstream: upstream, tracker: tracker}
}

func (w *initialEventHandlerRegistration) HasSynced() bool {
	return w.tracker.HasSynced()
}

func (w *initialEventHandlerRegistration) HasSyncedChecker() clientcache.DoneChecker {
	return &initialEventAsyncDoneChecker{upstream: w.upstream, tracker: w.tracker}
}

// initialEventAsyncDoneChecker implements clientcache.DoneChecker for the wrapped registration.
// Done closes after: (1) upstream's HasSyncedChecker fires, then (2) tracker.HasSynced is true.
// There is no channel from the workqueue; after (1) we poll tracker.HasSynced on a short interval
// until the queue drains — acceptable because this runs only during startup-style sync, not
// steady-state scheduling.
type initialEventAsyncDoneChecker struct {
	upstream clientcache.ResourceEventHandlerRegistration
	tracker  *InitialEventAsyncHandlerTracker

	ch   chan struct{}
	once sync.Once
}

func (d *initialEventAsyncDoneChecker) Name() string {
	return d.upstream.HasSyncedChecker().Name() + "+volcano-InitialEventAsyncHandlerTracker"
}

func (d *initialEventAsyncDoneChecker) Done() <-chan struct{} {
	d.once.Do(func() {
		d.ch = make(chan struct{})
		go func() {
			<-d.upstream.HasSyncedChecker().Done()
			for !d.tracker.HasSynced() {
				time.Sleep(10 * time.Millisecond)
			}
			close(d.ch)
		}()
	})
	return d.ch
}
