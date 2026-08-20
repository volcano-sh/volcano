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

package cache

import (
	"context"
	"sync"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// HintRegistry stores the HintProviders declared by plugins, keyed by plugin
// name, so the UnschedulableJobCache can look up a plugin's events at Record
// time.
type HintRegistry struct {
	mu             sync.RWMutex
	eventsByPlugin map[string][]api.ClusterEventWithHint
}

// NewHintRegistry creates an empty HintRegistry.
func NewHintRegistry() *HintRegistry {
	return &HintRegistry{
		eventsByPlugin: make(map[string][]api.ClusterEventWithHint),
	}
}

// Register calls p.EventsToRegister once, then stores the returned slice under
// name, overwriting any previous entry for the same plugin. name must match
// Rejection.Plugin.
func (r *HintRegistry) Register(name string, p api.HintProvider) {
	if r == nil || p == nil {
		return
	}
	events, err := p.EventsToRegister(context.TODO())
	if err != nil {
		klog.Errorf("Failed to register hints for plugin %s: %v", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || len(events) == 0 {
		delete(r.eventsByPlugin, name)
		return
	}
	seen := make(map[api.ClusterEvent]struct{}, len(events))
	for _, event := range events {
		if (event.JobKeysFn == nil) != (event.EventKeysFn == nil) {
			klog.Errorf("Failed to register hints for plugin %s: event %v must provide JobKeysFn and EventKeysFn together", name, event.Event)
			delete(r.eventsByPlugin, name)
			return
		}
		if _, exists := seen[event.Event]; exists {
			klog.Errorf("Failed to register hints for plugin %s: duplicate event %v", name, event.Event)
			delete(r.eventsByPlugin, name)
			return
		}
		seen[event.Event] = struct{}{}
	}
	previousByEvent := make(map[api.ClusterEvent]api.ClusterEventWithHint, len(r.eventsByPlugin[name]))
	for _, event := range r.eventsByPlugin[name] {
		previousByEvent[event.Event] = event
	}
	for _, event := range events {
		previous, exists := previousByEvent[event.Event]
		if !exists {
			continue
		}
		wasIndexed := previous.JobKeysFn != nil && previous.EventKeysFn != nil
		isIndexed := event.JobKeysFn != nil && event.EventKeysFn != nil
		if wasIndexed != isIndexed {
			klog.Errorf("Failed to register hints for plugin %s: event %v changed HintKey indexing mode", name, event.Event)
			delete(r.eventsByPlugin, name)
			return
		}
	}
	r.eventsByPlugin[name] = append([]api.ClusterEventWithHint(nil), events...)
	klog.V(5).Infof("Registered %d hint event(s) for plugin %s", len(events), name)
}

// eventsForPlugin returns a snapshot of the events registered for the given
// plugin, or nil when the plugin has no HintProvider.
func (r *HintRegistry) eventsForPlugin(name string) []api.ClusterEventWithHint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events, ok := r.eventsByPlugin[name]
	if !ok {
		return nil
	}
	out := make([]api.ClusterEventWithHint, len(events))
	copy(out, events)
	return out
}
