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
	"k8s.io/apimachinery/pkg/util/sets"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type eventIndexKey struct {
	plugin string
	event  api.ClusterEvent
}

type eventBucket struct {
	eventKeysFn api.EventKeysFn
	jobs        sets.Set[api.JobID]
	fallback    sets.Set[api.JobID]
	byHintKey   map[api.HintKey]sets.Set[api.JobID]
}

type recordIndexEntry struct {
	resource fwk.EventResource
	key      eventIndexKey
	fallback bool
	hintKeys []api.HintKey
}

type bucketSnapshot struct {
	key         eventIndexKey
	bucket      *eventBucket
	eventKeysFn api.EventKeysFn
	hintKeys    []api.HintKey
	err         error
}

// removeJobFromEventIndexLocked removes jobID from every bucket recorded by its
// current unschedulableRecord. It does not delete the record itself:
// RecordUnschedulable calls it before replacing a record, while
// ForgetUnschedulable calls it before deleting one. The caller must hold c.mu
// for writing.
func (c *UnschedulableJobCache) removeJobFromEventIndexLocked(jobID api.JobID) {
	rec, ok := c.records[jobID]
	if !ok {
		return
	}
	for _, entry := range rec.indexEntries {
		bucket := c.lookupBucketLocked(entry.key)
		if bucket == nil {
			continue
		}
		bucket.jobs.Delete(jobID)
		bucket.fallback.Delete(jobID)
		for _, hintKey := range entry.hintKeys {
			if jobs := bucket.byHintKey[hintKey]; jobs != nil {
				jobs.Delete(jobID)
				if jobs.Len() == 0 {
					delete(bucket.byHintKey, hintKey)
				}
			}
		}
		if bucket.jobs.Len() == 0 {
			c.deleteBucketLocked(entry.key)
		}
	}
}

func (c *UnschedulableJobCache) matchingBucketSnapshots(ev api.ClusterEvent) []bucketSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var snapshots []bucketSnapshot
	for key, bucket := range c.byResource[ev.Resource] {
		if eventMatches(key.event, ev) {
			snapshots = append(snapshots, bucketSnapshot{
				key:         key,
				bucket:      bucket,
				eventKeysFn: bucket.eventKeysFn,
			})
		}
	}
	for key, bucket := range c.wildcard {
		if eventMatches(key.event, ev) {
			snapshots = append(snapshots, bucketSnapshot{
				key:         key,
				bucket:      bucket,
				eventKeysFn: bucket.eventKeysFn,
			})
		}
	}
	return snapshots
}

func (c *UnschedulableJobCache) candidatesForSnapshots(snapshots []bucketSnapshot) []*unschedulableRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var candidates []*unschedulableRecord
	seen := sets.New[api.JobID]()
	addJob := func(jobID api.JobID) {
		if seen.Has(jobID) {
			return
		}
		rec := c.records[jobID]
		if rec == nil {
			return
		}
		seen.Insert(jobID)
		candidates = append(candidates, rec)
	}

	for _, snapshot := range snapshots {
		bucket := c.lookupBucketLocked(snapshot.key)
		if bucket == nil || bucket != snapshot.bucket {
			continue
		}
		if snapshot.err != nil {
			for jobID := range bucket.jobs {
				addJob(jobID)
			}
			continue
		}
		for jobID := range bucket.fallback {
			addJob(jobID)
		}
		for _, hintKey := range snapshot.hintKeys {
			for jobID := range bucket.byHintKey[hintKey] {
				addJob(jobID)
			}
		}
	}
	return candidates
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

func (c *UnschedulableJobCache) ensureBucketLocked(resource fwk.EventResource, key eventIndexKey, eventKeysFn api.EventKeysFn) *eventBucket {
	if resource == fwk.WildCard {
		if c.wildcard[key] == nil {
			c.wildcard[key] = &eventBucket{
				eventKeysFn: eventKeysFn,
				jobs:        sets.New[api.JobID](),
				fallback:    sets.New[api.JobID](),
				byHintKey:   make(map[api.HintKey]sets.Set[api.JobID]),
			}
		}
		return c.wildcard[key]
	}
	if c.byResource[resource] == nil {
		c.byResource[resource] = make(map[eventIndexKey]*eventBucket)
	}
	if c.byResource[resource][key] == nil {
		c.byResource[resource][key] = &eventBucket{
			eventKeysFn: eventKeysFn,
			jobs:        sets.New[api.JobID](),
			fallback:    sets.New[api.JobID](),
			byHintKey:   make(map[api.HintKey]sets.Set[api.JobID]),
		}
	}
	return c.byResource[resource][key]
}

func (c *UnschedulableJobCache) lookupBucketLocked(key eventIndexKey) *eventBucket {
	if key.event.Resource == fwk.WildCard {
		return c.wildcard[key]
	}
	if c.byResource[key.event.Resource] == nil {
		return nil
	}
	return c.byResource[key.event.Resource][key]
}

func (c *UnschedulableJobCache) deleteBucketLocked(key eventIndexKey) {
	if key.event.Resource == fwk.WildCard {
		delete(c.wildcard, key)
		return
	}
	buckets := c.byResource[key.event.Resource]
	delete(buckets, key)
	if len(buckets) == 0 {
		delete(c.byResource, key.event.Resource)
	}
}

func uniqueHintKeys(keys []api.HintKey) []api.HintKey {
	if len(keys) == 0 {
		return nil
	}
	seen := sets.New[api.HintKey]()
	unique := make([]api.HintKey, 0, len(keys))
	for _, key := range keys {
		if seen.Has(key) {
			continue
		}
		seen.Insert(key)
		unique = append(unique, key)
	}
	return unique
}
