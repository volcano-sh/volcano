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

package unschedulable

import (
	"context"
	"sync"

	"k8s.io/klog/v2"
)

// hintRegistry stores the HintProviders declared by plugins, keyed by plugin
// name, so JobCache can look up a plugin's events at Record
// time.
type hintRegistry struct {
	mu             sync.RWMutex
	eventsByPlugin map[string][]EventWithHint
}

func newHintRegistry() *hintRegistry {
	return &hintRegistry{
		eventsByPlugin: make(map[string][]EventWithHint),
	}
}

// register calls p.EventsToRegister once, then stores the returned slice under
// pluginName, overwriting any previous entry for the same plugin. pluginName
// must match Rejection.Plugin.
func (r *hintRegistry) register(pluginName string, p HintProvider) {
	if r == nil || p == nil {
		return
	}
	events, err := p.EventsToRegister(context.TODO())
	if err != nil {
		klog.Errorf("Failed to register hints for plugin %s: %v", pluginName, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil || len(events) == 0 {
		delete(r.eventsByPlugin, pluginName)
		return
	}
	seen := make(map[eventKey]struct{}, len(events))
	for _, event := range events {
		if (event.JobKeysFn == nil) != (event.EventKeysFn == nil) {
			klog.Errorf("Failed to register hints for plugin %s: event %v must provide JobKeysFn and EventKeysFn together", pluginName, event.Event)
			delete(r.eventsByPlugin, pluginName)
			return
		}
		key := newEventKey(event.Event)
		if _, exists := seen[key]; exists {
			klog.Errorf("Failed to register hints for plugin %s: duplicate event %v", pluginName, event.Event)
			delete(r.eventsByPlugin, pluginName)
			return
		}
		seen[key] = struct{}{}
	}
	previousByEvent := make(map[eventKey]EventWithHint, len(r.eventsByPlugin[pluginName]))
	for _, event := range r.eventsByPlugin[pluginName] {
		previousByEvent[newEventKey(event.Event)] = event
	}
	for _, event := range events {
		previous, exists := previousByEvent[newEventKey(event.Event)]
		if !exists {
			continue
		}
		wasIndexed := previous.JobKeysFn != nil && previous.EventKeysFn != nil
		isIndexed := event.JobKeysFn != nil && event.EventKeysFn != nil
		if wasIndexed != isIndexed {
			klog.Errorf("Failed to register hints for plugin %s: event %v changed HintKey indexing mode", pluginName, event.Event)
			delete(r.eventsByPlugin, pluginName)
			return
		}
	}
	r.eventsByPlugin[pluginName] = append([]EventWithHint(nil), events...)
	klog.V(5).Infof("Registered %d hint event(s) for plugin %s", len(events), pluginName)
}

// eventsForPlugin returns a snapshot of the events registered for the given
// plugin, or nil when the plugin has no HintProvider.
func (r *hintRegistry) eventsForPlugin(pluginName string) []EventWithHint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	events, ok := r.eventsByPlugin[pluginName]
	if !ok {
		return nil
	}
	out := make([]EventWithHint, len(events))
	copy(out, events)
	return out
}
