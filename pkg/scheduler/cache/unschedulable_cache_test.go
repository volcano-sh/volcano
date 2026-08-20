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
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type fakeHintProvider struct {
	events []api.ClusterEventWithHint
}

func (p *fakeHintProvider) EventsToRegister(context.Context) ([]api.ClusterEventWithHint, error) {
	return p.events, nil
}

func newTestUnschedulableCache() (*UnschedulableJobCache, *HintRegistry) {
	registry := NewHintRegistry()
	cache := NewUnschedulableJobCache(registry, DefaultMaxSkipDuration)
	return cache, registry
}

func registerTestHint(registry *HintRegistry, plugin string, event api.ClusterEvent, hintFn api.JobHintFn) {
	registry.Register(plugin, &fakeHintProvider{events: []api.ClusterEventWithHint{
		{Event: event, HintFn: hintFn},
	}})
}

func registerTestIndexedHint(
	registry *HintRegistry,
	plugin string,
	event api.ClusterEvent,
	jobKeysFn api.JobKeysFn,
	eventKeysFn api.EventKeysFn,
	hintFn api.JobHintFn,
) {
	registry.Register(plugin, &fakeHintProvider{events: []api.ClusterEventWithHint{{
		Event: event, JobKeysFn: jobKeysFn, EventKeysFn: eventKeysFn, HintFn: hintFn,
	}}})
}

func TestUnschedulableJobCacheRecordAndGet(t *testing.T) {
	rejections := []api.Rejection{{
		Plugin: "plugin",
		Source: api.RejectionPredicate,
		Tasks:  []api.TaskID{"task"},
	}}
	tests := []struct {
		name           string
		registerHints  bool
		wantRejections []api.Rejection
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
				registerTestHint(registry, "plugin", api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
			}

			cache.RecordUnschedulable(job, rejections)

			if got := cache.GetCachedRejections(job); !reflect.DeepEqual(got, test.wantRejections) {
				t.Fatalf("GetCachedRejections() = %#v, want %#v", got, test.wantRejections)
			}
		})
	}
}

func TestUnschedulableJobCacheDoesNotRecordAcrossMatchingSessionEvent(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	registerTestHint(registry, "plugin", event, func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
		return api.HintSkip, nil
	})

	cache.BeginSession()
	cache.OnEvent(event, nil, nil)
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

	if got := cache.GetCachedRejections(job); got != nil {
		t.Fatalf("GetCachedRejections() = %#v, want nil after a matching event occurred during the session", got)
	}
}

func TestUnschedulableJobCacheDoesNotRecordAfterJobInvalidationDuringSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", api.ClusterEvent{Resource: api.PodGroupEvent, ActionType: fwk.Update}, nil)

	cache.BeginSession()
	cache.InvalidateUnschedulable(job.UID)
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

	if got := cache.GetCachedRejections(job); got != nil {
		t.Fatalf("GetCachedRejections() = %#v, want nil after Job invalidation during the session", got)
	}
}

func TestUnschedulableJobCacheRecordsAfterInvalidationBeforeSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", api.ClusterEvent{Resource: api.PodGroupEvent, ActionType: fwk.Update}, nil)

	cache.InvalidateUnschedulable(job.UID)
	cache.BeginSession()
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

	if got := cache.GetCachedRejections(job); len(got) != 1 {
		t.Fatalf("GetCachedRejections() = %#v, want one rejection after a pre-session invalidation", got)
	}
}

func TestUnschedulableJobCacheForgetMissingRecordDoesNotInvalidateSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)

	cache.BeginSession()
	cache.ForgetUnschedulable(job.UID)
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

	if got := cache.GetCachedRejections(job); len(got) != 1 {
		t.Fatalf("GetCachedRejections() = %#v, want one rejection after forgetting a missing record", got)
	}
}

func TestUnschedulableJobCacheForgetExistingRecordDoesNotInvalidateSession(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	registerTestHint(registry, "plugin", api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
	rejections := []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}}
	cache.RecordUnschedulable(job, rejections)

	cache.BeginSession()
	cache.ForgetUnschedulable(job.UID)
	cache.RecordUnschedulable(job, rejections)

	if got := cache.GetCachedRejections(job); len(got) != 1 {
		t.Fatalf("GetCachedRejections() = %#v, want one rejection after forgetting an existing record", got)
	}
}

func TestUnschedulableJobCacheReplaceAndForget(t *testing.T) {
	nodeEvent := api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}
	podEvent := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	tests := []struct {
		name               string
		replaceUnsupported bool
		forget             bool
		event              api.ClusterEvent
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
			wake := func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				return api.HintWakeup, nil
			}
			registerTestHint(registry, "node-plugin", nodeEvent, wake)
			registerTestHint(registry, "pod-plugin", podEvent, wake)

			cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "node-plugin", Source: api.RejectionPredicate}})
			if test.replaceUnsupported {
				cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "unsupported", Source: api.RejectionPredicate}})
			} else {
				cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "pod-plugin", Source: api.RejectionPredicate}})
			}
			if test.forget {
				cache.ForgetUnschedulable(job.UID)
			}
			cache.OnEvent(test.event, nil, nil)

			gotCached := len(cache.GetCachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestUnschedulableJobCacheOnEvent(t *testing.T) {
	tests := []struct {
		name       string
		result     api.HintResult
		err        error
		wantCached bool
	}{
		{
			name:       "HintSkip keeps the record",
			result:     api.HintSkip,
			wantCached: true,
		},
		{
			name:   "HintWakeup removes the record",
			result: api.HintWakeup,
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
			hintFn := func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				hintCalls++
				return test.result, test.err
			}
			event := api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}
			registerTestHint(registry, "plugin", event, hintFn)
			cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})
			cache.OnEvent(event, nil, nil)

			if hintCalls != 1 {
				t.Fatalf("hint calls = %d, want 1", hintCalls)
			}
			gotCached := len(cache.GetCachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestUnschedulableJobCacheForgetExpired(t *testing.T) {
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
			registerTestHint(registry, "plugin", api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)
			cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

			cache.mu.Lock()
			cache.records[job.UID].retryAfter = time.Now().Add(test.retryAfter)
			cache.mu.Unlock()
			cache.forgetExpired()

			gotCached := len(cache.GetCachedRejections(job)) > 0
			if gotCached != test.wantCached {
				t.Fatalf("record cached = %v, want %v", gotCached, test.wantCached)
			}
		})
	}
}

func TestUnschedulableJobCacheUsesConfiguredMaxSkipDuration(t *testing.T) {
	job := api.NewJobInfo("job")
	registry := NewHintRegistry()
	const maxSkipDuration = 10 * time.Second
	cache := NewUnschedulableJobCache(registry, maxSkipDuration)
	registerTestHint(registry, "plugin", api.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add}, nil)

	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

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
