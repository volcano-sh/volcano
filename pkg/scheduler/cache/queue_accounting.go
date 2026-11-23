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
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// Direct per-queue resource accounting (QueueInfo.DirectAllocated and
// QueueInfo.DirectRequest).
//
// The cache updates these totals on events, as it does node totals with
// node.AddTask and node.RemoveTask. sc.jobQueueContribution records what each
// job currently adds to its queue. Every cache change to a job's tasks, task
// statuses, PodGroup or queue calls updateQueueForJob, which subtracts the recorded
// contribution and adds the current one, so the result does not depend on
// event order.
//
// All helpers assume sc.Mutex is held.

// queueContribution is what one job currently adds to its queue's totals.
type queueContribution struct {
	queue     schedulingapi.QueueID
	allocated *schedulingapi.Resource
	request   *schedulingapi.Resource
}

// updateQueueForJob re-applies a job's contribution to its queue's totals.
// Call it after any change to the job's tasks, PodGroup or queue.
func (sc *SchedulerCache) updateQueueForJob(job *schedulingapi.JobInfo) {
	if job == nil {
		return
	}
	if sc.jobQueueContribution == nil {
		sc.jobQueueContribution = make(map[schedulingapi.JobID]*queueContribution)
	}

	c, found := sc.jobQueueContribution[job.UID]
	if found {
		if q := sc.Queues[c.queue]; q != nil {
			q.DirectAllocated.SubWithoutAssert(c.allocated)
			q.DirectRequest.SubWithoutAssert(c.request)
		}
	}

	// Snapshot only exposes jobs with a PodGroup, so only those are counted.
	if job.PodGroup == nil || len(job.Queue) == 0 {
		delete(sc.jobQueueContribution, job.UID)
		return
	}
	// This runs on every pod event under sc.Mutex, so the contribution is
	// allocated once per job and overwritten in place afterwards.
	if !found {
		c = &queueContribution{allocated: schedulingapi.EmptyResource(), request: schedulingapi.EmptyResource()}
		sc.jobQueueContribution[job.UID] = c
	}
	c.queue = job.Queue
	overwriteResource(c.allocated, job.Allocated)
	overwriteResource(c.request, job.Allocated)
	c.request.Add(job.PendingRequest)
	// The queue may not be in the cache yet; setQueueInfo picks the
	// contribution up when it arrives.
	if q := sc.Queues[c.queue]; q != nil {
		q.DirectAllocated.Add(c.allocated)
		q.DirectRequest.Add(c.request)
	}
}

// overwriteResource sets dst to the accounted amounts of src (what Add sums),
// reusing dst's scalar map so a steady-state update does not allocate.
func overwriteResource(dst, src *schedulingapi.Resource) {
	dst.MilliCPU, dst.Memory = 0, 0
	clear(dst.ScalarResources)
	dst.Add(src)
}

// setQueueInfo stores a new or updated QueueInfo in the cache. Direct totals do
// not depend on the queue's spec or status, so an update copies them from the
// object it replaces. A queue seen for the first time sums the recorded job
// contributions, because jobs can reference a queue before it is added; that
// costs O(accounted jobs) and happens once per queue add.
func (sc *SchedulerCache) setQueueInfo(qi *schedulingapi.QueueInfo) {
	if prev, ok := sc.Queues[qi.UID]; ok {
		qi.DirectAllocated, qi.DirectRequest = prev.DirectAllocated, prev.DirectRequest
	} else {
		qi.DirectAllocated, qi.DirectRequest = schedulingapi.EmptyResource(), schedulingapi.EmptyResource()
		for _, c := range sc.jobQueueContribution {
			if c.queue == qi.UID {
				qi.DirectAllocated.Add(c.allocated)
				qi.DirectRequest.Add(c.request)
			}
		}
	}
	sc.Queues[qi.UID] = qi
}
