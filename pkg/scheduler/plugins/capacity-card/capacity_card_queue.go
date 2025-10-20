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
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// queueAttr is used to store the attributes of a queue.
type queueAttr struct {
	queueID        api.QueueID   // queue UID
	name           string        // queue name
	share          float64       // share = max(allocated/deserved) of all resources
	deserved       *api.Resource // deserved = min(realCapability, max(guarantee, queue.deserved))
	allocated      *api.Resource // allocated = sum(job.allocated) of all active jobs
	request        *api.Resource // request = sum(job.request) of all active jobs
	elastic        *api.Resource // elastic = job.allocated - job.minAvailable
	inqueue        *api.Resource // inqueue = sum(job.minAvailable) of all inqueue jobs
	capability     *api.Resource // the capability of a queue
	realCapability *api.Resource // the real capability of a queue = min(capability, guarantee + exceeded part of cluster)
	guarantee      *api.Resource // the guaranteed resource of a queue
}

// buildQueueAttrs builds the attributes for all queues in the session.
func (p *Plugin) buildQueueAttrs(ssn *framework.Session) bool {
	// initialize totalGuarantee from all queues.
	for _, queue := range ssn.Queues {
		guarantee := p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Guarantee.Resource)
		p.totalGuarantee.Add(guarantee)
	}

	// build attributes for Queues.
	for _, apiJob := range ssn.Jobs {
		if !p.buildQueueAttrByJob(ssn, apiJob) {
			return false
		}
	}

	// update queue deserved and print queue info.
	for _, qAttr := range p.queueOpts {
		if qAttr.realCapability != nil {
			qAttr.deserved.MinDimensionResource(qAttr.realCapability, api.Infinity)
		}
		qAttr.deserved = helpers.Max(qAttr.deserved, qAttr.guarantee)
		p.updateShare(qAttr)
		klog.V(4).Infof(
			"The attributes of queue <%s>: capacity: <%v>, realCapability <%v>, allocate <%v>, request <%v>",
			qAttr.name, qAttr.capability, qAttr.realCapability, qAttr.allocated, qAttr.request,
		)
	}

	// add the queue comparison function according to the queue's priority and share.
	ssn.AddQueueOrderFn(p.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)
		if lv.Queue.Spec.Priority != rv.Queue.Spec.Priority {
			// return negative means high priority
			return int(rv.Queue.Spec.Priority) - int(lv.Queue.Spec.Priority)
		}
		if p.queueOpts[lv.UID].share == p.queueOpts[rv.UID].share {
			return 0
		}
		if p.queueOpts[lv.UID].share < p.queueOpts[rv.UID].share {
			return -1
		}
		return 1
	})

	return true
}

// buildQueueAttrByJob builds/updates the attributes of a queue by a job.
func (p *Plugin) buildQueueAttrByJob(ssn *framework.Session, apiJob *api.JobInfo) bool {
	job, err := p.NewJobInfo(apiJob)
	if err != nil {
		klog.Errorf(
			"Failed to create jobInfo for job <%s/%s>: %+v",
			apiJob.Namespace, apiJob.Name, err,
		)
		return false
	}
	klog.V(5).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
	if _, found := p.queueOpts[job.Queue]; !found {
		queue := ssn.Queues[job.Queue]
		qAttr := p.newQueueAttr(queue)
		p.queueOpts[job.Queue] = qAttr
		klog.V(5).Infof("Created Queue attr for queue <%s>.", queue.Name)
	}

	// calculate allocated and request resource for running and pending tasks in a job to queue.
	qAttr := p.queueOpts[job.Queue]
	for status, tasks := range job.TaskStatusIndex {
		for _, t := range tasks {
			resReq, err := p.GetTaskRequestResources(t)
			if err != nil {
				klog.Warningf(
					"Failed to get request resource for task <%s/%s> in job <%s/%s>: %+v",
					t.Namespace, t.Name, apiJob.Namespace, apiJob.Name, err,
				)
				continue
			}
			if api.AllocatedStatus(status) {
				qAttr.allocated.Add(resReq)
				qAttr.request.Add(resReq)
			} else if status == api.Pending {
				qAttr.request.Add(resReq)
			}
		}
	}

	// calculate inqueue resource for pending jobs to queue.
	if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
		// deduct the resources of scheduling gated tasks in a job when calculating inqueued resources
		// so that it will not block other jobs from being inqueued.
		qAttr.inqueue.Add(job.DeductSchGatedResources(p.GetMinResources(job)))
	}

	// calculate inqueue resource for running jobs
	// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember'
	// will work on cases such as the following condition:
	// Considering a Spark job is completed(driver pod is completed) while the PodGroup keeps running,
	// the allocated resource will be reserved again if without the judgement.
	if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
		job.PodGroup.Spec.MinResources != nil &&
		int32(util.CalculateAllocatedTaskNum(job.JobInfo)) >= job.PodGroup.Spec.MinMember {
		inqueued := p.GetInqueueResource(job, job.allocated)
		qAttr.inqueue.Add(job.DeductSchGatedResources(inqueued))
	}
	qAttr.elastic.Add(p.GetElasticResources(job))
	klog.V(5).Infof(
		"Built queue <%s> with job <%s/%s>: allocated <%s>, request <%s>, inqueue <%s>",
		qAttr.name, job.Namespace, job.Name,
		qAttr.allocated.String(),
		qAttr.request.String(),
		qAttr.inqueue.String(),
	)

	return true
}

// newQueueAttr creates a new queueAttr from a QueueInfo object.
func (p *Plugin) newQueueAttr(queue *api.QueueInfo) *queueAttr {
	var (
		deserved   = p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Deserved)
		capability = p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Capability)
		guarantee  = p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Guarantee.Resource)
	)
	qAttr := &queueAttr{
		queueID:    queue.UID,
		name:       queue.Name,
		deserved:   deserved,
		capability: capability,
		guarantee:  guarantee,
		allocated:  api.EmptyResource(),
		request:    api.EmptyResource(),
		elastic:    api.EmptyResource(),
		inqueue:    api.EmptyResource(),
	}
	realCapability := api.ExceededPart(p.totalResource, p.totalGuarantee).Add(qAttr.guarantee)
	qAttr.realCapability = realCapability
	return qAttr
}

// newQueueResourceSupportingCard creates a new resource object from resource list
func (p *Plugin) newQueueResourceSupportingCard(q *scheduling.Queue, rl v1.ResourceList) *api.Resource {
	var (
		queueResource     = api.NewResource(rl)
		queueCardResource = GetCardResourceFromAnnotations(q.Annotations, QueueAnnotationKeyCardQuota)
	)
	queueResource.ScalarResources = make(map[v1.ResourceName]float64)
	for cardName, cardCountMilli := range queueCardResource.ScalarResources {
		queueResource.ScalarResources[cardName] = cardCountMilli
	}
	return queueResource
}

// buildQueueMetrics builds the metrics for all queues in the session.
func (p *Plugin) buildQueueMetrics(ssn *framework.Session) {
	for queueID, queueInfo := range ssn.Queues {
		queue := ssn.Queues[queueID]
		if attr, ok := p.queueOpts[queueID]; ok {
			metrics.UpdateQueueDeserved(
				attr.name, attr.deserved.MilliCPU, attr.deserved.Memory, attr.deserved.ScalarResources,
			)
			metrics.UpdateQueueAllocated(
				attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources,
			)
			metrics.UpdateQueueRequest(
				attr.name, attr.request.MilliCPU, attr.request.Memory, attr.request.ScalarResources,
			)
			if attr.capability != nil {
				metrics.UpdateQueueCapacity(
					attr.name, attr.capability.MilliCPU, attr.capability.Memory, attr.capability.ScalarResources,
				)
			}
			metrics.UpdateQueueRealCapacity(
				attr.name,
				attr.realCapability.MilliCPU, attr.realCapability.Memory,
				attr.realCapability.ScalarResources,
			)
			continue
		}
		deservedCPU, deservedMem, scalarResources := 0.0, 0.0, map[v1.ResourceName]float64{}
		if queue.Queue.Spec.Deserved != nil {
			attr := p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Deserved)
			deservedCPU = attr.MilliCPU
			deservedMem = attr.Memory
			scalarResources = attr.ScalarResources
		}
		metrics.UpdateQueueDeserved(queueInfo.Name, deservedCPU, deservedMem, scalarResources)
		metrics.UpdateQueueAllocated(queueInfo.Name, 0, 0, map[v1.ResourceName]float64{})
		metrics.UpdateQueueRequest(queueInfo.Name, 0, 0, map[v1.ResourceName]float64{})
		guarantee := api.EmptyResource()
		if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
			guarantee = p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Guarantee.Resource)
		}
		realCapacity := api.ExceededPart(p.totalResource, p.totalGuarantee).Add(guarantee)
		if len(queue.Queue.Spec.Capability) > 0 {
			capacity := p.newQueueResourceSupportingCard(queue.Queue, queue.Queue.Spec.Capability)
			realCapacity.MinDimensionResource(capacity, api.Infinity)
			metrics.UpdateQueueCapacity(
				queueInfo.Name, capacity.MilliCPU, capacity.Memory, capacity.ScalarResources,
			)
		}
		metrics.UpdateQueueRealCapacity(
			queueInfo.Name, realCapacity.MilliCPU, realCapacity.Memory, realCapacity.ScalarResources,
		)
	}
}

// updateShare updates the share of the queueAttr and records the metric.
func (p *Plugin) updateShare(attr *queueAttr) {
	updateQueueAttrShare(attr)
	metrics.UpdateQueueShare(attr.name, attr.share)
}

// updateQueueAttrShare updates the share of the queueAttr.
func updateQueueAttrShare(attr *queueAttr) {
	res := float64(0)
	for _, rn := range attr.deserved.ResourceNames() {
		res = max(res, helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn)))
	}
	attr.share = res
}

// GetInqueueResource returns reserved resource for running job whose part of pods have not been allocated resource.
func (p *Plugin) GetInqueueResource(job *JobInfo, allocated *api.Resource) *api.Resource {
	inqueue := &api.Resource{}
	minResources := p.GetMinResources(job)

	reservedCPU := float64(minResources.MilliCPU) - allocated.MilliCPU
	if reservedCPU > 0 {
		inqueue.MilliCPU = reservedCPU
	}

	reservedMemory := float64(minResources.Memory) - allocated.Memory
	if reservedMemory > 0 {
		inqueue.Memory = reservedMemory
	}

	for name, quantity := range minResources.ScalarResources {
		if inqueue.ScalarResources == nil {
			inqueue.ScalarResources = make(map[v1.ResourceName]float64)
		}
		if allocatedMount, ok := allocated.ScalarResources[name]; !ok {
			inqueue.ScalarResources[name] = quantity
		} else {
			reservedScalarRes := quantity - allocatedMount
			if reservedScalarRes > 0 {
				inqueue.ScalarResources[name] = reservedScalarRes
			}
		}
	}
	return inqueue
}
