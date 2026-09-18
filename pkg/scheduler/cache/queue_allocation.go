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
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/features"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// Per queue resource accounting maintained in the scheduler cache (issue #5565).
// Queue plugins rebuild per queue totals from scratch every OnSessionOpen. Here
// the cache keeps those totals updated in the pod/podgroup event handlers, the
// same way node totals are kept via node.AddTask/RemoveTask.
//
// The whole thing is gated by CacheQueueAccounting and does nothing while disabled.
//
// Invariant checked by ReconcileQueueAllocations:
//
//	queue.Allocated == sum(job.Allocated)    over jobs with a non nil PodGroup
//	queue.Request   == sum(job.TotalRequest) and a resolved queue
//
// That job set matches what Snapshot exposes to plugins, so the cache value is
// comparable to what plugins compute today.
//
// Each job's current contribution is stored in jobQueueContribution. Every event
// calls updateQueueForJob, which removes the stored contribution and applies the
// current one. This stays correct no matter the event order (for example a job
// that lingers with a stale queue after its podgroup is deleted).
//
// The mutation helpers assume sc.Mutex is held (true in the event handlers).
// ReconcileQueueAllocations takes the lock itself.

// queueContribution is what a single job currently adds to a queue.
type queueContribution struct {
	queue     schedulingapi.QueueID
	allocated *schedulingapi.Resource
	request   *schedulingapi.Resource
}

// queueAllocationEnabled reports whether cache side queue accounting is on.
func (sc *SchedulerCache) queueAllocationEnabled() bool {
	return utilfeature.DefaultFeatureGate.Enabled(features.CacheQueueAccounting)
}

// jobAccountedInQueue reports whether a job currently counts towards a queue. A
// job counts once it has a resolved PodGroup and a non empty queue id.
func jobAccountedInQueue(job *schedulingapi.JobInfo) bool {
	return job != nil && job.PodGroup != nil && len(job.Queue) != 0
}

// updateQueueForJob re-applies a single job's contribution to the queue totals.
// It subtracts whatever the job contributed before and adds its current value.
// Safe to call after any event that may change the job's totals or queue.
func (sc *SchedulerCache) updateQueueForJob(job *schedulingapi.JobInfo) {
	if !sc.queueAllocationEnabled() || job == nil {
		return
	}
	if sc.jobQueueContribution == nil {
		sc.jobQueueContribution = make(map[schedulingapi.JobID]*queueContribution)
	}

	if prev, ok := sc.jobQueueContribution[job.UID]; ok {
		sc.subQueueAllocation(prev.queue, prev.allocated, prev.request)
		delete(sc.jobQueueContribution, job.UID)
	}

	if !jobAccountedInQueue(job) {
		return
	}
	allocated := schedulingapi.EmptyResource()
	if job.Allocated != nil {
		allocated = job.Allocated.Clone()
	}
	request := schedulingapi.EmptyResource()
	if job.TotalRequest != nil {
		request = job.TotalRequest.Clone()
	}
	sc.addQueueAllocation(job.Queue, allocated, request)
	sc.jobQueueContribution[job.UID] = &queueContribution{
		queue:     job.Queue,
		allocated: allocated,
		request:   request,
	}
}

// addQueueAllocation adds the given amounts to a queue's totals. An unknown queue
// is skipped and gets rebuilt when it is added (reconcileQueueFromJobsLocked).
func (sc *SchedulerCache) addQueueAllocation(qid schedulingapi.QueueID, allocated, request *schedulingapi.Resource) {
	q := sc.queueForAccounting(qid)
	if q == nil {
		return
	}
	if allocated != nil {
		q.Allocated.Add(allocated)
	}
	if request != nil {
		q.Request.Add(request)
	}
}

// subQueueAllocation subtracts the given amounts from a queue's totals. It uses
// SubWithoutAssert so a transient inconsistency logs instead of panicking.
func (sc *SchedulerCache) subQueueAllocation(qid schedulingapi.QueueID, allocated, request *schedulingapi.Resource) {
	q := sc.queueForAccounting(qid)
	if q == nil {
		return
	}
	if allocated != nil {
		q.Allocated.SubWithoutAssert(allocated)
	}
	if request != nil {
		q.Request.SubWithoutAssert(request)
	}
}

// queueForAccounting returns the QueueInfo for qid with its total fields ready,
// or nil when the id is empty or the queue is not in the cache.
func (sc *SchedulerCache) queueForAccounting(qid schedulingapi.QueueID) *schedulingapi.QueueInfo {
	if len(qid) == 0 {
		return nil
	}
	q, ok := sc.Queues[qid]
	if !ok {
		return nil
	}
	if q.Allocated == nil {
		q.Allocated = schedulingapi.EmptyResource()
	}
	if q.Request == nil {
		q.Request = schedulingapi.EmptyResource()
	}
	return q
}

// reconcileQueueFromJobsLocked rebuilds one queue's totals from the per job
// totals and refreshes the stored contributions for that queue. Used when a
// queue is added, since addQueue replaces the QueueInfo with a fresh object and
// a queue may be created after jobs already reference it. Assumes sc.Mutex held.
func (sc *SchedulerCache) reconcileQueueFromJobsLocked(qid schedulingapi.QueueID) {
	q := sc.queueForAccounting(qid)
	if q == nil {
		return
	}
	if sc.jobQueueContribution == nil {
		sc.jobQueueContribution = make(map[schedulingapi.JobID]*queueContribution)
	}
	q.Allocated = schedulingapi.EmptyResource()
	q.Request = schedulingapi.EmptyResource()
	for _, job := range sc.Jobs {
		if job.Queue != qid || !jobAccountedInQueue(job) {
			continue
		}
		allocated := schedulingapi.EmptyResource()
		if job.Allocated != nil {
			allocated = job.Allocated.Clone()
		}
		request := schedulingapi.EmptyResource()
		if job.TotalRequest != nil {
			request = job.TotalRequest.Clone()
		}
		q.Allocated.Add(allocated)
		q.Request.Add(request)
		sc.jobQueueContribution[job.UID] = &queueContribution{
			queue:     qid,
			allocated: allocated,
			request:   request,
		}
	}
}

// ReconcileQueueAllocations rebuilds every queue's totals from the per job totals
// and rebuilds the contribution map. It is the ground truth used as a drift
// safety net and by tests to check the incremental value.
//
// It takes sc.Mutex, so do not call it while already holding the lock.
func (sc *SchedulerCache) ReconcileQueueAllocations() {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.reconcileQueueAllocationsLocked()
}

// reconcileQueueAllocationsLocked is ReconcileQueueAllocations with the lock held.
func (sc *SchedulerCache) reconcileQueueAllocationsLocked() {
	sc.jobQueueContribution = make(map[schedulingapi.JobID]*queueContribution)
	for _, q := range sc.Queues {
		q.Allocated = schedulingapi.EmptyResource()
		q.Request = schedulingapi.EmptyResource()
	}
	for _, job := range sc.Jobs {
		if !jobAccountedInQueue(job) {
			continue
		}
		q, ok := sc.Queues[job.Queue]
		if !ok {
			klog.V(5).Infof("Job <%s/%s> references queue <%s> not present in cache, skipping in reconcile",
				job.Namespace, job.Name, job.Queue)
			continue
		}
		allocated := schedulingapi.EmptyResource()
		if job.Allocated != nil {
			allocated = job.Allocated.Clone()
		}
		request := schedulingapi.EmptyResource()
		if job.TotalRequest != nil {
			request = job.TotalRequest.Clone()
		}
		q.Allocated.Add(allocated)
		q.Request.Add(request)
		sc.jobQueueContribution[job.UID] = &queueContribution{
			queue:     job.Queue,
			allocated: allocated,
			request:   request,
		}
	}
}
