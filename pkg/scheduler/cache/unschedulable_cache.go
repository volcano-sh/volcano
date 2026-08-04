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

// hintSubscription pairs a declared event with the plugin hint callback and the
// plugin's own Rejection. A nil HintFn means every matching occurrence of the
// event wakes the Job.
type hintSubscription struct {
	plugin    string
	event     api.ClusterEvent
	rejection api.Rejection
	hintFn    api.JobHintFn
}

// unschedulableRecord is one cached Job's state.
type unschedulableRecord struct {
	jobID      api.JobID
	rejections []api.Rejection

	lastFailedAt time.Time
	retryAfter   time.Time

	// subscriptions is a snapshot taken at Record time of every (event, hint)
	// pair that could wake this Job.
	subscriptions []hintSubscription
	// resources is the set of event resources this record subscribes to.
	resources sets.Set[fwk.EventResource]
}

// UnschedulableJobCache records Jobs that stayed unschedulable at CloseSession
// and lets later sessions skip their redundant filter work until a subscribed
// cluster event or the watchdog invalidates the record.
type UnschedulableJobCache struct {
	mu sync.RWMutex

	records map[api.JobID]*unschedulableRecord

	// byResource maps each subscribed event resource to the set of Jobs whose
	// hints care about it. wildcard holds Jobs subscribed against fwk.WildCard.
	byResource map[fwk.EventResource]sets.Set[api.JobID]
	wildcard   sets.Set[api.JobID]

	registry *HintRegistry

	// jobGetter returns the current JobInfo for a JobID, or nil when the Job is
	// no longer tracked by the scheduler cache.
	jobGetter func(api.JobID) *api.JobInfo

	maxSkipDuration time.Duration
}

var _ UnschedulableCache = (*UnschedulableJobCache)(nil)

// NewUnschedulableJobCache creates an UnschedulableJobCache backed by registry.
// jobGetter must return the current JobInfo for a JobID (nil if unknown).

func NewUnschedulableJobCache(registry *HintRegistry, jobGetter func(api.JobID) *api.JobInfo, maxSkipDuration time.Duration) *UnschedulableJobCache {
	if maxSkipDuration <= 0 {
		maxSkipDuration = DefaultMaxSkipDuration
	}
	return &UnschedulableJobCache{
		records:         make(map[api.JobID]*unschedulableRecord),
		byResource:      make(map[fwk.EventResource]sets.Set[api.JobID]),
		wildcard:        sets.New[api.JobID](),
		registry:        registry,
		jobGetter:       jobGetter,
		maxSkipDuration: maxSkipDuration,
	}
}

// AddHintProvider registers a plugin's event subscriptions and hint callbacks.
func (c *UnschedulableJobCache) AddHintProvider(name string, provider api.HintProvider) {
	c.registry.Register(name, provider)
}

// RecordUnschedulable inserts (or replaces) the Job with the rejections observed at
// CloseSession and copies the matching hint callbacks out of the registry. It
// returns without inserting if any rejection's plugin has no HintProvider.
func (c *UnschedulableJobCache) RecordUnschedulable(job *api.JobInfo, rejections []api.Rejection) {
	if c == nil || job == nil || len(rejections) == 0 {
		return
	}

	var subs []hintSubscription
	resources := sets.New[fwk.EventResource]()
	for _, r := range rejections {
		events := c.registry.eventsForPlugin(r.Plugin)
		if len(events) == 0 {
			// A rejecting plugin without hints can never wake the Job; do not cache it.
			klog.V(5).Infof("Job %s not cached: plugin %s has no HintProvider", job.UID, r.Plugin)
			c.ForgetUnschedulable(job.UID)
			return
		}
		for _, e := range events {
			subs = append(subs, hintSubscription{
				plugin:    r.Plugin,
				event:     e.Event,
				rejection: r,
				hintFn:    e.HintFn,
			})
			resources.Insert(e.Event.Resource)
		}
	}

	now := time.Now()
	rec := &unschedulableRecord{
		jobID:         job.UID,
		rejections:    rejections,
		lastFailedAt:  now,
		retryAfter:    now.Add(c.maxSkipDuration),
		subscriptions: subs,
		resources:     resources,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeJobFromEventIndexLocked(job.UID)
	c.records[job.UID] = rec
	for res := range resources {
		if res == fwk.WildCard {
			c.wildcard.Insert(job.UID)
			continue
		}
		if c.byResource[res] == nil {
			c.byResource[res] = sets.New[api.JobID]()
		}
		c.byResource[res].Insert(job.UID)
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
	c.mu.RLock()
	_, exists := c.records[jobID]
	c.mu.RUnlock()
	if !exists {
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

// removeJobFromEventIndexLocked removes jobID from the byResource and wildcard
// indexes recorded by its current unschedulableRecord. It does not delete the
// record itself: RecordUnschedulable calls it before replacing a record, while
// ForgetUnschedulable calls it before deleting one. The caller must hold c.mu
// for writing.
func (c *UnschedulableJobCache) removeJobFromEventIndexLocked(jobID api.JobID) {
	rec, ok := c.records[jobID]
	if !ok {
		return
	}
	c.wildcard.Delete(jobID)
	for res := range rec.resources {
		if s := c.byResource[res]; s != nil {
			s.Delete(jobID)
			if s.Len() == 0 {
				delete(c.byResource, res)
			}
		}
	}
}

// OnEvent is invoked by the informer dispatchers. It runs the hints subscribed to
// ev and Forgets any Job whose hint returns HintWakeup (or errors). A Job whose
// backing JobInfo is gone is also dropped.
func (c *UnschedulableJobCache) OnEvent(ev api.ClusterEvent, oldObj, newObj any) {
	if c == nil {
		return
	}
	c.mu.RLock()
	candidates := sets.New[api.JobID]()
	if s := c.byResource[ev.Resource]; s != nil {
		candidates = candidates.Union(s)
	}
	if c.wildcard.Len() > 0 {
		candidates = candidates.Union(c.wildcard)
	}
	c.mu.RUnlock()

	if candidates.Len() == 0 {
		return
	}

	for jobID := range candidates {
		job := c.jobGetter(jobID)
		if job == nil {
			c.ForgetUnschedulable(jobID)
			continue
		}
		if c.shouldWake(ev, jobID, job, oldObj, newObj) {
			metrics.RegisterUnschedulableJobCacheWakeup(job.Namespace, job.Name, string(ev.Resource), ev.ActionType.String())
			c.ForgetUnschedulable(jobID)
		}
	}
}

// shouldWake runs the Job's subscriptions matching ev and reports whether any of
// them asks to wake the Job. A nil HintFn wakes on any match; a hint error is
// treated as a wake.
func (c *UnschedulableJobCache) shouldWake(ev api.ClusterEvent, jobID api.JobID, job *api.JobInfo, oldObj, newObj any) bool {
	c.mu.RLock()
	rec, ok := c.records[jobID]
	var subs []hintSubscription
	if ok {
		subs = rec.subscriptions
	}
	c.mu.RUnlock()
	if !ok {
		return false
	}

	for _, sub := range subs {
		if !eventMatches(sub.event, ev) {
			continue
		}
		if sub.hintFn == nil {
			return true
		}
		result, err := sub.hintFn(job, sub.rejection, oldObj, newObj)
		if err != nil {
			klog.V(4).Infof("Hint %s errored for job %s, waking: %v", sub.plugin, jobID, err)
			return true
		}
		if result == api.HintWakeup {
			return true
		}
	}
	return false
}

// eventMatches reports whether an incoming event satisfies a declared
// subscription. A general Update subscription matches a specific update such as
// UpdatePodScaleDown, but a specific subscription does not match a general Update
// whose changed property could not be classified.
func eventMatches(sub, incoming api.ClusterEvent) bool {
	if sub.Resource != fwk.WildCard && sub.Resource != incoming.Resource {
		return false
	}
	return sub.ActionType&incoming.ActionType != 0 && incoming.ActionType <= sub.ActionType
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
	var expired []api.JobID
	for id, rec := range c.records {
		if !now.Before(rec.retryAfter) {
			expired = append(expired, id)
		}
	}
	c.mu.RUnlock()

	for _, id := range expired {
		klog.V(4).Infof("Watchdog forgetting expired unschedulable job %s", id)
		if job := c.jobGetter(id); job != nil {
			metrics.RegisterUnschedulableJobCacheWatchdogExpiration(job.Namespace, job.Name)
		}
		c.ForgetUnschedulable(id)
	}
}
