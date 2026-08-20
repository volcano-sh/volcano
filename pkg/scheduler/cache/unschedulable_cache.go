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
	fwk "k8s.io/kube-scheduler/framework"

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

// hintSubscription pairs a declared event with the plugin hint callback and the
// plugin's own Rejection. A nil HintFn means every matching occurrence of the
// event wakes the Job.
type hintSubscription struct {
	plugin      string
	event       api.ClusterEvent
	rejection   api.Rejection
	jobKeysFn   api.JobKeysFn
	eventKeysFn api.EventKeysFn
	hintFn      api.JobHintFn
}

// unschedulableRecord is one cached Job's state.
type unschedulableRecord struct {
	jobID      api.JobID
	job        *api.JobInfo
	rejections []api.Rejection

	lastFailedAt time.Time
	retryAfter   time.Time

	// subscriptions is a snapshot taken at Record time of every (event, hint)
	// pair that could wake this Job.
	subscriptions []hintSubscription
	// indexEntries stores the exact event-index buckets that currently contain
	// this record.
	indexEntries []recordIndexEntry
}

// UnschedulableJobCache records Jobs that stayed unschedulable at CloseSession
// and lets later sessions skip their redundant filter work until a subscribed
// cluster event or the watchdog invalidates the record.
type UnschedulableJobCache struct {
	mu sync.RWMutex

	records map[api.JobID]*unschedulableRecord

	// byResource maps each subscribed event resource to secondary-index buckets.
	// wildcard holds subscriptions declared against fwk.WildCard.
	byResource map[fwk.EventResource]map[eventIndexKey]*eventBucket
	wildcard   map[eventIndexKey]*eventBucket

	registry *HintRegistry

	// eventGeneration and lastEventGeneration form a session barrier. A Job
	// must not be cached from a snapshot older than a matching cluster event.
	eventGeneration     uint64
	sessionGeneration   uint64
	sessionStarted      bool
	lastEventGeneration map[api.ClusterEvent]uint64

	maxSkipDuration time.Duration
}

var _ UnschedulableCache = (*UnschedulableJobCache)(nil)

// NewUnschedulableJobCache creates an UnschedulableJobCache backed by registry.
func NewUnschedulableJobCache(registry *HintRegistry, maxSkipDuration time.Duration) *UnschedulableJobCache {
	if maxSkipDuration <= 0 {
		maxSkipDuration = DefaultMaxSkipDuration
	}
	return &UnschedulableJobCache{
		records:             make(map[api.JobID]*unschedulableRecord),
		byResource:          make(map[fwk.EventResource]map[eventIndexKey]*eventBucket),
		wildcard:            make(map[eventIndexKey]*eventBucket),
		registry:            registry,
		lastEventGeneration: make(map[api.ClusterEvent]uint64),
		maxSkipDuration:     maxSkipDuration,
	}
}

// AddHintProvider registers a plugin's event subscriptions and hint callbacks.
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
	c.sessionGeneration = c.eventGeneration
	c.sessionStarted = true
}

// RecordUnschedulable inserts (or replaces) the Job with the rejections observed at
// CloseSession and copies the matching hint callbacks out of the registry. The
// caller must not mutate the session-local Job snapshot after this call. It
// returns without inserting if any rejection's plugin has no HintProvider.
func (c *UnschedulableJobCache) RecordUnschedulable(job *api.JobInfo, rejections []api.Rejection) {
	if c == nil || job == nil || len(rejections) == 0 {
		return
	}

	var subs []hintSubscription
	var indexEntries []recordIndexEntry
	for _, r := range rejections {
		events := c.registry.eventsForPlugin(r.Plugin)
		if len(events) == 0 {
			// A rejecting plugin without hints can never wake the Job; do not cache it.
			klog.V(5).Infof("Job %s not cached: plugin %s has no HintProvider", job.UID, r.Plugin)
			c.ForgetUnschedulable(job.UID)
			return
		}
		for _, e := range events {
			sub := hintSubscription{
				plugin:      r.Plugin,
				event:       e.Event,
				rejection:   r,
				jobKeysFn:   e.JobKeysFn,
				eventKeysFn: e.EventKeysFn,
				hintFn:      e.HintFn,
			}
			subs = append(subs, sub)

			entry := recordIndexEntry{
				resource: e.Event.Resource,
				key: eventIndexKey{
					plugin: r.Plugin,
					event:  e.Event,
				},
				fallback: true,
			}
			if sub.jobKeysFn != nil && sub.eventKeysFn != nil {
				hintKeys, err := sub.jobKeysFn(job, r)
				if err == nil {
					hintKeys = uniqueHintKeys(hintKeys)
				}
				if err == nil && len(hintKeys) > 0 && len(hintKeys) <= api.MaxHintKeysPerSubscription {
					entry.fallback = false
					entry.hintKeys = hintKeys
				}
			}
			indexEntries = append(indexEntries, entry)
		}
	}

	now := time.Now()
	rec := &unschedulableRecord{
		jobID: job.UID,
		// The framework passes its session-local Job snapshot after all session
		// mutations have completed. Retain that immutable snapshot instead of
		// locking and cloning the live scheduler-cache Job for every event.
		job:           job,
		rejections:    rejections,
		lastFailedAt:  now,
		retryAfter:    now.Add(c.maxSkipDuration),
		subscriptions: subs,
		indexEntries:  indexEntries,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.matchingEventAfterSessionStartLocked(rec.subscriptions) {
		klog.V(4).Infof("Job %s not cached: a matching event occurred after the session snapshot boundary", job.UID)
		return
	}
	c.removeJobFromEventIndexLocked(job.UID)
	c.records[job.UID] = rec
	for i, entry := range rec.indexEntries {
		bucket := c.ensureBucketLocked(entry.resource, entry.key, rec.subscriptions[i].eventKeysFn)
		bucket.jobs.Insert(job.UID)
		if entry.fallback {
			bucket.fallback.Insert(job.UID)
			continue
		}
		for _, hintKey := range entry.hintKeys {
			if bucket.byHintKey[hintKey] == nil {
				bucket.byHintKey[hintKey] = sets.New[api.JobID]()
			}
			bucket.byHintKey[hintKey].Insert(job.UID)
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

// ForgetUnschedulable drops the record for jobID.
func (c *UnschedulableJobCache) ForgetUnschedulable(jobID api.JobID) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.records[jobID]; !ok {
		return
	}
	c.removeJobFromEventIndexLocked(jobID)
	delete(c.records, jobID)
	klog.V(4).Infof("Forgot unschedulable job %s", jobID)
}

// OnEvent is invoked by the informer dispatchers. It runs the hints subscribed to
// ev and Forgets any Job whose hint returns HintWakeup (or errors).
func (c *UnschedulableJobCache) OnEvent(ev api.ClusterEvent, oldObj, newObj any) {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.eventGeneration++
	c.lastEventGeneration[ev] = c.eventGeneration
	c.mu.Unlock()

	snapshots := c.matchingBucketSnapshots(ev)
	for i := range snapshots {
		if snapshots[i].eventKeysFn == nil {
			continue
		}
		hintKeys, err := snapshots[i].eventKeysFn(oldObj, newObj)
		if err != nil {
			snapshots[i].err = err
			continue
		}
		hintKeys = uniqueHintKeys(hintKeys)
		if len(hintKeys) > api.MaxHintKeysPerSubscription {
			snapshots[i].err = errEventHintKeysOverLimit
			continue
		}
		snapshots[i].hintKeys = hintKeys
	}
	candidates := c.candidatesForSnapshots(snapshots)
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

func (c *UnschedulableJobCache) matchingEventAfterSessionStartLocked(subscriptions []hintSubscription) bool {
	if !c.sessionStarted {
		return false
	}
	for event, generation := range c.lastEventGeneration {
		if generation <= c.sessionGeneration {
			continue
		}
		for _, subscription := range subscriptions {
			if eventMatches(subscription.event, event) {
				return true
			}
		}
	}
	return false
}

// shouldWake runs the matching hints and reports whether any hint wakes the
// Job. Records are immutable, so hints can run without holding c.mu.
func shouldWake(rec *unschedulableRecord, ev api.ClusterEvent, oldObj, newObj any) bool {
	for _, sub := range rec.subscriptions {
		if !eventMatches(sub.event, ev) {
			continue
		}
		if sub.hintFn == nil {
			return true
		}
		result, err := sub.hintFn(rec.job, sub.rejection, oldObj, newObj)
		if err != nil {
			klog.V(4).Infof("Hint %s errored for job %s, waking: %v", sub.plugin, rec.jobID, err)
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
	c.removeJobFromEventIndexLocked(rec.jobID)
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
