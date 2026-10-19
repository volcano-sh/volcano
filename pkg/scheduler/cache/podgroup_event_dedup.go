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

	"k8s.io/apimachinery/pkg/types"
)

type podGroupEventKey struct {
	eventType string
	reason    string
	message   string
}

type podGroupEventHistory struct {
	previous map[podGroupEventKey]struct{}
	current  map[podGroupEventKey]struct{}
}

// podGroupEventDeduper suppresses PodGroup events that repeat the ones recorded
// in the previous scheduling session. An event that stops being reported for a
// whole session is emitted again when it comes back.
type podGroupEventDeduper struct {
	sync.Mutex
	histories map[types.UID]*podGroupEventHistory
}

// shouldRecord reports whether the event should be sent to the API server and
// remembers it for the current session.
func (d *podGroupEventDeduper) shouldRecord(uid types.UID, eventType, reason, message string) bool {
	if uid == "" {
		return true
	}

	d.Lock()
	defer d.Unlock()

	if d.histories == nil {
		d.histories = make(map[types.UID]*podGroupEventHistory)
	}
	h, found := d.histories[uid]
	if !found {
		h = &podGroupEventHistory{}
		d.histories[uid] = h
	}

	key := podGroupEventKey{eventType: eventType, reason: reason, message: message}
	_, inPrevious := h.previous[key]
	_, inCurrent := h.current[key]
	if h.current == nil {
		h.current = make(map[podGroupEventKey]struct{})
	}
	h.current[key] = struct{}{}

	return !inPrevious && !inCurrent
}

// endSession marks the end of a scheduling session for the PodGroup.
func (d *podGroupEventDeduper) endSession(uid types.UID) {
	d.Lock()
	defer d.Unlock()

	h, found := d.histories[uid]
	if !found {
		return
	}
	if len(h.current) == 0 {
		delete(d.histories, uid)
		return
	}
	h.previous = h.current
	h.current = nil
}

func (d *podGroupEventDeduper) forget(uid types.UID) {
	d.Lock()
	defer d.Unlock()

	delete(d.histories, uid)
}
