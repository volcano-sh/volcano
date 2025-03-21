/*
Copyright 2024 The Volcano Authors.

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

package capacity

import (
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	PluginName  = "capacity"
	rootQueueID = "root"
)

type capacityPlugin struct {
	rootQueue      string
	totalResource  *api.Resource
	totalGuarantee *api.Resource

	queueOpts map[api.QueueID]*queueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

type queueAttr struct {
	queueID   api.QueueID
	name      string
	share     float64
	ancestors []api.QueueID
	children  map[api.QueueID]*queueAttr

	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
	// elastic represents the sum of job's elastic resource, job's elastic = job.allocated - job.minAvailable
	elastic *api.Resource
	// inqueue represents the resource request of the inqueue job
	inqueue    *api.Resource
	capability *api.Resource
	// realCapability represents the resource limit of the queue, LessEqual capability
	realCapability *api.Resource
	guarantee      *api.Resource
}

// New return capacityPlugin action
func New(arguments framework.Arguments) framework.Plugin {
	return &capacityPlugin{
		totalResource:   api.EmptyResource(),
		totalGuarantee:  api.EmptyResource(),
		queueOpts:       map[api.QueueID]*queueAttr{},
		pluginArguments: arguments,
	}
}

func (cp *capacityPlugin) Name() string {
	return PluginName
}

// HierarchyEnabled returns if hierarchy is enabled
func (cp *capacityPlugin) HierarchyEnabled(ssn *framework.Session) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != PluginName {
				continue
			}
			return plugin.EnabledHierarchy != nil && *plugin.EnabledHierarchy
		}
	}
	return false
}

func (cp *capacityPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	cp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", cp.totalResource)

	hierarchyEnabled := cp.HierarchyEnabled(ssn)
	readyToSchedule := true
	if hierarchyEnabled {
		readyToSchedule = cp.buildHierarchicalQueueAttrs(ssn)
	} else {
		cp.buildQueueAttrs(ssn)
	}

	ssn.AddReclaimableFn(cp.Name(), func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return victims, util.Reject
		}

		for _, reclaimee := range reclaimees {
			job := ssn.Jobs[reclaimee.Job]
			attr := cp.queueOpts[job.Queue]

			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.LessPartly(reclaimer.Resreq, api.Zero) {
				klog.V(3).Infof("Failed to allocate resource for Task <%s/%s> in Queue <%s>, not enough resource.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			exceptReclaimee := allocated.Clone().Sub(reclaimee.Resreq)
			// When scalar resource not specified in deserved such as "pods", we should skip it and consider it as infinity,
			// so the following first condition will be true and the current queue will not be reclaimed.
			if allocated.LessEqual(attr.deserved, api.Infinity) || !attr.guarantee.LessEqual(exceptReclaimee, api.Zero) {
				continue
			}
			allocated.Sub(reclaimee.Resreq)
			victims = append(victims, reclaimee)
		}
		klog.V(4).Infof("Victims from capacity plugin, victims=%+v reclaimer=%s", victims, reclaimer)
		return victims, util.Permit
	})

	ssn.AddPreemptiveFn(cp.Name(), func(obj interface{}, candidate interface{}) bool {
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return false
		}

		queue := obj.(*api.QueueInfo)
		task := candidate.(*api.TaskInfo)
		attr := cp.queueOpts[queue.UID]

		futureUsed := attr.allocated.Clone().Add(task.Resreq)
		overused := !futureUsed.LessEqualWithDimension(attr.deserved, task.Resreq)
		metrics.UpdateQueueOverused(attr.name, overused)
		if overused {
			klog.V(3).Infof("Queue <%v> can not reclaim, deserved <%v>, allocated <%v>, share <%v>, requested <%v>",
				queue.Name, attr.deserved, attr.allocated, attr.share, task.Resreq)
		}

		// PreemptiveFn is the opposite of OverusedFn in proportion plugin cause as long as there is a one-dimensional
		// resource whose deserved is greater than allocated, current task can reclaim by preempt others.
		return !overused
	})

	ssn.AddAllocatableFn(cp.Name(), func(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return false
		}
		if hierarchyEnabled && !cp.isLeafQueue(queue.UID) {
			klog.V(3).Infof("Queue <%s> is not a leaf queue, can not allocate task <%s>.", queue.Name, candidate.Name)
			return false
		}

		return cp.checkQueueAllocatableHierarchically(ssn, queue, candidate)
	})

	ssn.AddJobEnqueueableFn(cp.Name(), func(obj interface{}) int {
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return util.Reject
		}

		job := obj.(*api.JobInfo)
		queueID := job.Queue
		if hierarchyEnabled && !cp.isLeafQueue(queueID) {
			return util.Reject
		}

		attr := cp.queueOpts[queueID]
		queue := ssn.Queues[queueID]
		// If no capability is set, always enqueue the job.
		if attr.realCapability == nil {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}

		if !cp.checkJobEnqueueableHierarchically(ssn, queue, job) {
			return util.Reject
		}

		// job enqueued
		deductedResources := job.DeductSchGatedResources(job.GetMinResources())
		attr.inqueue.Add(deductedResources)
		// If enable hierarchy, update the inqueue resource for all ancestors queues
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := cp.queueOpts[ancestorID]
				ancestorAttr.inqueue.Add(deductedResources)
			}
		}
		klog.V(5).Infof("job <%s/%s> enqueued", job.Namespace, job.Name)
		return util.Permit
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := cp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			cp.updateShare(attr)
			if hierarchyEnabled {
				for _, ancestorID := range attr.ancestors {
					ancestorAttr := cp.queueOpts[ancestorID]
					ancestorAttr.allocated.Add(event.Task.Resreq)
				}
			}

			klog.V(4).Infof("Capacity AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := cp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			cp.updateShare(attr)
			if hierarchyEnabled {
				for _, ancestorID := range attr.ancestors {
					ancestorAttr := cp.queueOpts[ancestorID]
					ancestorAttr.allocated.Sub(event.Task.Resreq)
				}
			}

			klog.V(4).Infof("Capacity EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
	})
}

func (cp *capacityPlugin) OnSessionClose(ssn *framework.Session) {
	cp.totalResource = nil
	cp.totalGuarantee = nil
	cp.queueOpts = nil
}

func (cp *capacityPlugin) buildQueueAttrs(ssn *framework.Session) {
	for _, queue := range ssn.Queues {
		if len(queue.Queue.Spec.Guarantee.Resource) == 0 {
			continue
		}
		guarantee := api.NewResource(queue.Queue.Spec.Guarantee.Resource)
		cp.totalGuarantee.Add(guarantee)
	}
	klog.V(4).Infof("The total guarantee resource is <%v>", cp.totalGuarantee)
	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := cp.queueOpts[job.Queue]; !found {
			queue := ssn.Queues[job.Queue]
			attr := &queueAttr{
				queueID: queue.UID,
				name:    queue.Name,

				deserved:  api.NewResource(queue.Queue.Spec.Deserved),
				allocated: api.EmptyResource(),
				request:   api.EmptyResource(),
				elastic:   api.EmptyResource(),
				inqueue:   api.EmptyResource(),
				guarantee: api.EmptyResource(),
			}
			if len(queue.Queue.Spec.Capability) != 0 {
				attr.capability = api.NewResource(queue.Queue.Spec.Capability)
				if attr.capability.MilliCPU <= 0 {
					attr.capability.MilliCPU = math.MaxFloat64
				}
				if attr.capability.Memory <= 0 {
					attr.capability.Memory = math.MaxFloat64
				}
			}
			if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
				attr.guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
			}
			realCapability := api.ExceededPart(cp.totalResource, cp.totalGuarantee).Add(attr.guarantee)
			if attr.capability == nil {
				attr.realCapability = realCapability
			} else {
				realCapability.MinDimensionResource(attr.capability, api.Infinity)
				attr.realCapability = realCapability
			}
			cp.queueOpts[job.Queue] = attr
			klog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
		}

		attr := cp.queueOpts[job.Queue]
		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			// deduct the resources of scheduling gated tasks in a job when calculating inqueued resources
			// so that it will not block other jobs from being inqueued.
			attr.inqueue.Add(job.DeductSchGatedResources(job.GetMinResources()))
		}

		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueued := util.GetInqueueResource(job, job.Allocated)
			attr.inqueue.Add(job.DeductSchGatedResources(inqueued))
		}
		attr.elastic.Add(job.GetElasticResources())
		klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
			attr.name, attr.allocated.String(), attr.request.String(), attr.inqueue.String(), attr.elastic.String())
	}

	for _, attr := range cp.queueOpts {
		if attr.realCapability != nil {
			attr.deserved.MinDimensionResource(attr.realCapability, api.Infinity)
		}
		// When scalar resource not specified in deserved such as "pods", we should skip it and consider deserved resource as infinity.
		attr.deserved.MinDimensionResource(attr.request, api.Infinity)

		attr.deserved = helpers.Max(attr.deserved, attr.guarantee)
		cp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in capacity: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	// Record metrics
	for queueID, queueInfo := range ssn.Queues {
		queue := ssn.Queues[queueID]
		if attr, ok := cp.queueOpts[queueID]; ok {
			metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)
			metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory)
			continue
		}
		deservedCPU, deservedMem := 0.0, 0.0
		if queue.Queue.Spec.Deserved != nil {
			deservedCPU = float64(queue.Queue.Spec.Deserved.Cpu().MilliValue())
			deservedMem = float64(queue.Queue.Spec.Deserved.Memory().Value())
		}
		metrics.UpdateQueueDeserved(queueInfo.Name, deservedCPU, deservedMem)
		metrics.UpdateQueueAllocated(queueInfo.Name, 0, 0)
		metrics.UpdateQueueRequest(queueInfo.Name, 0, 0)
	}

	ssn.AddQueueOrderFn(cp.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if lv.Queue.Spec.Priority != rv.Queue.Spec.Priority {
			// return negative means high priority
			return int(rv.Queue.Spec.Priority) - int(lv.Queue.Spec.Priority)
		}

		if cp.queueOpts[lv.UID].share == cp.queueOpts[rv.UID].share {
			return 0
		}

		if cp.queueOpts[lv.UID].share < cp.queueOpts[rv.UID].share {
			return -1
		}

		return 1
	})
}

func (cp *capacityPlugin) buildHierarchicalQueueAttrs(ssn *framework.Session) bool {
	// Set the root queue
	cp.rootQueue = rootQueueID

	// Initialize queue attributes
	for _, queue := range ssn.Queues {
		_, found := cp.queueOpts[queue.UID]
		if found {
			continue
		}

		attr := cp.newQueueAttr(queue)
		cp.queueOpts[queue.UID] = attr
		err := cp.updateAncestors(queue, ssn)
		if err != nil {
			klog.Errorf("Failed to update Queue <%s> attributes, error: %v", queue.Name, err)
			return false
		}
	}

	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		attr := cp.queueOpts[job.Queue]
		if len(attr.children) > 0 {
			klog.Errorf("The Queue <%s> of Job <%s/%s> is not leaf queue", attr.name, job.Namespace, job.Name)
			return false
		}

		oldAllocated := attr.allocated.Clone()
		oldRequest := attr.request.Clone()
		oldInqueue := attr.inqueue.Clone()
		oldElastic := attr.elastic.Clone()

		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			attr.inqueue.Add(job.DeductSchGatedResources(job.GetMinResources()))
		}

		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueued := util.GetInqueueResource(job, job.Allocated)
			attr.inqueue.Add(job.DeductSchGatedResources(inqueued))
		}
		attr.elastic.Add(job.GetElasticResources())

		for _, ancestor := range attr.ancestors {
			ancestorAttr := cp.queueOpts[ancestor]
			ancestorAttr.allocated.Add(attr.allocated.Clone().Sub(oldAllocated))
			ancestorAttr.request.Add(attr.request.Clone().Sub(oldRequest))
			ancestorAttr.inqueue.Add(attr.inqueue.Clone().Sub(oldInqueue))
			ancestorAttr.elastic.Add(attr.elastic.Clone().Sub(oldElastic))
		}

		klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
			attr.name, attr.allocated.String(), attr.request.String(), attr.inqueue.String(), attr.elastic.String())
	}

	// init root queue realCapability/capability/deserved as cp.totalResource
	rootQueueAttr := cp.queueOpts[api.QueueID(cp.rootQueue)]
	rootQueueAttr.capability = cp.totalResource
	rootQueueAttr.realCapability = cp.totalResource
	rootQueueAttr.deserved = cp.totalResource
	// Check the hierarchical structure of queues
	err := cp.checkHierarchicalQueue(rootQueueAttr)
	if err != nil {
		klog.Errorf("Failed to check queue's hierarchical structure, error: %v", err)
		return false
	}
	klog.V(4).Infof("Successfully checked queue's hierarchical structure.")

	// update session attributes
	ssn.TotalGuarantee = cp.totalGuarantee

	// Update share
	for _, attr := range cp.queueOpts {
		cp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in capacity: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	// Record metrics
	for queueID := range ssn.Queues {
		attr := cp.queueOpts[queueID]
		metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory)
		metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)
		metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory)
	}

	ssn.AddQueueOrderFn(cp.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if lv.Queue.Spec.Priority != rv.Queue.Spec.Priority {
			// return negative means high priority
			return int(rv.Queue.Spec.Priority) - int(lv.Queue.Spec.Priority)
		}

		lvLeaf := cp.isLeafQueue(lv.UID)
		rvLeaf := cp.isLeafQueue(rv.UID)

		if lvLeaf && !rvLeaf {
			return -1
		} else if !lvLeaf && rvLeaf {
			return 1
		} else if !lvLeaf && !rvLeaf {
			if cp.queueOpts[lv.UID].share == cp.queueOpts[rv.UID].share {
				return 0
			}

			if cp.queueOpts[lv.UID].share < cp.queueOpts[rv.UID].share {
				return -1
			}
			return 1
		}

		lvAttr := cp.queueOpts[lv.UID]
		rvAttr := cp.queueOpts[rv.UID]
		level := getQueueLevel(lvAttr, rvAttr)
		lvParentID := lvAttr.queueID
		rvParentID := rvAttr.queueID
		if level+1 < len(lvAttr.ancestors) {
			lvParentID = lvAttr.ancestors[level+1]
		}
		if level+1 < len(rvAttr.ancestors) {
			rvParentID = rvAttr.ancestors[level+1]
		}

		if cp.queueOpts[lvParentID].share == cp.queueOpts[rvParentID].share {
			return 0
		}

		if cp.queueOpts[lvParentID].share < cp.queueOpts[rvParentID].share {
			return -1
		}

		return 1
	})

	ssn.AddVictimQueueOrderFn(cp.Name(), func(l, r, preemptor interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)
		pv := preemptor.(*api.QueueInfo)

		lLevel := getQueueLevel(cp.queueOpts[lv.UID], cp.queueOpts[pv.UID])
		rLevel := getQueueLevel(cp.queueOpts[rv.UID], cp.queueOpts[pv.UID])

		if lLevel == rLevel {
			return 0
		}

		if lLevel > rLevel {
			return -1
		}

		return 1
	})

	return true
}

func (cp *capacityPlugin) newQueueAttr(queue *api.QueueInfo) *queueAttr {
	attr := &queueAttr{
		queueID:   queue.UID,
		name:      queue.Name,
		ancestors: make([]api.QueueID, 0),
		children:  make(map[api.QueueID]*queueAttr),

		deserved:   api.NewResource(queue.Queue.Spec.Deserved),
		allocated:  api.EmptyResource(),
		request:    api.EmptyResource(),
		elastic:    api.EmptyResource(),
		inqueue:    api.EmptyResource(),
		guarantee:  api.EmptyResource(),
		capability: api.EmptyResource(),
	}
	if len(queue.Queue.Spec.Capability) != 0 {
		attr.capability = api.NewResource(queue.Queue.Spec.Capability)
	}

	if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
		attr.guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
	}

	return attr
}

func (cp *capacityPlugin) updateAncestors(queue *api.QueueInfo, ssn *framework.Session) error {
	if queue.Name == cp.rootQueue {
		return nil
	}

	parent := cp.rootQueue
	if queue.Queue.Spec.Parent != "" {
		parent = queue.Queue.Spec.Parent
	}
	if _, exist := ssn.Queues[api.QueueID(parent)]; !exist {
		return fmt.Errorf("the queue %s has invalid parent queue %s", queue.Name, parent)
	}

	parentInfo := ssn.Queues[api.QueueID(parent)]
	if _, found := cp.queueOpts[parentInfo.UID]; !found {
		parentAttr := cp.newQueueAttr(parentInfo)
		cp.queueOpts[parentAttr.queueID] = parentAttr
		err := cp.updateAncestors(parentInfo, ssn)
		if err != nil {
			return err
		}
	}

	cp.queueOpts[parentInfo.UID].children[queue.UID] = cp.queueOpts[queue.UID]
	cp.queueOpts[queue.UID].ancestors = append(cp.queueOpts[parentInfo.UID].ancestors, parentInfo.UID)
	return nil
}

func (cp *capacityPlugin) checkHierarchicalQueue(attr *queueAttr) error {
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()
	for _, childAttr := range attr.children {
		totalDeserved.Add(childAttr.deserved)
		totalGuarantee.Add(childAttr.guarantee)
		// if the user does not set CPU or memory in capability, we set the value to be the same as parent(we do not consider the situation where the user sets CPU or memory<=0)
		if childAttr.capability.MilliCPU <= 0 {
			childAttr.capability.MilliCPU = attr.capability.MilliCPU
		}
		if childAttr.capability.Memory <= 0 {
			childAttr.capability.Memory = attr.capability.Memory
		}
		// Check if the parent queue's capability is less than the child queue's capability
		if attr.capability.LessPartly(childAttr.capability, api.Zero) {
			return fmt.Errorf("queue <%s> capability is less than its child queue <%s>", attr.name, childAttr.name)
		}
	}

	if attr.name == cp.rootQueue {
		attr.guarantee = totalGuarantee
		cp.totalGuarantee = totalGuarantee
	}

	for _, childAttr := range attr.children {
		realCapability := attr.realCapability.Clone().Sub(totalGuarantee).Add(childAttr.guarantee)
		if childAttr.capability == nil {
			childAttr.realCapability = realCapability
		} else {
			realCapability.MinDimensionResource(childAttr.capability, api.Infinity)
			childAttr.realCapability = realCapability
		}
		oldDeserved := childAttr.deserved.Clone()
		childAttr.deserved.MinDimensionResource(childAttr.realCapability, api.Infinity)
		childAttr.deserved.MinDimensionResource(childAttr.request, api.Zero)

		childAttr.deserved = helpers.Max(childAttr.deserved, childAttr.guarantee)
		totalDeserved.Sub(oldDeserved).Add(childAttr.deserved)
	}

	// Check if the parent queue's deserved resources are less than the total deserved resources of child queues
	if attr.deserved.LessPartly(totalDeserved, api.Zero) {
		return fmt.Errorf("deserved resources of queue <%s> are less than the sum of its child queues' deserved resources", attr.name)
	}

	// Check if the parent queue's guarantee resources are less than the total guarantee resources of child queues
	if attr.guarantee.LessPartly(totalGuarantee, api.Zero) {
		return fmt.Errorf("guarantee resources of queue <%s> are less than the sum of its child queues' guarantee resources", attr.name)
	}

	for _, childAttr := range attr.children {
		err := cp.checkHierarchicalQueue(childAttr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (cp *capacityPlugin) updateShare(attr *queueAttr) {
	res := float64(0)

	for _, rn := range attr.deserved.ResourceNames() {
		res = max(res, helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn)))
	}

	attr.share = res
	metrics.UpdateQueueShare(attr.name, attr.share)
}

func (cp *capacityPlugin) isLeafQueue(queueID api.QueueID) bool {
	return len(cp.queueOpts[queueID].children) == 0
}

func (cp *capacityPlugin) queueAllocatable(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	attr := cp.queueOpts[queue.UID]
	futureUsed := attr.allocated.Clone().Add(candidate.Resreq)
	allocatable := futureUsed.LessEqualWithDimension(attr.realCapability, candidate.Resreq)
	if !allocatable {
		klog.V(3).Infof("Queue <%v>: realCapability <%v>, allocated <%v>; Candidate <%v>: resource request <%v>",
			queue.Name, attr.realCapability, attr.allocated, candidate.Name, candidate.Resreq)
	}

	return allocatable
}

func (cp *capacityPlugin) checkQueueAllocatableHierarchically(ssn *framework.Session, queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	// If hierarchical queue is not enabled, list will only contain the queue itself.
	list := append(cp.queueOpts[queue.UID].ancestors, queue.UID)
	// Check whether the candidate task can be allocated to the queue and all its ancestors.
	for i := len(list) - 1; i >= 0; i-- {
		if !cp.queueAllocatable(ssn.Queues[list[i]], candidate) {
			// If log level is 5, print the information of all queues from leaf to ancestor.
			if klog.V(5).Enabled() {
				for j := i - 1; j >= 0; j-- {
					cp.queueAllocatable(ssn.Queues[list[j]], candidate)
				}
			}
			return false
		}
	}
	return true
}

func (cp *capacityPlugin) jobEnqueueable(queue *api.QueueInfo, job *api.JobInfo) bool {
	attr := cp.queueOpts[queue.UID]
	minReq := job.GetMinResources()

	klog.V(5).Infof("job %s min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
		job.Name, minReq.String(), queue.Name, attr.realCapability.String(), attr.allocated.String(), attr.inqueue.String(), attr.elastic.String())
	// The queue resource quota limit has not reached
	r := minReq.Clone().Add(attr.allocated).Add(attr.inqueue).Sub(attr.elastic)

	return r.LessEqualWithDimension(attr.realCapability, minReq)
}

func (cp *capacityPlugin) checkJobEnqueueableHierarchically(ssn *framework.Session, queue *api.QueueInfo, job *api.JobInfo) bool {
	// If hierarchical queue is not enabled, list will only contain the queue itself.
	list := append(cp.queueOpts[queue.UID].ancestors, queue.UID)
	// Check whether the job can be enqueued to the queue and all its ancestors.
	for i := len(list) - 1; i >= 0; i-- {
		if !cp.jobEnqueueable(ssn.Queues[list[i]], job) {
			// If log level is 5, print the information of all queues from leaf to ancestor.
			if klog.V(5).Enabled() {
				for j := i - 1; j >= 0; j-- {
					cp.jobEnqueueable(ssn.Queues[list[j]], job)
				}
			}
			ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "queue resource quota insufficient")
			return false
		}
	}

	return true
}

func getQueueLevel(l *queueAttr, r *queueAttr) int {
	level := 0

	for i := 0; i < min(len(l.ancestors), len(r.ancestors)); i++ {
		if l.ancestors[i] == r.ancestors[i] {
			level = i
		} else {
			return level
		}
	}

	return level
}
