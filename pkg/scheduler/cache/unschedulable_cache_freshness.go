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

import "volcano.sh/volcano/pkg/scheduler/api"

// freshnessTracker prevents session results from being cached when they were
// computed from a snapshot older than a relevant cluster event or direct Job
// invalidation. Its methods must be called while UnschedulableJobCache.mu is
// held.
type freshnessTracker struct {
	currentGeneration   uint64
	sessionGeneration   uint64
	sessionStarted      bool
	lastEventGeneration map[api.ClusterEvent]uint64
	lastJobInvalidation map[api.JobID]uint64
}

func newFreshnessTracker() freshnessTracker {
	return freshnessTracker{
		lastEventGeneration: make(map[api.ClusterEvent]uint64),
		lastJobInvalidation: make(map[api.JobID]uint64),
	}
}

// beginSession marks the generation immediately before the scheduler takes its
// session snapshot. Later changes make conclusions from that snapshot stale.
func (t *freshnessTracker) beginSession() {
	t.sessionGeneration = t.currentGeneration
	t.sessionStarted = true
	clear(t.lastJobInvalidation)
}

func (t *freshnessTracker) recordEvent(event api.ClusterEvent) {
	t.currentGeneration++
	t.lastEventGeneration[event] = t.currentGeneration
}

func (t *freshnessTracker) recordJobInvalidation(jobID api.JobID) {
	t.currentGeneration++
	t.lastJobInvalidation[jobID] = t.currentGeneration
}

func (t *freshnessTracker) jobChangedAfterSessionStart(jobID api.JobID) bool {
	return t.lastJobInvalidation[jobID] > t.sessionGeneration
}

func (t *freshnessTracker) matchingEventOccurredAfterSessionStart(eventHints []pluginEventHint) bool {
	if !t.sessionStarted {
		return false
	}
	for event, generation := range t.lastEventGeneration {
		if generation <= t.sessionGeneration {
			continue
		}
		for _, eventHint := range eventHints {
			if eventMatches(eventHint.event, event) {
				return true
			}
		}
	}
	return false
}
