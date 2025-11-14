/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// JobEnqueueableFn checks whether the job can be enqueued, which does the queue-level capacity pre-check.
// It handles pending PodGroups, and checks whether the queue has enough resource to proceed.
// If it returns util.Permit, which will do the following aspects to resources:
// 1. PogGroup phase will be changed from Pending to InQueue.
// 2. Pod will be created and in phase of Pending.
func (p *Plugin) JobEnqueueableFn(ssn *framework.Session, jobInfo *api.JobInfo) int {
	job, err := p.NewJobInfo(jobInfo)
	if err != nil {
		klog.Errorf(
			"Failed to create jobInfo for job <%s/%s>: %+v",
			jobInfo.Namespace, jobInfo.Name, err,
		)
		return util.Reject
	}

	var (
		queueID = job.Queue
		queue   = ssn.Queues[queueID]
		qAttr   = p.queueOpts[queueID]
	)
	// If the queue is not open, do not enqueue
	if queue.Queue.Status.State != scheduling.QueueStateOpen {
		klog.V(3).Infof(
			"Queue <%s> current state: %s, is not open state, reject job <%s/%s>.",
			queue.Name, queue.Queue.Status.State, job.Namespace, job.Name,
		)
		return util.Reject
	}

	// it checks whether the queue has enough resource to run the job.
	if !p.isJobEnqueueable(ssn, qAttr, job) {
		klog.V(2).Infof(
			"Queue <%s> has no enough resource for job <%s/%s>",
			queue.Name, job.Namespace, job.Name,
		)
		return util.Reject
	}

	// job enqueued
	deductedResources := job.DeductSchGatedResources(p.GetMinResources(job))
	qAttr.inqueue.Add(deductedResources)
	klog.V(5).Infof("Job <%s/%s> enqueued", job.Namespace, job.Name)
	return util.Permit
}

// isJobEnqueueable checks whether the job can be enqueued in the queue according to the queue's real capability.
func (p *Plugin) isJobEnqueueable(ssn *framework.Session, qAttr *queueAttr, job *JobInfo) bool {
	var (
		jobReqResource  = p.GetMinResources(job)
		queueCapability = qAttr.capability
		totalToBeUsed   = jobReqResource.Clone().
				Add(qAttr.allocated).
				Add(qAttr.inqueue).
				Sub(qAttr.elastic)
	)
	klog.V(5).Infof(
		"Job <%s/%s> min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
		job.Namespace, job.Name, jobReqResource.String(), qAttr.name,
		queueCapability.String(),
		qAttr.allocated.String(),
		qAttr.inqueue.String(),
		qAttr.elastic.String(),
	)

	if jobReqResource == nil {
		if ok := totalToBeUsed.LessEqual(queueCapability, api.Zero); !ok {
			klog.Warningf(
				"Job <%s/%s>, Queue <%s> capability <%s> is empty, deny it to enqueue",
				job.Namespace, job.Name, qAttr.name, queueCapability.String(),
			)
			ssn.RecordPodGroupEvent(
				job.PodGroup, v1.EventTypeWarning, EventTypeEmptyQueueCapability,
				fmt.Sprintf(
					"Queue <%s> capability <%s> is empty, deny it to enqueue",
					qAttr.name, queueCapability.String(),
				),
			)
			return false
		}
		klog.V(5).Infof(
			"Job <%s/%s>, Queue <%s> request is nil, allow it to enqueue",
			job.Namespace, job.Name, qAttr.name,
		)
		return true
	}

	if totalToBeUsed == nil {
		klog.V(5).Infof(
			"Job <%s/%s>, Queue <%s> totalToBeUsed is nil, allow it to enqueue",
			job.Namespace, job.Name, qAttr.name,
		)
		return true
	}

	// check cpu and memory
	if jobReqResource.MilliCPU > 0 && totalToBeUsed.MilliCPU > queueCapability.MilliCPU {
		klog.Warningf(
			"Job <%s/%s>, Queue <%s> has no enough CPU, request <%v>, total would be <%v>, capability <%v>",
			job.Namespace, job.Name, qAttr.name,
			jobReqResource.MilliCPU, totalToBeUsed.MilliCPU, queueCapability.MilliCPU,
		)
		ssn.RecordPodGroupEvent(
			job.PodGroup, v1.EventTypeWarning, EventTypeInsufficientCPUQuota,
			fmt.Sprintf(
				"Queue <%s> has insufficient CPU quota: requested <%v>, total would be <%v>, but capability is <%v>",
				qAttr.name, jobReqResource.MilliCPU, totalToBeUsed.MilliCPU, queueCapability.MilliCPU,
			),
		)
		return false
	}
	if jobReqResource.Memory > 0 && totalToBeUsed.Memory > queueCapability.Memory {
		var (
			jobReqResourceMi  = jobReqResource.Memory / 1024 / 1024
			totalToBeUsedMi   = totalToBeUsed.Memory / 1024 / 1024
			queueCapabilityMi = queueCapability.Memory / 1024 / 1024
		)
		klog.Warningf(
			"Job <%s/%s>, Queue <%s> has no enough Memory, request <%v Mi>, total would be <%v Mi>, capability <%v Mi>",
			job.Namespace, job.Name, qAttr.name,
			jobReqResourceMi, totalToBeUsedMi, queueCapabilityMi,
		)
		ssn.RecordPodGroupEvent(
			job.PodGroup, v1.EventTypeWarning, EventTypeInsufficientMemoryQuota,
			fmt.Sprintf(
				"Queue %s has insufficient memory quota: requested <%v Mi>, total would be <%v Mi>, but capability is <%v Mi>",
				qAttr.name, jobReqResourceMi, totalToBeUsedMi, queueCapabilityMi,
			),
		)
		return false
	}

	// if r.scalar is nil, whatever rr.scalar is, r is less or equal to rr
	if totalToBeUsed.ScalarResources == nil {
		return true
	}

	for scalarName, scalarQuant := range jobReqResource.ScalarResources {
		if api.IsIgnoredScalarResource(scalarName) {
			continue
		}
		checkResult := CheckSingleScalarResource(
			scalarName, scalarQuant, totalToBeUsed, queueCapability, CheckModeJob,
		)
		if checkResult.Ok {
			continue
		}
		klog.Warningf(
			"Job <%s/%s>, Queue <%s> has no enough %s, request <%v>, total would be <%v>, capability <%v>",
			job.Namespace, job.Name, qAttr.name,
			checkResult.NoEnoughScalarName,
			checkResult.NoEnoughScalarCount,
			checkResult.ToBeUsedScalarQuant,
			checkResult.QueueCapabilityQuant,
		)
		ssn.RecordPodGroupEvent(
			job.PodGroup, v1.EventTypeWarning, EventTypeInsufficientScalarQuota,
			fmt.Sprintf(
				"Queue <%s> has insufficient <%s> quota: requested <%v>, total would be <%v>, but capability is <%v>",
				qAttr.name, checkResult.NoEnoughScalarName, checkResult.NoEnoughScalarCount,
				checkResult.ToBeUsedScalarQuant, checkResult.QueueCapabilityQuant,
			),
		)
		return false
	}
	return true
}
