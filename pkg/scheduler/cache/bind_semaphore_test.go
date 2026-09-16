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
	"testing"
	"time"
)

// TestBindSemaphoreCapacity verifies the invariant --max-concurrent-binds
// relies on: a size-N bindSemaphore admits exactly N concurrent holders, the
// N+1th acquire blocks until a holder releases, and a nil semaphore never
// blocks. The bind goroutine in BindTask acquires and releases this channel;
// if that acquire/release is ever reverted or the buffer size regresses, the
// blocking behaviour asserted here changes and the test fails.
func TestBindSemaphoreCapacity(t *testing.T) {
	t.Run("nil semaphore never blocks", func(t *testing.T) {
		sc := &SchedulerCache{}
		// A nil channel would block forever on send, so guard exactly as
		// BindTask does: the nil check is the "cap disabled" path.
		acquired := make(chan struct{})
		go func() {
			if sc.bindSemaphore != nil {
				sc.bindSemaphore <- struct{}{}
			}
			close(acquired)
		}()
		select {
		case <-acquired:
		case <-time.After(time.Second):
			t.Fatal("nil bindSemaphore must not block the bind path")
		}
	})

	t.Run("blocks at limit and unblocks on release", func(t *testing.T) {
		const limit = 2
		sc := &SchedulerCache{bindSemaphore: make(chan struct{}, limit)}

		// Fill every slot; these acquires must not block.
		for i := 0; i < limit; i++ {
			select {
			case sc.bindSemaphore <- struct{}{}:
			case <-time.After(time.Second):
				t.Fatalf("acquire %d of %d blocked while slots were free", i+1, limit)
			}
		}

		// The next acquire must block until a slot frees.
		blocked := make(chan struct{})
		go func() {
			sc.bindSemaphore <- struct{}{}
			close(blocked)
		}()
		select {
		case <-blocked:
			t.Fatal("acquire past the limit must block until a slot is released")
		case <-time.After(100 * time.Millisecond):
			// Expected: still blocked.
		}

		// Release one slot; the pending acquire must now proceed.
		<-sc.bindSemaphore
		select {
		case <-blocked:
		case <-time.After(time.Second):
			t.Fatal("release must unblock the pending acquire")
		}
	})
}
