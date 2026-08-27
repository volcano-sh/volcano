/*
Copyright 2025 The Volcano Authors.

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
	"context"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// Volcano-specific event resources that are not part of the kube-scheduler
// framework resource set.
const (
	PodGroupEvent  fwk.EventResource = "PodGroup"
	QueueEvent     fwk.EventResource = "Queue"
	HyperNodeEvent fwk.EventResource = "HyperNode"
	NumaInfoEvent  fwk.EventResource = "NumaInfo"

	// MaxHintKeysPerPluginEvent bounds the HintKeys one Job or incoming event may
	// contribute for a plugin event. An over-limit Job is indexed without
	// HintKeys; an over-limit event selects every Job handled by that event.
	MaxHintKeysPerPluginEvent = 256
)

// eventKey is the semantic identity used by registries and freshness tracking.
// ClusterEvent.CustomLabel affects only presentation and therefore is excluded.
type eventKey struct {
	resource   fwk.EventResource
	actionType fwk.ActionType
}

func newEventKey(event fwk.ClusterEvent) eventKey {
	return eventKey{resource: event.Resource, actionType: event.ActionType}
}

func (k eventKey) clusterEvent() fwk.ClusterEvent {
	return fwk.ClusterEvent{Resource: k.resource, ActionType: k.actionType}
}

// HintResult is the decision a JobHintFn returns for one Job on one event.
type HintResult int

const (
	// HintSkip means the event cannot unblock this Job; keep it cached.
	HintSkip HintResult = iota
	// HintWakeup means the event may unblock this Job; drop the record.
	HintWakeup
)

// JobHintFn is invoked when a registered cluster event fires for a Job that the
// plugin previously rejected. It reports whether this event may make the Job
// schedulable.
//
//	job       the cached Job under evaluation.
//	rejection the plugin's own rejection from the previous session; for predicate
//	          sources it also carries the task IDs that failed.
//	oldObj    object state before the change; nil on Add events.
//	newObj    object state after the change; nil on Delete events.
//
// A non-nil error is treated as HintWakeup by the caller.
type JobHintFn func(
	job *api.JobInfo,
	rejection Rejection,
	oldObj, newObj any,
) (HintResult, error)

// HintKey identifies a necessary scheduling condition shared by a rejected Job
// and an incoming plugin event. Matching keys narrow the candidate Jobs.
type HintKey string

// JobKeysFn returns the necessary-condition keys for one rejected Job and one
// plugin event. If the paired HintFn can return HintWakeup, this key
// set must share at least one key with EventKeysFn's result for that event, or
// the cache cannot narrow dispatch safely. It must return an error when it
// cannot construct a complete key set; callers treat an empty result, an error,
// or an over-limit result by selecting this Job without HintKeys.
type JobKeysFn func(job *api.JobInfo, rejection Rejection) ([]HintKey, error)

// EventKeysFn returns the necessary-condition keys for one incoming event
// object pair. If the paired HintFn can return HintWakeup, this key set must
// share at least one key with JobKeysFn's result for that plugin event, or the
// cache cannot narrow dispatch safely. An empty successful result means that
// this event has no indexed candidates. It must return an error when it cannot
// construct a complete key set; callers treat an error or an over-limit result
// by selecting every Job handled by that plugin event.
type EventKeysFn func(oldObj, newObj any) ([]HintKey, error)

// EventWithHint pairs one cluster event a plugin cares about with the
// callbacks used to narrow candidate Jobs and check whether that event may help
// a specific Job. JobKeysFn and EventKeysFn are optional as a pair. A nil HintFn
// means every occurrence of Event wakes Jobs blocked by this plugin. A provider
// must declare each exact Event at most once; duplicate declarations invalidate
// that provider so the cache fails open. When a plugin registers the same Event
// in later Sessions, its paired JobKeysFn and EventKeysFn must preserve their
// HintKey namespace and extraction semantics because one index can contain Jobs
// recorded under multiple registrations.
type EventWithHint struct {
	Event       fwk.ClusterEvent
	JobKeysFn   JobKeysFn
	EventKeysFn EventKeysFn
	HintFn      JobHintFn
}

// HintProvider lets a plugin declare the events that can change its previous
// unschedulable decisions.
type HintProvider interface {
	// EventsToRegister returns every event and hint pair handled by this plugin.
	EventsToRegister(ctx context.Context) ([]EventWithHint, error)
}

// RejectionSource names the extension point that emitted a rejection.
type RejectionSource string

const (
	// RejectionPredicate comes from a PredicateFn or PrePredicateFn failure, or
	// from the built-in node resource check.
	RejectionPredicate RejectionSource = "predicate"
	// RejectionAllocatable comes from the Allocatable extension point.
	RejectionAllocatable RejectionSource = "allocatable"
	// RejectionEnqueue comes from the JobEnqueueable extension point.
	RejectionEnqueue RejectionSource = "enqueue"
)

// Rejection describes one plugin decision that made a Job unschedulable in a session.
type Rejection struct {
	// Plugin is the registered HintProvider name, e.g. "predicates/nodeaffinity".
	Plugin string
	// Source is the extension point that emitted the rejection.
	Source RejectionSource
	// Tasks holds the failed task IDs; nil only for RejectionEnqueue, which is
	// a whole-PodGroup decision.
	Tasks []api.TaskID
	// HintKeys holds the optional necessary-condition keys that accompanied this
	// rejection. Empty when the session fell back to coarse dispatch for this
	// plugin/source aggregate.
	HintKeys []HintKey
	// Queues is the Job's queue and its ancestors, populated at record time.
	// A resource change confined to a queue outside this set cannot affect a
	// quota decision for the Job, so quota-plugin hints use it to scope wakeups.
	// Empty when the recording context has no queue hierarchy available.
	Queues []api.QueueID
}

// ComputeSkip turns the cached rejections into an api.SkipDecision for the Job. The
// RejectionEnqueue source sets Enqueue; per-task sources set Allocate when the
// Job can no longer reach its gang criterion, otherwise they list the tasks to
// skip.
func ComputeSkip(job *api.JobInfo, rejections []Rejection) api.SkipDecision {
	var d api.SkipDecision
	tasks := map[api.TaskID]struct{}{}
	for _, r := range rejections {
		if r.Source == RejectionEnqueue {
			d.Enqueue = true
			continue
		}
		for _, t := range r.Tasks {
			tasks[t] = struct{}{}
		}
	}
	if len(tasks) == 0 {
		return d
	}
	if !gangMinimumsReachable(job, tasks) {
		d.Allocate = true
		return d
	}
	d.Tasks = tasks
	return d
}

// gangMinimumsReachable reports whether job can still satisfy every gang minimum after the
// tasks in skippedPendingTasks are treated as unschedulable this session. The scheduler
// enforces three independent gang minimums, and the Job stays reachable only
// while all of them still hold:
//
//   - job-level: the total task count meets MinAvailable;
//   - per-role: each role keeps its TaskMinAvailable, enforced only when
//     MinAvailable covers the sum of per-role minimums;
//   - per-subgroup: enough subJobs in each group can
//     independently reach their own MinAvailable to satisfy MinSubJobs.
//
// Only pending tasks are removable: a task already holding or reserving
// resources counts regardless of whether it appears in skippedPendingTasks.
func gangMinimumsReachable(job *api.JobInfo, skippedPendingTasks map[api.TaskID]struct{}) bool {
	members := countPotentialGangMembers(job.TaskStatusIndex, skippedPendingTasks)

	// Job-level minimum.
	if members.total < job.MinAvailable {
		return false
	}

	// Per-role minimums are enforced only when they do not exceed the Job-level minimum.
	if job.MinAvailable >= job.TaskMinAvailableTotal {
		for role, minMember := range job.TaskMinAvailable {
			if members.byRole[role] < minMember {
				return false
			}
		}
	}

	// Count the subJobs in each group that can still reach their own MinAvailable,
	// then require enough of them per MinSubJobs.
	if len(job.MinSubJobs) > 0 {
		reachableSubJobs := map[api.SubJobGID]int32{}
		for _, sj := range job.SubJobs {
			if countPotentialGangMembers(sj.TaskStatusIndex, skippedPendingTasks).total >= sj.MinAvailable {
				reachableSubJobs[sj.GID]++
			}
		}
		for gid, minSubJob := range job.MinSubJobs {
			if reachableSubJobs[gid] < minSubJob {
				return false
			}
		}
	}

	return true
}

type gangMemberCounts struct {
	total  int32
	byRole map[string]int32
}

// countPotentialGangMembers counts tasks that can still contribute to a gang
// minimum. Allocated, Binding, Bound, Running, Succeeded, and Pipelined tasks
// always count. Pending tasks count unless rejected in the previous session.
func countPotentialGangMembers(taskStatusIndex map[api.TaskStatus]api.TasksMap, skippedPendingTasks map[api.TaskID]struct{}) gangMemberCounts {
	counts := gangMemberCounts{
		byRole: make(map[string]int32),
	}
	for status, tasks := range taskStatusIndex {
		skipRejected := false
		switch {
		case api.AllocatedStatus(status), status == api.Succeeded, status == api.Pipelined:
		// A Job below its gang minimum can still contain progressed tasks:
		//  1. A previously ready Job may retain Running tasks after losing other members(e.g. preemption or eviction).
		//  2. Retried tasks may be Pending while earlier members remain Bound or Running.
		//  3. Scaling up a gang minimum leaves existing members running while new members are Pending.
		case status == api.Pending:
			skipRejected = true
		default:
			continue
		}

		for _, task := range tasks {
			if skipRejected {
				if _, rejected := skippedPendingTasks[task.UID]; rejected {
					continue
				}
			}
			counts.total++
			counts.byRole[task.TaskRole]++
		}
	}
	return counts
}
