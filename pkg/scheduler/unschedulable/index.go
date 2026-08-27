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
	"k8s.io/apimachinery/pkg/util/sets"
	fwk "k8s.io/kube-scheduler/framework"
	kubeschedulerframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// eventIndex first classifies Job indexes by EventResource, then by the plugin
// and ActionType that further classify events of that resource. Wildcard event
// registrations are kept separately because they must be considered for every
// incoming EventResource.
type eventIndex struct {
	jobIndexesByResource map[fwk.EventResource]map[pluginActionKey]*pluginActionIndex
	wildcardJobIndexes   map[pluginActionKey]*pluginActionIndex
}

// pluginActionKey identifies one plugin's further classification of an
// EventResource. The plugin name keeps different plugins' HintKey spaces and
// event key extractors separate even when they handle the same ActionType.
type pluginActionKey struct {
	pluginName string
	actionType fwk.ActionType
}

// pluginActionIndex pairs the event-side HintKey extractor for one
// plugin/action classification with the Jobs classified by those HintKeys.
// Keeping the Job collections in jobHintKeyIndex separates event extraction
// from candidate Job lookup.
type pluginActionIndex struct {
	eventHintKeysFn EventKeysFn
	jobs            jobHintKeyIndex
}

// jobHintKeyIndex stores Job IDs according to whether precise HintKeys are
// available. allJobIDs is the fail-open candidate set when event key extraction
// fails. jobIDsWithoutHintKeys must be considered for every matching event.
// jobIDsByHintKey narrows Jobs with precise keys.
type jobHintKeyIndex struct {
	allJobIDs             sets.Set[api.JobID]
	jobIDsWithoutHintKeys sets.Set[api.JobID]
	jobIDsByHintKey       map[HintKey]sets.Set[api.JobID]
}

// jobIndexLocation identifies one index location containing a Job. It lets
// replacement and deletion remove the Job without scanning every index. An
// empty hintKeys slice means the Job is in jobIDsWithoutHintKeys.
type jobIndexLocation struct {
	resource        fwk.EventResource
	pluginActionKey pluginActionKey
	hintKeys        []HintKey
}

// pluginActionIndexSnapshot captures a matching plugin/action index before its
// event key extractor runs without c.mu. Candidate selection later verifies
// pointer identity so an old snapshot cannot select Jobs from a replacement
// index.
type pluginActionIndexSnapshot struct {
	resource        fwk.EventResource
	pluginActionKey pluginActionKey
	index           *pluginActionIndex
	eventHintKeysFn EventKeysFn
	eventHintKeys   []HintKey
	err             error
}

func newEventIndex() eventIndex {
	return eventIndex{
		jobIndexesByResource: make(map[fwk.EventResource]map[pluginActionKey]*pluginActionIndex),
		wildcardJobIndexes:   make(map[pluginActionKey]*pluginActionIndex),
	}
}

// removeJobFromIndexesLocked removes jobID from every plugin/action index
// recorded by its current unschedulableRecord. It does not delete the record:
// Record calls it before replacing a record, while Forget calls it before
// deleting one. The caller must hold c.mu
// for writing.
func (c *JobCache) removeJobFromIndexesLocked(jobID api.JobID) {
	rec, ok := c.records[jobID]
	if !ok {
		return
	}
	for _, location := range rec.indexLocations {
		index := c.lookupPluginActionIndexLocked(location.resource, location.pluginActionKey)
		if index == nil {
			continue
		}
		index.jobs.allJobIDs.Delete(jobID)
		index.jobs.jobIDsWithoutHintKeys.Delete(jobID)
		for _, hintKey := range location.hintKeys {
			if jobIDs := index.jobs.jobIDsByHintKey[hintKey]; jobIDs != nil {
				jobIDs.Delete(jobID)
				if jobIDs.Len() == 0 {
					delete(index.jobs.jobIDsByHintKey, hintKey)
				}
			}
		}
		if index.jobs.allJobIDs.Len() == 0 {
			c.deletePluginActionIndexLocked(location.resource, location.pluginActionKey)
		}
	}
}

// matchingPluginActionIndexSnapshots returns the plugin/action indexes that
// match ev. It first selects the concrete EventResource indexes, then checks
// ActionType, and finally includes matching wildcard indexes. EventKeysFn
// callbacks run after this method releases c.mu.
func (c *JobCache) matchingPluginActionIndexSnapshots(ev fwk.ClusterEvent) []pluginActionIndexSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var snapshots []pluginActionIndexSnapshot
	for key, index := range c.eventIndex.jobIndexesByResource[ev.Resource] {
		if eventMatches(fwk.ClusterEvent{Resource: ev.Resource, ActionType: key.actionType}, ev) {
			snapshots = append(snapshots, pluginActionIndexSnapshot{
				resource:        ev.Resource,
				pluginActionKey: key,
				index:           index,
				eventHintKeysFn: index.eventHintKeysFn,
			})
		}
	}
	for key, index := range c.eventIndex.wildcardJobIndexes {
		if eventMatches(fwk.ClusterEvent{Resource: fwk.WildCard, ActionType: key.actionType}, ev) {
			snapshots = append(snapshots, pluginActionIndexSnapshot{
				resource:        fwk.WildCard,
				pluginActionKey: key,
				index:           index,
				eventHintKeysFn: index.eventHintKeysFn,
			})
		}
	}
	return snapshots
}

// candidateRecordsForIndexSnapshots resolves extracted event HintKeys to
// immutable records. Extraction failure selects all Jobs in that index;
// otherwise Jobs without HintKeys and Jobs sharing an event HintKey are
// selected. A Job present in multiple matching indexes is returned once.
func (c *JobCache) candidateRecordsForIndexSnapshots(snapshots []pluginActionIndexSnapshot) []*unschedulableRecord {
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
		index := c.lookupPluginActionIndexLocked(snapshot.resource, snapshot.pluginActionKey)
		if index == nil || index != snapshot.index {
			continue
		}
		if snapshot.err != nil {
			for jobID := range index.jobs.allJobIDs {
				addJob(jobID)
			}
			continue
		}
		for jobID := range index.jobs.jobIDsWithoutHintKeys {
			addJob(jobID)
		}
		for _, hintKey := range snapshot.eventHintKeys {
			for jobID := range index.jobs.jobIDsByHintKey[hintKey] {
				addJob(jobID)
			}
		}
	}
	return candidates
}

// eventMatches reports whether an incoming event matches a plugin's registered
// event. A general Update matches a specific update such as UpdatePodScaleDown,
// but a specific update does not match a general Update whose changed property
// could not be classified.
func eventMatches(registered, incoming fwk.ClusterEvent) bool {
	return kubeschedulerframework.MatchClusterEvents(registered, incoming)
}

// ensurePluginActionIndexLocked returns the Job index for one resource/plugin/
// action classification, creating it when absent. An existing index retains
// the EventKeysFn installed when it was created so Jobs recorded in different
// Sessions share one event-side extraction. AddHintProvider therefore requires
// the paired JobKeysFn and EventKeysFn to keep the same HintKey semantics across
// registrations for this classification. The caller must hold c.mu for writing.
func (c *JobCache) ensurePluginActionIndexLocked(resource fwk.EventResource, key pluginActionKey, eventHintKeysFn EventKeysFn) *pluginActionIndex {
	newIndex := func() *pluginActionIndex {
		return &pluginActionIndex{
			eventHintKeysFn: eventHintKeysFn,
			jobs: jobHintKeyIndex{
				allJobIDs:             sets.New[api.JobID](),
				jobIDsWithoutHintKeys: sets.New[api.JobID](),
				jobIDsByHintKey:       make(map[HintKey]sets.Set[api.JobID]),
			},
		}
	}
	if resource == fwk.WildCard {
		if c.eventIndex.wildcardJobIndexes[key] == nil {
			c.eventIndex.wildcardJobIndexes[key] = newIndex()
		}
		return c.eventIndex.wildcardJobIndexes[key]
	}
	if c.eventIndex.jobIndexesByResource[resource] == nil {
		c.eventIndex.jobIndexesByResource[resource] = make(map[pluginActionKey]*pluginActionIndex)
	}
	if c.eventIndex.jobIndexesByResource[resource][key] == nil {
		c.eventIndex.jobIndexesByResource[resource][key] = newIndex()
	}
	return c.eventIndex.jobIndexesByResource[resource][key]
}

// lookupPluginActionIndexLocked returns the current index for one resource/
// plugin/action classification. The caller must hold c.mu for reading or
// writing.
func (c *JobCache) lookupPluginActionIndexLocked(resource fwk.EventResource, key pluginActionKey) *pluginActionIndex {
	if resource == fwk.WildCard {
		return c.eventIndex.wildcardJobIndexes[key]
	}
	if c.eventIndex.jobIndexesByResource[resource] == nil {
		return nil
	}
	return c.eventIndex.jobIndexesByResource[resource][key]
}

// deletePluginActionIndexLocked removes an empty index and its empty resource
// map. The caller must hold c.mu for writing.
func (c *JobCache) deletePluginActionIndexLocked(resource fwk.EventResource, key pluginActionKey) {
	if resource == fwk.WildCard {
		delete(c.eventIndex.wildcardJobIndexes, key)
		return
	}
	indexes := c.eventIndex.jobIndexesByResource[resource]
	delete(indexes, key)
	if len(indexes) == 0 {
		delete(c.eventIndex.jobIndexesByResource, resource)
	}
}

// uniqueHintKeys removes duplicate keys while preserving first-seen order.
func uniqueHintKeys(keys []HintKey) []HintKey {
	if len(keys) == 0 {
		return nil
	}
	seen := sets.New[HintKey]()
	unique := make([]HintKey, 0, len(keys))
	for _, key := range keys {
		if seen.Has(key) {
			continue
		}
		seen.Insert(key)
		unique = append(unique, key)
	}
	return unique
}
