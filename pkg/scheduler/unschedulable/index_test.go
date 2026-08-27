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
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// assertByHintKeyMembership checks whether jobID is present under key in any
// plugin/action index registered for event.Resource, so tests can prove that
// Replace, Forget, and watchdog expiry remove the stale HintKey index entry
// instead of only dropping the primary record.
func assertByHintKeyMembership(t *testing.T, cache *JobCache, event fwk.ClusterEvent, key HintKey, jobID api.JobID, want bool) {
	t.Helper()
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	var got bool
	for _, index := range cache.eventIndex.jobIndexesByResource[event.Resource] {
		if jobIDs, ok := index.jobs.jobIDsByHintKey[key]; ok && jobIDs.Has(jobID) {
			got = true
		}
	}
	if got != want {
		t.Fatalf("jobIDsByHintKey[%q] contains job %s = %v, want %v", key, jobID, got, want)
	}
}

// TestJobCacheByHintKeyIndexRemoval proves that the HintKey
// secondary index is kept consistent with the primary record on replacement,
// explicit Forget, and watchdog expiry: the stale key entry is removed so it
// can no longer dispatch, and (for replacement) the new key entry is present
// and does dispatch.
func TestJobCacheByHintKeyIndexRemoval(t *testing.T) {
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	// echoJobKeys/echoEventKey let each test control exactly which HintKey a
	// record or event carries while sharing one plugin/action index.
	echoJobKeys := func(_ *api.JobInfo, rejection Rejection) ([]HintKey, error) {
		return rejection.HintKeys, nil
	}
	echoEventKey := func(_ any, newObj any) ([]HintKey, error) {
		return []HintKey{HintKey(newObj.(string))}, nil
	}

	// keepAlive is recorded under its own stable key in the same plugin/action
	// index as the Job under test, so the index's allJobIDs set never empties.
	// Without this, deleting and recreating a single-Job index would mask a
	// hypothetical bug in the per-hintKey/per-job removal these tests target.
	newKeepAlive := func(cache *JobCache) {
		keepAlive := api.NewJobInfo("keep-alive")
		cache.Record(keepAlive, []Rejection{{Plugin: "plugin", Source: RejectionPredicate, HintKeys: []HintKey{"keep-alive-key"}}})
	}

	t.Run("replacement removes the stale key and the new key dispatches", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				return HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate, HintKeys: []HintKey{"old-key"}}})
		assertByHintKeyMembership(t, cache, event, "old-key", job.UID, true)

		cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate, HintKeys: []HintKey{"new-key"}}})
		assertByHintKeyMembership(t, cache, event, "old-key", job.UID, false)
		assertByHintKeyMembership(t, cache, event, "new-key", job.UID, true)

		cache.OnEvent(event, nil, "old-key")
		if got := len(cache.CachedRejections(job)); got == 0 {
			t.Fatalf("stale key dispatched and forgot the record; record cached = %d, want > 0", got)
		}

		cache.OnEvent(event, nil, "new-key")
		if got := len(cache.CachedRejections(job)); got != 0 {
			t.Fatalf("replacement key must dispatch and wake the record; record cached = %d, want 0", got)
		}
	})

	t.Run("explicit Forget removes the key entry", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				return HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate, HintKeys: []HintKey{"key"}}})
		assertByHintKeyMembership(t, cache, event, "key", job.UID, true)

		cache.Forget(job.UID)
		assertByHintKeyMembership(t, cache, event, "key", job.UID, false)

		// The stale key must not dispatch: the only Job left under it is
		// keep-alive, unaffected by job's removal.
		cache.OnEvent(event, nil, "key")
		if got := len(cache.CachedRejections(job)); got != 0 {
			t.Fatalf("forgotten job's record reappeared; record cached = %d, want 0", got)
		}
	})

	t.Run("watchdog expiry removes the key entry", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				return HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate, HintKeys: []HintKey{"key"}}})
		assertByHintKeyMembership(t, cache, event, "key", job.UID, true)

		cache.mu.Lock()
		cache.records[job.UID].retryAfter = time.Now().Add(-time.Second)
		cache.mu.Unlock()
		cache.forgetExpired()

		assertByHintKeyMembership(t, cache, event, "key", job.UID, false)

		cache.OnEvent(event, nil, "key")
		if got := len(cache.CachedRejections(job)); got != 0 {
			t.Fatalf("watchdog-expired job's record reappeared; record cached = %d, want 0", got)
		}
	})
}

func TestJobCacheSecondaryIndex(t *testing.T) {
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	overLimitEventKeys := make([]HintKey, MaxHintKeysPerPluginEvent+1)
	for i := range overLimitEventKeys {
		overLimitEventKeys[i] = HintKey(fmt.Sprintf("event-key-%d", i))
	}
	tests := []struct {
		name                string
		eventKeys           []HintKey
		eventErr            error
		jobBWithoutHintKeys bool
		wantCallsA          int
		wantCallsB          int
	}{
		{name: "matching indexed key dispatches only indexed match", eventKeys: []HintKey{"key-a"}, wantCallsA: 1},
		{name: "non-matching indexed key skips indexed jobs", eventKeys: []HintKey{"key-miss"}},
		{name: "job without HintKeys is dispatched beside indexed match", eventKeys: []HintKey{"key-a"}, jobBWithoutHintKeys: true, wantCallsA: 1, wantCallsB: 1},
		{name: "event extraction error dispatches every job in the plugin action index", eventErr: errors.New("bad event"), wantCallsA: 1, wantCallsB: 1},
		{name: "too many event keys dispatch every job in the plugin action index", eventKeys: overLimitEventKeys, wantCallsA: 1, wantCallsB: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache, registry := newTestUnschedulableCache()
			jobA := api.NewJobInfo("job-a")
			jobB := api.NewJobInfo("job-b")
			calls := map[api.JobID]int{}

			registerTestIndexedHint(
				registry,
				"plugin",
				event,
				func(job *api.JobInfo, _ Rejection) ([]HintKey, error) {
					switch job.UID {
					case jobA.UID:
						return []HintKey{"key-a"}, nil
					case jobB.UID:
						if test.jobBWithoutHintKeys {
							return nil, nil
						}
						return []HintKey{"key-b"}, nil
					default:
						return nil, fmt.Errorf("unexpected job %s", job.UID)
					}
				},
				func(any, any) ([]HintKey, error) {
					if test.eventErr != nil {
						return nil, test.eventErr
					}
					return append([]HintKey(nil), test.eventKeys...), nil
				},
				func(job *api.JobInfo, _ Rejection, _, _ any) (HintResult, error) {
					calls[job.UID]++
					return HintSkip, nil
				},
			)

			rejection := []Rejection{{Plugin: "plugin", Source: RejectionPredicate}}
			cache.Record(jobA, rejection)
			cache.Record(jobB, rejection)

			cache.OnEvent(event, nil, "event")

			if got := calls[jobA.UID]; got != test.wantCallsA {
				t.Fatalf("job-a hint calls = %d, want %d", got, test.wantCallsA)
			}
			if got := calls[jobB.UID]; got != test.wantCallsB {
				t.Fatalf("job-b hint calls = %d, want %d", got, test.wantCallsB)
			}
		})
	}
}

func TestJobCacheWildcardIndexMatchesConcreteResource(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	registeredEvent := fwk.ClusterEvent{Resource: fwk.WildCard, ActionType: fwk.Delete}
	incomingEvent := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	hintCalls := 0

	registerTestIndexedHint(
		registry,
		"plugin",
		registeredEvent,
		func(*api.JobInfo, Rejection) ([]HintKey, error) {
			return []HintKey{"shared-key"}, nil
		},
		func(any, any) ([]HintKey, error) {
			return []HintKey{"shared-key"}, nil
		},
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			hintCalls++
			return HintWakeup, nil
		},
	)
	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	cache.OnEvent(incomingEvent, nil, "event")

	if hintCalls != 1 {
		t.Fatalf("wildcard hint calls = %d, want 1 for a concrete Pod event", hintCalls)
	}
	if got := cache.CachedRejections(job); got != nil {
		t.Fatalf("CachedRejections() = %#v, want nil after the wildcard event wakes the Job", got)
	}
}

func TestJobCacheDoesNotCacheDuplicatePluginEvent(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	indexedCalls := 0
	noHintKeysCalls := 0

	registry.register("plugin", &fakeHintProvider{events: []EventWithHint{
		{
			Event: event,
			JobKeysFn: func(*api.JobInfo, Rejection) ([]HintKey, error) {
				return []HintKey{"match"}, nil
			},
			EventKeysFn: func(any, any) ([]HintKey, error) {
				return []HintKey{"match"}, nil
			},
			HintFn: func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				indexedCalls++
				return HintSkip, nil
			},
		},
		{
			Event: event,
			HintFn: func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
				noHintKeysCalls++
				return HintSkip, nil
			},
		},
	}})

	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
	if got := cache.CachedRejections(job); got != nil {
		t.Fatalf("CachedRejections() = %#v, want nil for a duplicate plugin event", got)
	}
	cache.OnEvent(event, nil, "event")

	if indexedCalls != 0 {
		t.Fatalf("indexed hint calls = %d, want 0", indexedCalls)
	}
	if noHintKeysCalls != 0 {
		t.Fatalf("hint calls without HintKeys = %d, want 0", noHintKeysCalls)
	}
}

func TestJobCacheJobsWithoutHintKeys(t *testing.T) {
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	tests := []struct {
		name        string
		jobKeysFn   JobKeysFn
		eventKeysFn EventKeysFn
		wantCalls   int
	}{
		{
			name:      "nil extractors dispatch the job without HintKeys",
			wantCalls: 1,
		},
		{
			name: "job extraction error dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, Rejection) ([]HintKey, error) {
				return nil, errors.New("bad job")
			},
			eventKeysFn: func(any, any) ([]HintKey, error) {
				return []HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "zero job keys dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, Rejection) ([]HintKey, error) {
				return []HintKey{}, nil
			},
			eventKeysFn: func(any, any) ([]HintKey, error) {
				return []HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "too many job keys dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, Rejection) ([]HintKey, error) {
				keys := make([]HintKey, MaxHintKeysPerPluginEvent+1)
				for i := range keys {
					keys[i] = HintKey(fmt.Sprintf("key-%d", i))
				}
				return keys, nil
			},
			eventKeysFn: func(any, any) ([]HintKey, error) {
				return []HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "duplicate keys dispatch once",
			jobKeysFn: func(*api.JobInfo, Rejection) ([]HintKey, error) {
				return []HintKey{"dup", "dup"}, nil
			},
			eventKeysFn: func(any, any) ([]HintKey, error) {
				return []HintKey{"dup", "dup"}, nil
			},
			wantCalls: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache, registry := newTestUnschedulableCache()
			job := api.NewJobInfo("job")
			hintCalls := 0
			registerTestIndexedHint(
				registry,
				"plugin",
				event,
				test.jobKeysFn,
				test.eventKeysFn,
				func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
					hintCalls++
					return HintSkip, nil
				},
			)

			cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
			cache.OnEvent(event, nil, "event")

			if hintCalls != test.wantCalls {
				t.Fatalf("hint calls = %d, want %d", hintCalls, test.wantCalls)
			}
		})
	}
}

func TestJobCacheNilHintDispatchesWithoutHintKeys(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		func(*api.JobInfo, Rejection) ([]HintKey, error) {
			return []HintKey{"job-key"}, nil
		},
		func(any, any) ([]HintKey, error) {
			return []HintKey{"different-event-key"}, nil
		},
		nil,
	)

	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
	cache.OnEvent(event, nil, "event")

	if got := cache.CachedRejections(job); got != nil {
		t.Fatalf("CachedRejections() = %#v, want nil after matching event with nil HintFn", got)
	}
}

func TestJobCacheClassifiedUpdate(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	calls := 0

	registerTestIndexedHint(
		registry,
		"plugin",
		fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Update},
		func(*api.JobInfo, Rejection) ([]HintKey, error) {
			return []HintKey{"scale-down"}, nil
		},
		func(any, any) ([]HintKey, error) {
			return []HintKey{"scale-down"}, nil
		},
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			calls++
			return HintSkip, nil
		},
	)

	cache.Record(job, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
	cache.OnEvent(fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.UpdatePodScaleDown}, nil, "event")

	if calls != 1 {
		t.Fatalf("hint calls = %d, want 1", calls)
	}
}

func TestEventMatchesUsesKubeSchedulerSemantics(t *testing.T) {
	tests := []struct {
		name       string
		registered fwk.ClusterEvent
		incoming   fwk.ClusterEvent
		want       bool
	}{
		{
			name:       "general update matches classified update",
			registered: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Update},
			incoming:   fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.UpdatePodScaleDown},
			want:       true,
		},
		{
			name:       "classified update does not match unclassified update",
			registered: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.UpdatePodScaleDown},
			incoming:   fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Update},
		},
		{
			name:       "custom label does not affect matching",
			registered: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete, CustomLabel: "registered"},
			incoming:   fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete, CustomLabel: "incoming"},
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := eventMatches(test.registered, test.incoming); got != test.want {
				t.Fatalf("eventMatches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOnEventDoesNotForgetNewerRecord(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	hintStarted := make(chan struct{})
	releaseHint := make(chan struct{})
	eventKeysFn := func(_ any, newObj any) ([]HintKey, error) {
		return []HintKey{HintKey(newObj.(string))}, nil
	}
	registerTestIndexedHint(registry, "old-plugin", event, func(*api.JobInfo, Rejection) ([]HintKey, error) {
		return []HintKey{"old-key"}, nil
	}, eventKeysFn, func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
		close(hintStarted)
		<-releaseHint
		return HintWakeup, nil
	})
	registerTestIndexedHint(registry, "new-plugin", event, func(*api.JobInfo, Rejection) ([]HintKey, error) {
		return []HintKey{"new-key"}, nil
	}, eventKeysFn, func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
		return HintSkip, nil
	})
	cache.Record(job, []Rejection{{Plugin: "old-plugin", Source: RejectionPredicate}})

	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		cache.OnEvent(event, nil, "old-key")
	}()
	<-hintStarted
	cache.Record(job, []Rejection{{Plugin: "new-plugin", Source: RejectionPredicate}})
	close(releaseHint)
	<-dispatched

	want := []Rejection{{Plugin: "new-plugin", Source: RejectionPredicate}}
	if got := cache.CachedRejections(job); !reflect.DeepEqual(got, want) {
		t.Fatalf("CachedRejections() = %#v, want newer record %#v", got, want)
	}
}

func TestOnEventIgnoresRecreatedPluginActionIndex(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	oldJob := api.NewJobInfo("old-job")
	newJob := api.NewJobInfo("new-job")
	eventKeysStarted := make(chan struct{})
	releaseEventKeys := make(chan struct{})
	newHintCalls := 0

	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		func(*api.JobInfo, Rejection) ([]HintKey, error) {
			return []HintKey{"shared-key"}, nil
		},
		func(any, any) ([]HintKey, error) {
			close(eventKeysStarted)
			<-releaseEventKeys
			return []HintKey{"shared-key"}, nil
		},
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			return HintWakeup, nil
		},
	)
	cache.Record(oldJob, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		cache.OnEvent(event, nil, "event")
	}()
	<-eventKeysStarted

	cache.Forget(oldJob.UID)
	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		func(*api.JobInfo, Rejection) ([]HintKey, error) {
			return []HintKey{"shared-key"}, nil
		},
		func(any, any) ([]HintKey, error) {
			return []HintKey{"shared-key"}, nil
		},
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			newHintCalls++
			return HintWakeup, nil
		},
	)
	cache.Record(newJob, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})
	close(releaseEventKeys)
	<-dispatched

	if newHintCalls != 0 {
		t.Fatalf("new index hint calls = %d, want 0 for an event using the deleted index snapshot", newHintCalls)
	}
	if got := len(cache.CachedRejections(newJob)); got != 1 {
		t.Fatalf("new Job cached rejections = %d, want 1", got)
	}
}

func TestJobCacheReusesPluginActionIndexAcrossRegistrations(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	jobOld := api.NewJobInfo("job-old")
	jobNew := api.NewJobInfo("job-new")
	oldCalls := 0
	newCalls := 0
	eventKeyCalls := 0
	jobKeysFn := func(*api.JobInfo, Rejection) ([]HintKey, error) {
		return []HintKey{"shared-key"}, nil
	}
	eventKeysFn := func(any, any) ([]HintKey, error) {
		eventKeyCalls++
		return []HintKey{"shared-key"}, nil
	}

	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		jobKeysFn,
		eventKeysFn,
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			oldCalls++
			return HintSkip, nil
		},
	)
	cache.Record(jobOld, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		jobKeysFn,
		eventKeysFn,
		func(*api.JobInfo, Rejection, any, any) (HintResult, error) {
			newCalls++
			return HintSkip, nil
		},
	)
	cache.Record(jobNew, []Rejection{{Plugin: "plugin", Source: RejectionPredicate}})

	cache.OnEvent(event, nil, "event")

	if oldCalls != 1 {
		t.Fatalf("old record calls = %d, want 1", oldCalls)
	}
	if newCalls != 1 {
		t.Fatalf("new record calls = %d, want 1", newCalls)
	}
	if eventKeyCalls != 1 {
		t.Fatalf("event key calls = %d, want 1 shared plugin/action index extraction", eventKeyCalls)
	}
}
