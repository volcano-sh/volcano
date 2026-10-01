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

package framework

import (
	"sort"

	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

// rejectionKey identifies a rejection by the plugin and extension point that
// produced it, so repeated rejections for the same key merge their tasks.
type rejectionKey struct {
	plugin string
	source unschedulable.RejectionSource
}

// rejectionAggregate contains the tasks and hint keys recorded for one rejection key.
type rejectionAggregate struct {
	tasks    sets.Set[api.TaskID]
	hintKeys sets.Set[unschedulable.HintKey] // nil means coarse fallback
}

// jobRejectionScope contains rejections produced while evaluating one
// allocation candidate for a Job or one of its SubJobs. SubJob rejections use
// the owning Job ID because the unschedulable cache stores Job-level records.
type jobRejectionScope struct {
	jobID      api.JobID
	rejections map[rejectionKey]*rejectionAggregate
}

// jobRejectionTracker keeps temporary candidate rejections separate from the
// rejections confirmed for the current Session.
type jobRejectionTracker struct {
	// recorded contains confirmed rejections that will be reconciled with the
	// unschedulable Job cache when the Session closes.
	recorded map[api.JobID]map[rejectionKey]*rejectionAggregate
	// evaluationScopes is a stack because Job candidate evaluation can contain
	// nested SubJob candidate evaluations.
	evaluationScopes []jobRejectionScope
}

// RejectionTrackingEnabled reports whether this Session records Job
// rejections for the unschedulable Job cache.
func (ssn *Session) RejectionTrackingEnabled() bool {
	return ssn.unschedulableJobCacheEnabled
}

// AddRejection records, for the current session, that plugin made job
// unschedulable through the given source, optionally naming the failed tasks.
// Rejections are drained into the unschedulable-job cache at CloseSession.
func (ssn *Session) AddRejection(jobID api.JobID, plugin string, source unschedulable.RejectionSource, tasks ...api.TaskID) {
	ssn.AddRejectionWithKeys(jobID, plugin, source, nil, tasks...)
}

// AddRejectionWithKeys records, for the current session, that plugin made
// job unschedulable through the given source, optionally naming the failed
// tasks and the hint keys that were available for that rejection.
func (ssn *Session) AddRejectionWithKeys(jobID api.JobID, plugin string, source unschedulable.RejectionSource, hintKeys []unschedulable.HintKey, tasks ...api.TaskID) {
	if !ssn.unschedulableJobCacheEnabled {
		return
	}
	ssn.jobRejections.record(jobID, plugin, source, hintKeys, tasks...)
}

// record adds a rejection to the active evaluation scope for jobID, or to the
// confirmed Session aggregate when no matching scope is active.
func (tracker *jobRejectionTracker) record(jobID api.JobID, plugin string, source unschedulable.RejectionSource, hintKeys []unschedulable.HintKey, tasks ...api.TaskID) {
	rejectionsByKey := tracker.rejectionsForWrite(jobID)
	key := rejectionKey{plugin: plugin, source: source}
	aggregate, ok := rejectionsByKey[key]
	if !ok {
		aggregate = &rejectionAggregate{tasks: sets.New[api.TaskID]()}
		rejectionsByKey[key] = aggregate
	}
	aggregate.tasks.Insert(tasks...)

	if !ok {
		if len(hintKeys) == 0 {
			return
		}
		aggregate.hintKeys = sets.New[unschedulable.HintKey](hintKeys...)
		if aggregate.hintKeys.Len() > unschedulable.MaxHintKeysPerPluginEvent {
			aggregate.hintKeys = nil
		}
		return
	}

	if aggregate.hintKeys == nil || len(hintKeys) == 0 {
		aggregate.hintKeys = nil
		return
	}
	aggregate.hintKeys.Insert(hintKeys...)
	if aggregate.hintKeys.Len() > unschedulable.MaxHintKeysPerPluginEvent {
		aggregate.hintKeys = nil
	}
}

// rejectionsForWrite returns the active evaluation aggregate for jobID, or the
// Session aggregate when the Job is not being evaluated in a rejection scope.
func (tracker *jobRejectionTracker) rejectionsForWrite(jobID api.JobID) map[rejectionKey]*rejectionAggregate {
	// Search from the innermost evaluation outwards. When Job and SubJob
	// evaluations are nested, a rejection belongs to the currently active
	// SubJob candidate first.
	for i := len(tracker.evaluationScopes) - 1; i >= 0; i-- {
		scope := &tracker.evaluationScopes[i]
		if scope.jobID != jobID {
			continue
		}
		if scope.rejections == nil {
			scope.rejections = make(map[rejectionKey]*rejectionAggregate)
		}
		return scope.rejections
	}

	// Without an active candidate evaluation, the caller has confirmed the
	// rejection and it belongs to the Session result.
	if tracker.recorded == nil {
		tracker.recorded = make(map[api.JobID]map[rejectionKey]*rejectionAggregate)
	}
	rejectionsByKey := tracker.recorded[jobID]
	if rejectionsByKey == nil {
		rejectionsByKey = make(map[rejectionKey]*rejectionAggregate)
		tracker.recorded[jobID] = rejectionsByKey
	}
	return rejectionsByKey
}

// rejectionsForJob returns the rejections accumulated for job this session.
func (ssn *Session) rejectionsForJob(jobID api.JobID) []unschedulable.Rejection {
	return ssn.jobRejections.list(jobID)
}

// list returns the confirmed rejections recorded for jobID.
func (tracker *jobRejectionTracker) list(jobID api.JobID) []unschedulable.Rejection {
	return buildRejections(tracker.recorded[jobID])
}

// buildRejections converts an aggregate into stable rejection values.
func buildRejections(rejectionsByKey map[rejectionKey]*rejectionAggregate) []unschedulable.Rejection {
	if len(rejectionsByKey) == 0 {
		return nil
	}
	rejections := make([]unschedulable.Rejection, 0, len(rejectionsByKey))
	for key, aggregate := range rejectionsByKey {
		var taskIDs []api.TaskID
		if aggregate.tasks.Len() > 0 {
			taskIDs = sets.List(aggregate.tasks)
			sort.Slice(taskIDs, func(i, j int) bool { return taskIDs[i] < taskIDs[j] })
		}
		var hintKeys []unschedulable.HintKey
		if aggregate.hintKeys != nil {
			hintKeys = sets.List(aggregate.hintKeys)
			sort.Slice(hintKeys, func(i, j int) bool { return hintKeys[i] < hintKeys[j] })
		}
		rejections = append(rejections, unschedulable.Rejection{
			Plugin:   key.plugin,
			Source:   key.source,
			Tasks:    taskIDs,
			HintKeys: hintKeys,
		})
	}
	sort.Slice(rejections, func(i, j int) bool {
		if rejections[i].Plugin != rejections[j].Plugin {
			return rejections[i].Plugin < rejections[j].Plugin
		}
		return rejections[i].Source < rejections[j].Source
	})
	return rejections
}

// collect runs evaluate in a new rejection scope and returns the rejections
// produced by that evaluation.
func (tracker *jobRejectionTracker) collect(jobID api.JobID, evaluate func()) []unschedulable.Rejection {
	// Remember the previous stack length so the deferred restore removes only
	// the scope created by this call and any nested scopes.
	scopeIndex := len(tracker.evaluationScopes)
	tracker.evaluationScopes = append(tracker.evaluationScopes, jobRejectionScope{jobID: jobID})
	defer tracker.restoreScopes(scopeIndex)

	evaluate()
	// The return value is built before the deferred restore removes this scope.
	return buildRejections(tracker.evaluationScopes[scopeIndex].rejections)
}

// restoreScopes restores the evaluation stack to its previous length.
func (tracker *jobRejectionTracker) restoreScopes(scopeIndex int) {
	tracker.evaluationScopes = tracker.evaluationScopes[:scopeIndex]
}

// CollectJobRejections runs evaluate inside a temporary rejection scope and
// returns only the rejections produced by that evaluation. Callers can then
// keep or discard those rejections after choosing an allocation candidate.
// Nested calls are isolated from their parent evaluation until the caller
// explicitly records the returned rejections.
func (ssn *Session) CollectJobRejections(jobID api.JobID, evaluate func()) []unschedulable.Rejection {
	if !ssn.unschedulableJobCacheEnabled {
		evaluate()
		return nil
	}

	return ssn.jobRejections.collect(jobID, evaluate)
}
