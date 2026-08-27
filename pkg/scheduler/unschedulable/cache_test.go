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

package unschedulable

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type fakeHintProvider struct {
	events []EventWithHint
}

func (p *fakeHintProvider) EventsToRegister(context.Context) ([]EventWithHint, error) {
	return p.events, nil
}

func newTestUnschedulableCache() (*JobCache, *hintRegistry) {
	cache := NewJobCache(DefaultMaxSkipDuration)
	return cache, cache.registry
}

func registerTestHint(registry *hintRegistry, plugin string, event fwk.ClusterEvent, hintFn JobHintFn) {
	registry.register(plugin, &fakeHintProvider{events: []EventWithHint{
		{Event: event, HintFn: hintFn},
	}})
}

func registerTestIndexedHint(
	registry *hintRegistry,
	plugin string,
	event fwk.ClusterEvent,
	jobKeysFn JobKeysFn,
	eventKeysFn EventKeysFn,
	hintFn JobHintFn,
) {
	registry.register(plugin, &fakeHintProvider{events: []EventWithHint{{
		Event: event, JobKeysFn: jobKeysFn, EventKeysFn: eventKeysFn, HintFn: hintFn,
	}}})
}

func TestJobCacheRecordAndGet(t *testing.T) {
	rejections := []Rejection{{
		Plugin: "plugin",
		Source: RejectionPredicate,
		Tasks:  []api.TaskID{"task"},
	}}
	tests := []struct {
		name           string
		registerHints  bool
		wantRejections []Rejection
	}{
		{
			name:           "returns recorded rejections",
			registerHints:  true,
			wantRejections: rejections,
		},
		{
			name: "does not cache rejection without hints",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := api.NewJobInfo("job")
			cache, registry := newTestUnschedulableCache()
			if test.registerHints {
				registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
			}

			cache.Record(job, rejections)

			if got := cache.CachedRejections(job); !reflect.DeepEqual(got, test.wantRejections) {
				t.Fatalf("CachedRejections() = %#v, want %#v", got, test.wantRejections)
			}
		})
	}
}

func TestJobCacheDoesNotRecordAcrossMatchingSessionEvent(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	registerTestHint(registry, "plugin", event, func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
		return HintSkip, nil
	})

	cache.BeginSession()
	cache.OnEvent(event, nil, nil)
	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	if got := cache.CachedRejections(job); got != nil {
		t.Fatalf("CachedRejections() = %#v, want nil after a matching event occurred during the session", got)
	}
}

func TestJobCacheDoesNotRecordAfterJobInvalidationDuringSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: PodGroupEvent, ActionType: fwk.Update}, nil)

	cache.BeginSession()
	cache.Invalidate(job.UID)
	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	if got := cache.CachedRejections(job); got != nil {
		t.Fatalf("CachedRejections() = %#v, want nil after Job invalidation during the session", got)
	}
}

func TestJobCacheRecordsAfterInvalidationBeforeSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: PodGroupEvent, ActionType: fwk.Update}, nil)

	cache.Invalidate(job.UID)
	cache.BeginSession()
	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	if got := cache.CachedRejections(job); len(got) != 1 {
		t.Fatalf("CachedRejections() = %#v, want one rejection after a pre-session invalidation", got)
	}
}

func TestJobCacheForgetMissingRecordDoesNotInvalidateSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)

	cache.BeginSession()
	cache.Forget(job.UID)
	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	if got := cache.CachedRejections(job); len(got) != 1 {
		t.Fatalf("CachedRejections() = %#v, want one rejection after forgetting a missing record", got)
	}
}

func TestJobCacheForgetExistingRecordDoesNotInvalidateSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
	rejections := []Rejection{{Plugin: "plugin", Source: RejectionPredicate}}
	cache.Record(job, rejections)

	cache.BeginSession()
	cache.Forget(job.UID)
	cache.Record(job, rejections)

	if got := cache.CachedRejections(job); len(got) != 1 {
		t.Fatalf("CachedRejections() = %#v, want one rejection after forgetting an existing record", got)
	}
}

func TestJobCacheReplaceAndForget(t *testing.T) {
	nodeEvent := fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}
	podEvent := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	tests := []struct {
		name               string
		replaceUnsupported bool
		forget             bool
		event              fwk.ClusterEvent
		wantCached         bool
	}{
		{
			name:       "replacement ignores old registered event",
			event:      nodeEvent,
			wantCached: true,
		},
		{
			name:  "replacement uses new registered event",
			event: podEvent,
		},
		{
			name:   "Forget removes the record",
			forget: true,
			event:  podEvent,
		},
		{
			name:               "unsupported replacement removes old record",
			replaceUnsupported: true,
			event:              nodeEvent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := api.NewJobInfo("job")
			cache, registry := newTestUnschedulableCache()
			wake := func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				return HintWakeup, nil
			}
			registerTestHint(registry, "node-plugin", nodeEvent, wake)
			registerTestHint(registry, "pod-plugin", podEvent, wake)

			cache.Record(job, []Rejection{{Plugin: "node-plugin", Source: RejectionPredicate}})
			if test.replaceUnsupported {
				cache.Record(job, []Rejection{{Plugin: "unsupported", Source: RejectionPredicate}})
			} else {
				cache.Record(job, []Rejection{{Plugin: "pod-plugin", Source: RejectionPredicate}})
			}
			if test.forget {
				cache.Forget(job.UID)
			}
			cache.OnEvent(test.event, nil, nil)

			gotCached := len(cache.CachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestJobCacheOnEvent(t *testing.T) {
	tests := []struct {
		name       string
		result     HintResult
		err        error
		wantCached bool
	}{
		{
			name:       "HintSkip keeps the record",
			result:     HintSkip,
			wantCached: true,
		},
		{
			name:   "HintWakeup removes the record",
			result: HintWakeup,
		},
		{
			name: "hint error removes the record",
			err:  errors.New("hint failed"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := api.NewJobInfo("job")
			cache, registry := newTestUnschedulableCache()

			hintCalls := 0
			hintFn := func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				hintCalls++
				return test.result, test.err
			}
			event := fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}
			registerTestHint(registry, "plugin", event, hintFn)
			cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
			cache.OnEvent(event, nil, nil)

			if hintCalls != 1 {
				t.Fatalf("hint calls = %d, want 1", hintCalls)
			}
			gotCached := len(cache.CachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestJobCacheForgetExpired(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantCached bool
	}{
		{
			name:       "keeps active record",
			retryAfter: time.Hour,
			wantCached: true,
		},
		{
			name:       "removes expired record",
			retryAfter: -time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job := api.NewJobInfo("job")
			cache, registry := newTestUnschedulableCache()
			registerTestHint(registry, "plugin", fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
			cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

			cache.mu.Lock()
			cache.records[job.UID].retryAfter = time.Now().Add(test.retryAfter)
			cache.mu.Unlock()
			cache.forgetExpired()

			gotCached := len(cache.CachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestJobCacheUsesConfiguredMaxSkipDuration(t *testing.T) {
	job := api.NewJobInfo("job")
	const maxSkipDuration = 10 * time.Second
	cache := NewJobCache(maxSkipDuration)
	registerTestHint(cache.registry, "plugin", fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)

	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	cache.mu.RLock()
	defer cache.mu.RUnlock()
	record := cache.records[job.UID]
	if record == nil {
		t.Fatal("expected Job to be cached")
	}
	if got := record.retryAfter.Sub(record.lastFailedAt); got != maxSkipDuration {
		t.Fatalf("retry duration = %v, want %v", got, maxSkipDuration)
	}
}
