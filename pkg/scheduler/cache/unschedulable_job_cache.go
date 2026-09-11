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

	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const defaultJobMaxInUnschedulableCacheDuration = 5 * time.Minute

type unschedulableJobInfo struct {
	addedAt   time.Time
	expiresAt time.Time
	reason    string
}

// unschedulableJobCache prevents jobs from being re-enqueued for a bounded
// period after a failed scheduling attempt.
type unschedulableJobCache struct {
	mutex       sync.Mutex
	jobs        map[api.JobID]unschedulableJobInfo
	clock       clock.Clock
	maxDuration time.Duration
}

func newUnschedulableJobCache() unschedulableJobCache {
	return unschedulableJobCache{
		jobs:        make(map[api.JobID]unschedulableJobInfo),
		clock:       clock.RealClock{},
		maxDuration: defaultJobMaxInUnschedulableCacheDuration,
	}
}

func newUnschedulableJobCacheWithClock(clock clock.Clock, maxDuration time.Duration) unschedulableJobCache {
	return unschedulableJobCache{
		jobs:        make(map[api.JobID]unschedulableJobInfo),
		clock:       clock,
		maxDuration: maxDuration,
	}
}

// Add inserts a job if it is not already cached. Repeated additions do not
// extend the original expiration deadline.
func (c *unschedulableJobCache) Add(jobID api.JobID, reason string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.ensureInitialized()
	now := c.clock.Now()
	if existing, found := c.jobs[jobID]; found && now.Before(existing.expiresAt) {
		return
	}

	info := unschedulableJobInfo{
		addedAt:   now,
		expiresAt: now.Add(c.maxDuration),
		reason:    reason,
	}
	c.jobs[jobID] = info
	klog.V(3).InfoS("Added job to unschedulable job cache",
		"job", jobID, "reason", reason, "expiresAt", info.expiresAt)
}

// Delete removes a job from the cache.
func (c *unschedulableJobCache) Delete(jobID api.JobID) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.jobs == nil {
		return
	}
	delete(c.jobs, jobID)
}

// Contains reports whether a job is still within its maximum cache duration.
// Expired entries are removed lazily.
func (c *unschedulableJobCache) Contains(jobID api.JobID) bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.jobs == nil {
		return false
	}
	info, found := c.jobs[jobID]
	if !found {
		return false
	}

	now := c.now()
	if now.Before(info.expiresAt) {
		return true
	}

	delete(c.jobs, jobID)
	klog.V(3).InfoS("Expired job from unschedulable job cache",
		"job", jobID, "reason", info.reason,
		"addedAt", info.addedAt, "expiredAt", info.expiresAt)
	return false
}

func (c *unschedulableJobCache) ensureInitialized() {
	if c.jobs == nil {
		c.jobs = make(map[api.JobID]unschedulableJobInfo)
	}
	if c.clock == nil {
		c.clock = clock.RealClock{}
	}
	if c.maxDuration <= 0 {
		c.maxDuration = defaultJobMaxInUnschedulableCacheDuration
	}
}

func (c *unschedulableJobCache) now() time.Time {
	if c.clock == nil {
		return time.Now()
	}
	return c.clock.Now()
}
