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

// Package cache provides helpers for Volcano scheduler caches that coordinate shared informer
// handlers with local workqueues. The main use case is to wait until initial list/watch objects
// have been processed through the queue before starting scheduling; see
// https://github.com/volcano-sh/volcano/pull/5172 for background.
//
// Client-go v0.36+ requires ResourceEventHandlerRegistration to expose HasSyncedChecker in addition
// to HasSynced; types in this package that are stored in such maps are wrapped in
// initial_event_registration.go (NewInitialEventHandlerRegistration).
package cache

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

// QueueObjectWrapper is the element type for node/hypernode workqueues when initial-list events
// are processed asynchronously outside the informer Add callback.
type QueueObjectWrapper struct {
	Object          string
	IsInInitialList bool
}

// InitialEventAsyncHandlerTracker records which initial-list objects are still pending in the
// Volcano workqueue for a single informer handler registration.
//
// Flow (node/hypernode): the informer Add path enqueues work and calls Add(objectKey); the queue
// worker calls Done(objectKey) after handling. HasSynced is true only when the upstream informer
// registration reports synced and there are no pending keys in ObjectSet.
//
// This type intentionally does not implement client-go's ResourceEventHandlerRegistration: it only
// holds Volcano-side queue state. Callers that need a registration value (e.g. registeredHandlers)
// must use NewInitialEventHandlerRegistration with the same handler registration used to construct
// the tracker via NewQueueHandlerTracker.
type InitialEventAsyncHandlerTracker struct {
	// UpstreamHasSynced is bound from ResourceEventHandlerRegistration.HasSynced at construction;
	// it reports whether the shared informer has delivered initial sync for this handler.
	UpstreamHasSynced func() bool
	// ObjectSet holds object keys (or composite keys) still queued for initial processing.
	ObjectSet sets.Set[string]
	mu        sync.Mutex
}

// NewQueueHandlerTracker builds a tracker for one AddEventHandler registration. The handler
// argument MUST be the same registration later passed to NewInitialEventHandlerRegistration
// together with this tracker, so UpstreamHasSynced and HasSyncedChecker stay aligned.
func NewQueueHandlerTracker(handler cache.ResourceEventHandlerRegistration) *InitialEventAsyncHandlerTracker {
	return &InitialEventAsyncHandlerTracker{UpstreamHasSynced: handler.HasSynced, ObjectSet: sets.Set[string]{}}
}

// Add records that an object key was enqueued for initial handling and must be cleared with Done.
func (tracker *InitialEventAsyncHandlerTracker) Add(obj string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.ObjectSet.Insert(obj)
}

// Done records that the queue finished processing one initial object key.
func (tracker *InitialEventAsyncHandlerTracker) Done(obj string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.ObjectSet.Delete(obj)
}

// HasSynced reports whether it is safe to treat initial sync as complete for this handler:
// the upstream registration has synced AND every key passed to Add has been removed by Done.
func (tracker *InitialEventAsyncHandlerTracker) HasSynced() bool {
	if !tracker.UpstreamHasSynced() {
		return false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.ObjectSet.Len() == 0
}
