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
func assertByHintKeyMembership(t *testing.T, cache *UnschedulableJobCache, event api.ClusterEvent, key api.HintKey, jobID api.JobID, want bool) {
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

// TestUnschedulableJobCacheByHintKeyIndexRemoval proves that the HintKey
// secondary index is kept consistent with the primary record on replacement,
// explicit Forget, and watchdog expiry: the stale key entry is removed so it
// can no longer dispatch, and (for replacement) the new key entry is present
// and does dispatch.
func TestUnschedulableJobCacheByHintKeyIndexRemoval(t *testing.T) {
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	// echoJobKeys/echoEventKey let each test control exactly which HintKey a
	// record or event carries while sharing one plugin/action index.
	echoJobKeys := func(_ *api.JobInfo, rejection api.Rejection) ([]api.HintKey, error) {
		return rejection.HintKeys, nil
	}
	echoEventKey := func(_ any, newObj any) ([]api.HintKey, error) {
		return []api.HintKey{api.HintKey(newObj.(string))}, nil
	}

	// keepAlive is recorded under its own stable key in the same plugin/action
	// index as the Job under test, so the index's allJobIDs set never empties.
	// Without this, deleting and recreating a single-Job index would mask a
	// hypothetical bug in the per-hintKey/per-job removal these tests target.
	newKeepAlive := func(cache *UnschedulableJobCache) {
		keepAlive := api.NewJobInfo("keep-alive")
		cache.RecordUnschedulable(keepAlive, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate, HintKeys: []api.HintKey{"keep-alive-key"}}})
	}

	t.Run("replacement removes the stale key and the new key dispatches", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				return api.HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate, HintKeys: []api.HintKey{"old-key"}}})
		assertByHintKeyMembership(t, cache, event, "old-key", job.UID, true)

		cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate, HintKeys: []api.HintKey{"new-key"}}})
		assertByHintKeyMembership(t, cache, event, "old-key", job.UID, false)
		assertByHintKeyMembership(t, cache, event, "new-key", job.UID, true)

		cache.OnEvent(event, nil, "old-key")
		if got := len(cache.GetCachedRejections(job)); got == 0 {
			t.Fatalf("stale key dispatched and forgot the record; record cached = %d, want > 0", got)
		}

		cache.OnEvent(event, nil, "new-key")
		if got := len(cache.GetCachedRejections(job)); got != 0 {
			t.Fatalf("replacement key must dispatch and wake the record; record cached = %d, want 0", got)
		}
	})

	t.Run("explicit Forget removes the key entry", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				return api.HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate, HintKeys: []api.HintKey{"key"}}})
		assertByHintKeyMembership(t, cache, event, "key", job.UID, true)

		cache.ForgetUnschedulable(job.UID)
		assertByHintKeyMembership(t, cache, event, "key", job.UID, false)

		// The stale key must not dispatch: the only Job left under it is
		// keep-alive, unaffected by job's removal.
		cache.OnEvent(event, nil, "key")
		if got := len(cache.GetCachedRejections(job)); got != 0 {
			t.Fatalf("forgotten job's record reappeared; record cached = %d, want 0", got)
		}
	})

	t.Run("watchdog expiry removes the key entry", func(t *testing.T) {
		job := api.NewJobInfo("job")
		cache, registry := newTestUnschedulableCache()
		registerTestIndexedHint(registry, "plugin", event, echoJobKeys, echoEventKey,
			func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				return api.HintWakeup, nil
			})
		newKeepAlive(cache)

		cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate, HintKeys: []api.HintKey{"key"}}})
		assertByHintKeyMembership(t, cache, event, "key", job.UID, true)

		cache.mu.Lock()
		cache.records[job.UID].retryAfter = time.Now().Add(-time.Second)
		cache.mu.Unlock()
		cache.forgetExpired()

		assertByHintKeyMembership(t, cache, event, "key", job.UID, false)

		cache.OnEvent(event, nil, "key")
		if got := len(cache.GetCachedRejections(job)); got != 0 {
			t.Fatalf("watchdog-expired job's record reappeared; record cached = %d, want 0", got)
		}
	})
}

func TestUnschedulableJobCacheSecondaryIndex(t *testing.T) {
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	overLimitEventKeys := make([]api.HintKey, api.MaxHintKeysPerPluginEvent+1)
	for i := range overLimitEventKeys {
		overLimitEventKeys[i] = api.HintKey(fmt.Sprintf("event-key-%d", i))
	}
	tests := []struct {
		name                string
		eventKeys           []api.HintKey
		eventErr            error
		jobBWithoutHintKeys bool
		wantCallsA          int
		wantCallsB          int
	}{
		{name: "matching indexed key dispatches only indexed match", eventKeys: []api.HintKey{"key-a"}, wantCallsA: 1},
		{name: "non-matching indexed key skips indexed jobs", eventKeys: []api.HintKey{"key-miss"}},
		{name: "job without HintKeys is dispatched beside indexed match", eventKeys: []api.HintKey{"key-a"}, jobBWithoutHintKeys: true, wantCallsA: 1, wantCallsB: 1},
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
				func(job *api.JobInfo, _ api.Rejection) ([]api.HintKey, error) {
					switch job.UID {
					case jobA.UID:
						return []api.HintKey{"key-a"}, nil
					case jobB.UID:
						if test.jobBWithoutHintKeys {
							return nil, nil
						}
						return []api.HintKey{"key-b"}, nil
					default:
						return nil, fmt.Errorf("unexpected job %s", job.UID)
					}
				},
				func(any, any) ([]api.HintKey, error) {
					if test.eventErr != nil {
						return nil, test.eventErr
					}
					return append([]api.HintKey(nil), test.eventKeys...), nil
				},
				func(job *api.JobInfo, _ api.Rejection, _, _ any) (api.HintResult, error) {
					calls[job.UID]++
					return api.HintSkip, nil
				},
			)

			rejection := []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}}
			cache.RecordUnschedulable(jobA, rejection)
			cache.RecordUnschedulable(jobB, rejection)

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

func TestUnschedulableJobCacheDoesNotCacheDuplicatePluginEvent(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	indexedCalls := 0
	noHintKeysCalls := 0

	registry.Register("plugin", &fakeHintProvider{events: []api.ClusterEventWithHint{
		{
			Event: event,
			JobKeysFn: func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
				return []api.HintKey{"match"}, nil
			},
			EventKeysFn: func(any, any) ([]api.HintKey, error) {
				return []api.HintKey{"match"}, nil
			},
			HintFn: func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				indexedCalls++
				return api.HintSkip, nil
			},
		},
		{
			Event: event,
			HintFn: func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
				noHintKeysCalls++
				return api.HintSkip, nil
			},
		},
	}})

	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})
	if got := cache.GetCachedRejections(job); got != nil {
		t.Fatalf("GetCachedRejections() = %#v, want nil for a duplicate plugin event", got)
	}
	cache.OnEvent(event, nil, "event")

	if indexedCalls != 0 {
		t.Fatalf("indexed hint calls = %d, want 0", indexedCalls)
	}
	if noHintKeysCalls != 0 {
		t.Fatalf("hint calls without HintKeys = %d, want 0", noHintKeysCalls)
	}
}

func TestUnschedulableJobCacheJobsWithoutHintKeys(t *testing.T) {
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	tests := []struct {
		name        string
		jobKeysFn   api.JobKeysFn
		eventKeysFn api.EventKeysFn
		wantCalls   int
	}{
		{
			name:      "nil extractors dispatch the job without HintKeys",
			wantCalls: 1,
		},
		{
			name: "job extraction error dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
				return nil, errors.New("bad job")
			},
			eventKeysFn: func(any, any) ([]api.HintKey, error) {
				return []api.HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "zero job keys dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
				return []api.HintKey{}, nil
			},
			eventKeysFn: func(any, any) ([]api.HintKey, error) {
				return []api.HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "too many job keys dispatches the job without HintKeys",
			jobKeysFn: func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
				keys := make([]api.HintKey, api.MaxHintKeysPerPluginEvent+1)
				for i := range keys {
					keys[i] = api.HintKey(fmt.Sprintf("key-%d", i))
				}
				return keys, nil
			},
			eventKeysFn: func(any, any) ([]api.HintKey, error) {
				return []api.HintKey{"key"}, nil
			},
			wantCalls: 1,
		},
		{
			name: "duplicate keys dispatch once",
			jobKeysFn: func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
				return []api.HintKey{"dup", "dup"}, nil
			},
			eventKeysFn: func(any, any) ([]api.HintKey, error) {
				return []api.HintKey{"dup", "dup"}, nil
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
				func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
					hintCalls++
					return api.HintSkip, nil
				},
			)

			cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})
			cache.OnEvent(event, nil, "event")

			if hintCalls != test.wantCalls {
				t.Fatalf("hint calls = %d, want %d", hintCalls, test.wantCalls)
			}
		})
	}
}

func TestUnschedulableJobCacheNilHintDispatchesWithoutHintKeys(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
			return []api.HintKey{"job-key"}, nil
		},
		func(any, any) ([]api.HintKey, error) {
			return []api.HintKey{"different-event-key"}, nil
		},
		nil,
	)

	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})
	cache.OnEvent(event, nil, "event")

	if got := cache.GetCachedRejections(job); got != nil {
		t.Fatalf("GetCachedRejections() = %#v, want nil after matching event with nil HintFn", got)
	}
}

func TestUnschedulableJobCacheClassifiedUpdate(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	job := api.NewJobInfo("job")
	calls := 0

	registerTestIndexedHint(
		registry,
		"plugin",
		api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Update},
		func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
			return []api.HintKey{"scale-down"}, nil
		},
		func(any, any) ([]api.HintKey, error) {
			return []api.HintKey{"scale-down"}, nil
		},
		func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
			calls++
			return api.HintSkip, nil
		},
	)

	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})
	cache.OnEvent(api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.UpdatePodScaleDown}, nil, "event")

	if calls != 1 {
		t.Fatalf("hint calls = %d, want 1", calls)
	}
}

func TestOnEventDoesNotForgetNewerRecord(t *testing.T) {
	job := api.NewJobInfo("job")
	cache, registry := newTestUnschedulableCache()
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	hintStarted := make(chan struct{})
	releaseHint := make(chan struct{})
	eventKeysFn := func(_ any, newObj any) ([]api.HintKey, error) {
		return []api.HintKey{api.HintKey(newObj.(string))}, nil
	}
	registerTestIndexedHint(registry, "old-plugin", event, func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
		return []api.HintKey{"old-key"}, nil
	}, eventKeysFn, func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
		close(hintStarted)
		<-releaseHint
		return api.HintWakeup, nil
	})
	registerTestIndexedHint(registry, "new-plugin", event, func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
		return []api.HintKey{"new-key"}, nil
	}, eventKeysFn, func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
		return api.HintSkip, nil
	})
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "old-plugin", Source: api.RejectionPredicate}})

	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		cache.OnEvent(event, nil, "old-key")
	}()
	<-hintStarted
	cache.RecordUnschedulable(job, []api.Rejection{{Plugin: "new-plugin", Source: api.RejectionPredicate}})
	close(releaseHint)
	<-dispatched

	want := []api.Rejection{{Plugin: "new-plugin", Source: api.RejectionPredicate}}
	if got := cache.GetCachedRejections(job); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetCachedRejections() = %#v, want newer record %#v", got, want)
	}
}

func TestUnschedulableJobCacheReusesPluginActionIndexAcrossRegistrations(t *testing.T) {
	cache, registry := newTestUnschedulableCache()
	event := api.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}
	jobOld := api.NewJobInfo("job-old")
	jobNew := api.NewJobInfo("job-new")
	oldCalls := 0
	newCalls := 0
	eventKeyCalls := 0
	jobKeysFn := func(*api.JobInfo, api.Rejection) ([]api.HintKey, error) {
		return []api.HintKey{"shared-key"}, nil
	}
	eventKeysFn := func(any, any) ([]api.HintKey, error) {
		eventKeyCalls++
		return []api.HintKey{"shared-key"}, nil
	}

	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		jobKeysFn,
		eventKeysFn,
		func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
			oldCalls++
			return api.HintSkip, nil
		},
	)
	cache.RecordUnschedulable(jobOld, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

	registerTestIndexedHint(
		registry,
		"plugin",
		event,
		jobKeysFn,
		eventKeysFn,
		func(*api.JobInfo, api.Rejection, any, any) (api.HintResult, error) {
			newCalls++
			return api.HintSkip, nil
		},
	)
	cache.RecordUnschedulable(jobNew, []api.Rejection{{Plugin: "plugin", Source: api.RejectionPredicate}})

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
