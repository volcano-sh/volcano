package cache

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	toolscache "k8s.io/client-go/tools/cache"
)

type QueueObjectWrapper struct {
	Object          string
	IsInInitialList bool
}

// InitialEventAsyncHandlerTracker track the queue handling status. For initial event put in queue by event handler,
// use tracker to track whether the initial list handling is completed. In add event handler, call Add(obj) to
// add initial event in to tracker. After event in queue is handled, call Done(obj) to mark initial event handled.
type InitialEventAsyncHandlerTracker struct {
	reg       toolscache.ResourceEventHandlerRegistration
	ObjectSet sets.Set[string]
	mu        sync.Mutex
}

// NewQueueHandlerTracker create a tracker to track event handling in queue
func NewQueueHandlerTracker(handler toolscache.ResourceEventHandlerRegistration) *InitialEventAsyncHandlerTracker {
	return &InitialEventAsyncHandlerTracker{reg: handler, ObjectSet: sets.Set[string]{}}
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
	if !tracker.reg.HasSynced() {
		return false
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return tracker.ObjectSet.Len() == 0
}

// HasSyncedChecker implements toolscache.ResourceEventHandlerRegistration (client-go 1.36+).
func (tracker *InitialEventAsyncHandlerTracker) HasSyncedChecker() toolscache.DoneChecker {
	return &initialEventAsyncDoneChecker{t: tracker}
}

type initialEventAsyncDoneChecker struct {
	t *InitialEventAsyncHandlerTracker

	ch   chan struct{}
	once sync.Once
}

func (d *initialEventAsyncDoneChecker) Name() string {
	return d.t.reg.HasSyncedChecker().Name() + "+volcano-InitialEventAsyncHandlerTracker"
}

func (d *initialEventAsyncDoneChecker) Done() <-chan struct{} {
	d.once.Do(func() {
		d.ch = make(chan struct{})
		go func() {
			<-d.t.reg.HasSyncedChecker().Done()
			for !d.t.HasSynced() {
				time.Sleep(10 * time.Millisecond)
			}
			close(d.ch)
		}()
	})
	return d.ch
}
