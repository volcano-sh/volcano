package cache

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type QueueObjectWrapper struct {
	Object          string
	IsInInitialList bool
}

// InitialEventAsyncHandlerTracker track the queue handling status. For initial event put in queue by event handler,
// use tracker to track whether the initial list handling is completed. In add event handler, call Add(obj) to
// add initial event in to tracker. After event in queue is handled, call Done(obj) to mark initial event handled.
type InitialEventAsyncHandlerTracker struct {
	UpstreamHasSynced func() bool
	ObjectSet         sets.Set[string]
	mu                sync.Mutex
}

// NewQueueHandlerTracker create a tracker to track event handling in queue
func NewQueueHandlerTracker(handler cache.ResourceEventHandlerRegistration) *InitialEventAsyncHandlerTracker {
	return &InitialEventAsyncHandlerTracker{UpstreamHasSynced: handler.HasSynced, ObjectSet: sets.Set[string]{}}
}

// Add track the object to be handled from queue
func (tracker *InitialEventAsyncHandlerTracker) Add(obj string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.ObjectSet.Insert(obj)
}

// Done mark object has been handled in tracker
func (tracker *InitialEventAsyncHandlerTracker) Done(obj string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.ObjectSet.Delete(obj)
}

// HasSynced report if both the parent has synced and all
// tracked objects in queue have been handled
func (tracker *InitialEventAsyncHandlerTracker) HasSynced() bool {
	if !tracker.UpstreamHasSynced() {
		return false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.ObjectSet.Len() == 0
}
