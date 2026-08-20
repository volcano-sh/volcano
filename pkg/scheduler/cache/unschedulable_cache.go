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
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

const (
	// DefaultMaxSkipDuration bounds how long a Job may stay cached without a
	// matching event before the watchdog re-evaluates it.
	DefaultMaxSkipDuration = 5 * time.Minute
	// watchdogInterval is how often the watchdog scans for expired records.
	watchdogInterval = time.Minute
)

var errEventHintKeysOverLimit = errors.New("event hint keys exceed limit")

// pluginEventHint contains one plugin's handling of an event that may change
// its rejection. A nil jobHintFn means every matching event wakes the Job.
type pluginEventHint struct {
	pluginName      string
	event           api.ClusterEvent
	rejection       api.Rejection
	jobHintKeysFn   api.JobKeysFn
	eventHintKeysFn api.EventKeysFn
	jobHintFn       api.JobHintFn
}

// unschedulableRecord is one cached Job's state.
type unschedulableRecord struct {
	jobID      api.JobID
	job        *api.JobInfo
	rejections []api.Rejection

	lastFailedAt time.Time
	retryAfter   time.Time

	// eventHints is a snapshot taken at Record time of every plugin event hint
	// that could wake this Job.
	eventHints []pluginEventHint
	// indexLocations records every index location containing this Job so the
	// locations can be populated and later cleaned without scanning all indexes.
	indexLocations []jobIndexLocation
}

// UnschedulableJobCache records Jobs that stayed unschedulable at CloseSession
// and lets later sessions skip their redundant filter work until a subscribed
// cluster event or the watchdog invalidates the record.
type UnschedulableJobCache struct {
	mu sync.RWMutex

	records map[api.JobID]*unschedulableRecord

	// eventIndex narrows each incoming resource/plugin/action event to candidate
	// Jobs before their final JobHintFn runs.
	eventIndex eventIndex

	registry *HintRegistry

	// freshness prevents a stale session result from being cached after a
	// relevant event or direct Job invalidation has already occurred.
	freshness freshnessTracker

	maxSkipDuration time.Duration
}

var _ UnschedulableCache = (*UnschedulableJobCache)(nil)

// NewUnschedulableJobCache creates an UnschedulableJobCache backed by registry.
func NewUnschedulableJobCache(registry *HintRegistry, maxSkipDuration time.Duration) *UnschedulableJobCache {
	if maxSkipDuration <= 0 {
		maxSkipDuration = DefaultMaxSkipDuration
	}
	return &UnschedulableJobCache{
		records:         make(map[api.JobID]*unschedulableRecord),
		eventIndex:      newEventIndex(),
		registry:        registry,
		freshness:       newFreshnessTracker(),
		maxSkipDuration: maxSkipDuration,
	}
}

// AddHintProvider registers the events and hint callbacks handled by a plugin.
func (c *UnschedulableJobCache) AddHintProvider(name string, provider api.HintProvider) {
	c.registry.Register(name, provider)
}

// BeginSession captures the event generation immediately before the scheduler
// snapshot is taken. RecordUnschedulable uses it to reject stale conclusions.
func (c *UnschedulableJobCache) BeginSession() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.freshness.beginSession()
}

// RecordUnschedulable inserts (or replaces) the Job with the rejections observed at
// CloseSession and copies the matching hint callbacks out of the registry. The
// caller must not mutate the session-local Job snapshot after this call. It
// returns without inserting if any rejection's plugin has no HintProvider.
func (c *UnschedulableJobCache) RecordUnschedulable(job *api.JobInfo, rejections []api.Rejection) {
	if c == nil || job == nil || len(rejections) == 0 {
		return
	}

	var eventHints []pluginEventHint
	var indexLocations []jobIndexLocation
	for _, r := range rejections {
		events := c.registry.eventsForPlugin(r.Plugin)
		if len(events) == 0 {
			// A rejecting plugin without hints can never wake the Job; do not cache it.
			klog.V(5).Infof("Job %s not cached: plugin %s has no HintProvider", job.UID, r.Plugin)
			c.ForgetUnschedulable(job.UID)
			return
		}
		for _, e := range events {
			eventHint := pluginEventHint{
				pluginName:      r.Plugin,
				event:           e.Event,
				rejection:       r,
				jobHintKeysFn:   e.JobKeysFn,
				eventHintKeysFn: e.EventKeysFn,
				jobHintFn:       e.HintFn,
			}
			eventHints = append(eventHints, eventHint)

			location := jobIndexLocation{
				resource: e.Event.Resource,
				pluginActionKey: pluginActionKey{
					pluginName: r.Plugin,
					actionType: e.Event.ActionType,
				},
			}
			if eventHint.jobHintFn != nil && eventHint.jobHintKeysFn != nil && eventHint.eventHintKeysFn != nil {
				hintKeys, err := eventHint.jobHintKeysFn(job, r)
				if err == nil {
					hintKeys = uniqueHintKeys(hintKeys)
				}
				if err == nil && len(hintKeys) > 0 && len(hintKeys) <= api.MaxHintKeysPerPluginEvent {
					location.hintKeys = hintKeys
				}
			}
			indexLocations = append(indexLocations, location)
		}
	}

	now := time.Now()
	rec := &unschedulableRecord{
		jobID: job.UID,
		// The framework passes its session-local Job snapshot after all session
		// mutations have completed. Retain that immutable snapshot instead of
		// locking and cloning the live scheduler-cache Job for every event.
		job:            job,
		rejections:     rejections,
		lastFailedAt:   now,
		retryAfter:     now.Add(c.maxSkipDuration),
		eventHints:     eventHints,
		indexLocations: indexLocations,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.freshness.jobChangedAfterSessionStart(job.UID) {
		klog.V(4).Infof("Job %s not cached: its scheduling inputs changed after the session snapshot boundary", job.UID)
		return
	}
	if c.freshness.matchingEventOccurredAfterSessionStart(rec.eventHints) {
		klog.V(4).Infof("Job %s not cached: a matching event occurred after the session snapshot boundary", job.UID)
		return
	}
	c.removeJobFromIndexesLocked(job.UID)
	c.records[job.UID] = rec
	for i, location := range rec.indexLocations {
		index := c.ensurePluginActionIndexLocked(location.resource, location.pluginActionKey, rec.eventHints[i].eventHintKeysFn)
		// allJobIDs contains every Job ID in this index, with or without HintKeys.
		index.jobs.allJobIDs.Insert(job.UID)
		// A Job without HintKeys is always considered for matching events;
		// otherwise, the Job is indexed under each of its HintKeys.
		if len(location.hintKeys) == 0 {
			index.jobs.jobIDsWithoutHintKeys.Insert(job.UID)
			continue
		}
		for _, hintKey := range location.hintKeys {
			if index.jobs.jobIDsByHintKey[hintKey] == nil {
				index.jobs.jobIDsByHintKey[hintKey] = sets.New[api.JobID]()
			}
			index.jobs.jobIDsByHintKey[hintKey].Insert(job.UID)
		}
	}
	klog.V(4).Infof("Cached unschedulable job %s with %d rejection(s), retryAfter %v",
		job.UID, len(rejections), rec.retryAfter)
}

// GetCachedRejections returns the rejections recorded for job in the previous
// session, or nil when no record exists and the Job should be evaluated normally.
func (c *UnschedulableJobCache) GetCachedRejections(job *api.JobInfo) []api.Rejection {
	if c == nil || job == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	rec, ok := c.records[job.UID]
	if !ok {
		return nil
	}
	return rec.rejections
}

// ForgetUnschedulable drops an existing record for jobID. The read-lock fast
// path avoids exclusive-lock contention when session reconciliation checks a
// Job that has no cached record.
func (c *UnschedulableJobCache) ForgetUnschedulable(jobID api.JobID) {
	if c == nil {
		return
	}
	c.mu.RLock()
	rec := c.records[jobID]
	c.mu.RUnlock()
	if rec == nil {
		return
	}
	c.forgetRecord(rec)
}

// InvalidateUnschedulable records a workload change even when jobID has no
// current record. This prevents RecordUnschedulable from publishing a stale
// conclusion computed before the change.
func (c *UnschedulableJobCache) InvalidateUnschedulable(jobID api.JobID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.freshness.recordJobInvalidation(jobID)
	if _, ok := c.records[jobID]; !ok {
		return
	}
	c.removeJobFromIndexesLocked(jobID)
	delete(c.records, jobID)
	klog.V(4).Infof("Invalidated unschedulable job %s", jobID)
}

// OnEvent is invoked by the informer dispatchers. It runs the hints subscribed to
// ev and Forgets any Job whose hint returns HintWakeup (or errors).
func (c *UnschedulableJobCache) OnEvent(ev api.ClusterEvent, oldObj, newObj any) {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.freshness.recordEvent(ev)
	c.mu.Unlock()

	snapshots := c.matchingPluginActionIndexSnapshots(ev)
	for i := range snapshots {
		if snapshots[i].eventHintKeysFn == nil {
			continue
		}
		hintKeys, err := snapshots[i].eventHintKeysFn(oldObj, newObj)
		if err != nil {
			snapshots[i].err = err
			continue
		}
		hintKeys = uniqueHintKeys(hintKeys)
		if len(hintKeys) > api.MaxHintKeysPerPluginEvent {
			snapshots[i].err = errEventHintKeysOverLimit
			continue
		}
		snapshots[i].eventHintKeys = hintKeys
	}
	candidates := c.candidateRecordsForIndexSnapshots(snapshots)
	if len(candidates) == 0 {
		return
	}

	for _, rec := range candidates {
		if shouldWake(rec, ev, oldObj, newObj) {
			metrics.RegisterUnschedulableJobCacheWakeup(rec.job.Namespace, rec.job.Name, string(ev.Resource), ev.ActionType.String())
			c.forgetRecord(rec)
		}
	}
}

// shouldWake runs the matching hints and reports whether any hint wakes the
// Job. Records are immutable, so hints can run without holding c.mu.
func shouldWake(rec *unschedulableRecord, ev api.ClusterEvent, oldObj, newObj any) bool {
	for _, eventHint := range rec.eventHints {
		if !eventMatches(eventHint.event, ev) {
			continue
		}
		if eventHint.jobHintFn == nil {
			return true
		}
		result, err := eventHint.jobHintFn(rec.job, eventHint.rejection, oldObj, newObj)
		if err != nil {
			klog.V(4).Infof("Hint %s errored for job %s, waking: %v", eventHint.pluginName, rec.jobID, err)
			return true
		}
		if result == api.HintWakeup {
			return true
		}
	}
	return false
}

// forgetRecord removes rec only when it is still current, so an event using an
// older snapshot cannot remove a replacement record.
func (c *UnschedulableJobCache) forgetRecord(rec *unschedulableRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.records[rec.jobID] != rec {
		return
	}
	c.removeJobFromIndexesLocked(rec.jobID)
	delete(c.records, rec.jobID)
	klog.V(4).Infof("Forgot unschedulable job %s", rec.jobID)
}

// StartWatchdog runs the background goroutine that Forgets expired records.
func (c *UnschedulableJobCache) StartWatchdog(stopCh <-chan struct{}) {
	if c == nil {
		return
	}
	interval := min(watchdogInterval, c.maxSkipDuration)
	go wait.Until(c.forgetExpired, interval, stopCh)
}

func (c *UnschedulableJobCache) forgetExpired() {
	now := time.Now()
	c.mu.RLock()
	var expired []*unschedulableRecord
	for _, rec := range c.records {
		if !now.Before(rec.retryAfter) {
			expired = append(expired, rec)
		}
	}
	c.mu.RUnlock()

	for _, rec := range expired {
		klog.V(4).Infof("Watchdog forgetting expired unschedulable job %s", rec.jobID)
		metrics.RegisterUnschedulableJobCacheWatchdogExpiration(rec.job.Namespace, rec.job.Name)
		c.forgetRecord(rec)
	}
}
