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
	"context"
	"fmt"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	PluginName = "capacity"

	// preFilterStateKey is the key in CycleState to InterPodAffinity pre-computed data for Filtering.
	// Using the name of the plugin will likely help us avoid collisions with other plugins.
	capacityStateKey = PluginName
	rootQueueID      = "root"
)

type capacityPlugin struct {
	rootQueue      string
	totalResource  *api.Resource
	totalGuarantee *api.Resource
	totalDeserved  *api.Resource

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

func (cp *capacityPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	cp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", cp.totalResource)

	hierarchyEnabled := ssn.HierarchyEnabled(cp.Name())
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
		if queue.Queue.Status.State != scheduling.QueueStateOpen {
			klog.V(3).Infof("Queue <%s> current state: %s, is not open state, can not reclaim for <%s>.", queue.Name, queue.Queue.Status.State, task.Name)
			return false
		}
		attr := cp.queueOpts[queue.UID]

		futureUsed := attr.allocated.Clone().Add(task.Resreq)
		allocatable, _ := futureUsed.LessEqualWithDimensionAndResourcesName(attr.deserved, task.Resreq)
		overused := !allocatable
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
		if queue.Queue.Status.State != scheduling.QueueStateOpen {
			klog.V(3).Infof("Queue <%s> current state: %s, cannot allocate task <%s>.", queue.Name, queue.Queue.Status.State, candidate.Name)
			return false
		}
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
		// If the queue is not open, do not enqueue
		if queue.Queue.Status.State != scheduling.QueueStateOpen {
			klog.V(3).Infof("Queue <%s> current state: %s, is not open state, reject job <%s/%s>.", queue.Name, queue.Queue.Status.State, job.Namespace, job.Name)
			return util.Reject
		}
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

	ssn.AddPrePredicateFn(cp.Name(), func(task *api.TaskInfo) error {
		state := &capacityState{
			queueAttrs: make(map[api.QueueID]*queueAttr),
		}

		for _, queue := range cp.queueOpts {
			state.queueAttrs[queue.queueID] = queue.Clone()
		}

		ssn.GetCycleState(task.UID).Write(capacityStateKey, state)
		return nil
	})

	ssn.AddSimulateAddTaskFn(cp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, taskToSchedule *api.TaskInfo, taskToAdd *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		state, err := getCapacityState(cycleState)
		if err != nil {
			return fmt.Errorf("failed to get capacity state: %w", err)
		}

		job := ssn.Jobs[taskToAdd.Job]
		attr := state.queueAttrs[job.Queue]
		if attr == nil {
			return fmt.Errorf("queue %s not found", job.Queue)
		}
		attr.allocated.Add(taskToAdd.Resreq)
		updateQueueAttrShare(attr)
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := state.queueAttrs[ancestorID]
				ancestorAttr.allocated.Add(taskToAdd.Resreq)
			}
		}
		return nil
	})

	ssn.AddSimulateRemoveTaskFn(cp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, taskToSchedule *api.TaskInfo, taskToRemove *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		state, err := getCapacityState(cycleState)
		if err != nil {
			return fmt.Errorf("failed to get capacity state: %w", err)
		}
		job := ssn.Jobs[taskToRemove.Job]
		attr := state.queueAttrs[job.Queue]
		if attr == nil {
			return fmt.Errorf("queue %s not found", job.Queue)
		}
		attr.allocated.Sub(taskToRemove.Resreq)
		updateQueueAttrShare(attr)
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := state.queueAttrs[ancestorID]
				ancestorAttr.allocated.Sub(taskToRemove.Resreq)
			}
		}
		return nil
	})

	ssn.AddSimulateAllocatableFn(cp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		state, err := getCapacityState(cycleState)
		if err != nil {
			return false
		}

		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return false
		}
		if hierarchyEnabled && !cp.isLeafQueue(queue.UID) {
			klog.V(3).Infof("Queue <%s> is not a leaf queue, can not allocate task <%s>.", queue.Name, candidate.Name)
			return false
		}

		simulateQueueAllocatable := func(state *capacityState, queue *api.QueueInfo, candidate *api.TaskInfo) bool {
			attr := state.queueAttrs[queue.UID]
			return queueAllocatable(attr, candidate, queue)
		}

		list := append(state.queueAttrs[queue.UID].ancestors, queue.UID)
		for i := len(list) - 1; i >= 0; i-- {
			if !simulateQueueAllocatable(state, ssn.Queues[list[i]], candidate) {
				if klog.V(5).Enabled() {
					for i--; i >= 0; i-- {
						simulateQueueAllocatable(state, ssn.Queues[list[i]], candidate)
					}
				}
				return false
			}
		}
		return true
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := cp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)

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
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)

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
				attr.capability = api.EmptyResource()
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

		attr.deserved = helpers.Max(attr.deserved, attr.guarantee)
		cp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in capacity: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	// Record metrics
	for queueID, queueInfo := range ssn.Queues {
		queue := ssn.Queues[queueID]
		if attr, ok := cp.queueOpts[queueID]; ok {
			metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory, attr.deserved.ScalarResources)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)
			metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory, attr.request.ScalarResources)
			if attr.capability != nil {
				metrics.UpdateQueueCapacity(attr.name, attr.capability.MilliCPU, attr.capability.Memory, attr.capability.ScalarResources)
			}
			metrics.UpdateQueueRealCapacity(attr.name, attr.realCapability.MilliCPU, attr.realCapability.Memory, attr.realCapability.ScalarResources)
			continue
		}
		deservedCPU, deservedMem, scalarResources := 0.0, 0.0, map[v1.ResourceName]float64{}
		if queue.Queue.Spec.Deserved != nil {
			attr := api.NewResource(queue.Queue.Spec.Deserved)
			deservedCPU = attr.MilliCPU
			deservedMem = attr.Memory
			scalarResources = attr.ScalarResources
		}
		metrics.UpdateQueueDeserved(queueInfo.Name, deservedCPU, deservedMem, scalarResources)
		metrics.UpdateQueueAllocated(queueInfo.Name, 0, 0, map[v1.ResourceName]float64{})
		metrics.UpdateQueueRequest(queueInfo.Name, 0, 0, map[v1.ResourceName]float64{})
		guarantee := api.EmptyResource()
		if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
			guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
		}
		realCapacity := api.ExceededPart(cp.totalResource, cp.totalGuarantee).Add(guarantee)
		if len(queue.Queue.Spec.Capability) > 0 {
			capacity := api.NewResource(queue.Queue.Spec.Capability)
			realCapacity.MinDimensionResource(capacity, api.Infinity)
			metrics.UpdateQueueCapacity(queueInfo.Name, capacity.MilliCPU, capacity.Memory, capacity.ScalarResources)
		}
		metrics.UpdateQueueRealCapacity(queueInfo.Name, realCapacity.MilliCPU, realCapacity.Memory, realCapacity.ScalarResources)
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

	// init root queue: realCapability is set to total resource, and capability/deserved are also set if empty.
	rootQueueAttr := cp.queueOpts[api.QueueID(cp.rootQueue)]
	if rootQueueAttr.capability.IsEmpty() {
		rootQueueAttr.capability = cp.totalResource
	}
	if rootQueueAttr.deserved.IsEmpty() {
		rootQueueAttr.deserved = cp.totalResource
	}
	rootQueueAttr.realCapability = cp.totalResource
	// Check the hierarchical structure of queues
	err := cp.checkHierarchicalQueue(rootQueueAttr)
	if err != nil {
		klog.Errorf("Failed to check queue's hierarchical structure, error: %v", err)
		return false
	}
	klog.V(4).Infof("Successfully checked queue's hierarchical structure.")

	// update session attributes
	ssn.TotalGuarantee = cp.totalGuarantee
	ssn.TotalDeserved = cp.totalDeserved

	// Update share
	for _, attr := range cp.queueOpts {
		cp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in capacity: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	// Record metrics
	for queueID := range ssn.Queues {
		attr := cp.queueOpts[queueID]
		metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory, attr.deserved.ScalarResources)
		metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)
		metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory, attr.request.ScalarResources)
		metrics.UpdateQueueCapacity(attr.name, attr.capability.MilliCPU, attr.capability.Memory, attr.capability.ScalarResources)
		metrics.UpdateQueueRealCapacity(attr.name, attr.realCapability.MilliCPU, attr.realCapability.Memory, attr.realCapability.ScalarResources)
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

		deserved:       api.NewResource(queue.Queue.Spec.Deserved),
		allocated:      api.EmptyResource(),
		request:        api.EmptyResource(),
		elastic:        api.EmptyResource(),
		inqueue:        api.EmptyResource(),
		guarantee:      api.EmptyResource(),
		capability:     api.EmptyResource(),
		realCapability: api.EmptyResource(),
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

		// Inherit scalar resources from parent if child's scalar resources is nil or some fields are not set
		if attr.capability.ScalarResources != nil {
			if childAttr.capability.ScalarResources == nil {
				childAttr.capability.ScalarResources = make(map[v1.ResourceName]float64)
			}
			for k, v := range attr.capability.ScalarResources {
				if _, exists := childAttr.capability.ScalarResources[k]; !exists {
					childAttr.capability.ScalarResources[k] = v
				}
			}
		}

		// Check if the parent queue's capability is less than the child queue's capability
		if attr.capability.LessPartly(childAttr.capability, api.Zero) {
			return fmt.Errorf("queue <%s> capability <%s> is less than its child queue <%s> capability <%s>",
				attr.name, attr.capability, childAttr.name, childAttr.capability)
		}
	}

	if attr.name == cp.rootQueue {
		if attr.guarantee.IsEmpty() {
			attr.guarantee = totalGuarantee
		}
		if attr.deserved.IsEmpty() {
			attr.deserved = totalDeserved
		}
		cp.totalGuarantee = attr.guarantee
		cp.totalDeserved = attr.deserved
	}

	for _, childAttr := range attr.children {
		realCapability := api.ExceededPart(attr.realCapability, totalGuarantee).Add(childAttr.guarantee)
		if childAttr.capability == nil {
			childAttr.capability = api.EmptyResource()
			childAttr.realCapability = realCapability
		} else {
			realCapability.MinDimensionResource(childAttr.capability, api.Infinity)
			childAttr.realCapability = realCapability
		}
	}

	// Check if the parent queue's deserved resources are less than the total deserved resources of child queues
	if attr.deserved.LessPartly(totalDeserved, api.Zero) {
		return fmt.Errorf("queue <%s> deserved resources <%s> are less than the sum of its child queues' deserved resources <%s>",
			attr.name, attr.deserved, totalDeserved)
	}

	// Check if the parent queue's guarantee resources are less than the total guarantee resources of child queues
	if attr.guarantee.LessPartly(totalGuarantee, api.Zero) {
		return fmt.Errorf("queue <%s> guarantee resources <%s> are less than the sum of its child queues' guarantee resources <%s>",
			attr.name, attr.guarantee, totalGuarantee)
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
	updateQueueAttrShare(attr)
	metrics.UpdateQueueShare(attr.name, attr.share)
}

func (cp *capacityPlugin) isLeafQueue(queueID api.QueueID) bool {
	return len(cp.queueOpts[queueID].children) == 0
}

func (cp *capacityPlugin) queueAllocatable(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	attr := cp.queueOpts[queue.UID]
	return queueAllocatable(attr, candidate, queue)
}

func queueAllocatable(attr *queueAttr, candidate *api.TaskInfo, queue *api.QueueInfo) bool {
	futureUsed := attr.allocated.Clone().Add(candidate.Resreq)
	allocatable, _ := futureUsed.LessEqualWithDimensionAndResourcesName(attr.realCapability, candidate.Resreq)
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

func (cp *capacityPlugin) jobEnqueueable(queue *api.QueueInfo, job *api.JobInfo) (bool, []string) {
	attr := cp.queueOpts[queue.UID]
	minReq := job.GetMinResources()

	klog.V(5).Infof("job %s min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
		job.Name, minReq.String(), queue.Name, attr.realCapability.String(), attr.allocated.String(), attr.inqueue.String(), attr.elastic.String())
	// The queue resource quota limit has not reached
	r := minReq.Clone().Add(attr.allocated).Add(attr.inqueue).Sub(attr.elastic)

	return r.LessEqualWithDimensionAndResourcesName(attr.realCapability, minReq)
}

func (cp *capacityPlugin) checkJobEnqueueableHierarchically(ssn *framework.Session, queue *api.QueueInfo, job *api.JobInfo) bool {
	// If hierarchical queue is not enabled, list will only contain the queue itself.
	list := append(cp.queueOpts[queue.UID].ancestors, queue.UID)
	// Check whether the job can be enqueued to the queue and all its ancestors.
	for i := len(list) - 1; i >= 0; i-- {
		if inqueue, resourceNames := cp.jobEnqueueable(ssn.Queues[list[i]], job); !inqueue {
			// If log level is 5, print the information of all queues from leaf to ancestor.
			if klog.V(5).Enabled() {
				for j := i - 1; j >= 0; j-- {
					cp.jobEnqueueable(ssn.Queues[list[j]], job)
				}
			}

			ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), util.FormatResourceNames("queue resource quota insufficient", "insufficient", resourceNames))
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

func getCapacityState(cycleState *k8sframework.CycleState) (*capacityState, error) {
	c, err := cycleState.Read(capacityStateKey)
	if err != nil {
		// preFilterState doesn't exist, likely PreFilter wasn't invoked.
		return nil, fmt.Errorf("error reading %q from cycleState: %w", capacityStateKey, err)
	}

	s, ok := c.(*capacityState)
	if !ok {
		return nil, fmt.Errorf("%+v  convert to capacity.state error", c)
	}
	return s, nil
}

type capacityState struct {
	queueAttrs map[api.QueueID]*queueAttr
}

func (qa *queueAttr) Clone() *queueAttr {
	if qa == nil {
		return nil
	}

	cloned := &queueAttr{
		queueID:        qa.queueID,
		name:           qa.name,
		share:          qa.share,
		deserved:       qa.deserved.Clone(),
		allocated:      qa.allocated.Clone(),
		request:        qa.request.Clone(),
		elastic:        qa.elastic.Clone(),
		inqueue:        qa.inqueue.Clone(),
		capability:     qa.capability.Clone(),
		realCapability: qa.realCapability.Clone(),
		guarantee:      qa.guarantee.Clone(),
		children:       make(map[api.QueueID]*queueAttr),
	}

	if len(qa.ancestors) > 0 {
		cloned.ancestors = make([]api.QueueID, len(qa.ancestors))
		copy(cloned.ancestors, qa.ancestors)
	}

	for childID, childNode := range qa.children {
		cloned.children[childID] = childNode.Clone()
	}

	return cloned
}

func (s *capacityState) Clone() k8sframework.StateData {
	if s == nil {
		return nil
	}

	newState := &capacityState{
		queueAttrs: make(map[api.QueueID]*queueAttr, len(s.queueAttrs)),
	}

	for qID, qa := range s.queueAttrs {
		newState.queueAttrs[qID] = qa.Clone()
	}

	return newState
}

func updateQueueAttrShare(attr *queueAttr) {
	res := float64(0)

	for _, rn := range attr.deserved.ResourceNames() {
		res = max(res, helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn)))
	}

	attr.share = res
}
