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
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	PluginName              = "capacity"
	ancestorReclaimLevelKey = "ancestorReclaimLevel"

	// preFilterStateKey is the key in CycleState to InterPodAffinity pre-computed data for Filtering.
	// Using the name of the plugin will likely help us avoid collisions with other plugins.
	capacityStateKey = PluginName
	rootQueueID      = "root"

	// DynamicResourceAllocationEnable is the key for enabling DRA quota enforcement in the capacity plugin
	DynamicResourceAllocationEnable = "capacity.DynamicResourceAllocationEnable"

	// DRAConsumableCapacityEnable is the key for enabling DRA consumable capacity quota in the capacity plugin
	DRAConsumableCapacityEnable = "capacity.DRAConsumableCapacityEnable"

	DeviceClassCountPrefix = "deviceclass/"
	DeviceClassCapacitySep = ".deviceclass/"
)

type capacityPlugin struct {
	rootQueue            string
	totalResource        *api.Resource
	totalGuarantee       *api.Resource
	ancestorReclaimLevel int

	queueOpts map[api.QueueID]*queueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	// queueGateReservedTasks tracks tasks that passed capacity checks but cannot be scheduled
	// These tasks reserve queue capacity to prevent other tasks from consuming it
	// Rebuilt fresh at the start of each scheduling cycle in OnSessionOpen
	queueGateReservedTasks map[api.QueueID]map[api.TaskID]*api.TaskInfo
	// reservedTaskQueue is a reverse index from a reserved task to its (leaf) queue.
	// It gives O(1) membership tests for candidate exclusion and turns
	// removeTaskFromReservedCache from an O(queues) scan into an O(1) lookup.
	// Rebuilt together with queueGateReservedTasks in buildQueueReservedTasksCache.
	reservedTaskQueue map[api.TaskID]api.QueueID

	// dynamicResourceAllocationEnable controls whether DRA quota is enforced
	dynamicResourceAllocationEnable bool
	// draConsumableCapacityEnable controls whether Capacity dimensions inside DRA are enforced
	draConsumableCapacityEnable bool
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
	realCapability    *api.Resource
	guarantee         *api.Resource
	dra               *draQuotaAttr
	resourceClaimRefs map[string]int
	// reservedSubtree is the aggregate Resreq of gate-reserved (admitted but not yet
	// allocated) tasks in this queue AND every descendant queue. It is maintained
	// incrementally up the ancestor chain, mirroring how `allocated` is propagated, so
	// hierarchical capacity checks can read it in O(1) instead of walking the subtree.
	reservedSubtree *api.Resource
}

// draQuotaAttr holds DRA quota tracking state for a queue
type draQuotaAttr struct {
	// capability is the hard limit per DeviceClass
	capability map[string]*api.DRAResource
	// deserved is the soft limit per DeviceClass
	deserved map[string]*api.DRAResource
	// guarantee is the guaranteed minimum per DeviceClass
	guarantee map[string]*api.DRAResource
	// allocated tracks current allocation per DeviceClass
	allocated map[string]*api.DRAResource
	// inqueue tracks DRA resources admitted to the queue in the current scheduling session.
	inqueue map[string]*api.DRAResource
}

func (da *draQuotaAttr) Clone() *draQuotaAttr {
	if da == nil {
		return nil
	}
	out := &draQuotaAttr{
		capability: cloneDRAResourceMap(da.capability),
		deserved:   cloneDRAResourceMap(da.deserved),
		guarantee:  cloneDRAResourceMap(da.guarantee),
		allocated:  cloneDRAResourceMap(da.allocated),
		inqueue:    cloneDRAResourceMap(da.inqueue),
	}
	return out
}

func cloneDRAResourceMap(m map[string]*api.DRAResource) map[string]*api.DRAResource {
	if m == nil {
		return nil
	}
	out := make(map[string]*api.DRAResource, len(m))
	for k, v := range m {
		out[k] = v.Clone()
	}
	return out
}

func parseDRAResourceList(resources v1.ResourceList) map[string]*api.DRAResource {
	if len(resources) == 0 {
		return nil
	}
	out := make(map[string]*api.DRAResource)
	for name, quantity := range resources {
		resourceName := string(name)
		if idx := strings.Index(resourceName, DeviceClassCapacitySep); idx > 0 {
			dim := resourceName[:idx]
			deviceClass := resourceName[idx+len(DeviceClassCapacitySep):]
			if dim == "" || deviceClass == "" {
				continue
			}
			if out[deviceClass] == nil {
				out[deviceClass] = &api.DRAResource{Capacity: make(map[string]resource.Quantity)}
			}
			if out[deviceClass].Capacity == nil {
				out[deviceClass].Capacity = make(map[string]resource.Quantity)
			}
			out[deviceClass].Capacity[dim] = quantity.DeepCopy()
			continue
		}
		if strings.HasPrefix(resourceName, DeviceClassCountPrefix) {
			deviceClass := strings.TrimPrefix(resourceName, DeviceClassCountPrefix)
			if deviceClass == "" {
				continue
			}
			if out[deviceClass] == nil {
				out[deviceClass] = &api.DRAResource{}
			}
			out[deviceClass].Count = quantity.Value()
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func newDRAQuotaAttr(capability, deserved, guarantee v1.ResourceList) *draQuotaAttr {
	da := &draQuotaAttr{
		capability: parseDRAResourceList(capability),
		deserved:   parseDRAResourceList(deserved),
		guarantee:  parseDRAResourceList(guarantee),
		allocated:  make(map[string]*api.DRAResource),
		inqueue:    make(map[string]*api.DRAResource),
	}
	if da.capability == nil && da.deserved == nil && da.guarantee == nil {
		return nil
	}
	return da
}

func getDRADelta(newAttr, oldAttr *draQuotaAttr) map[string]*api.DRAResource {
	if newAttr == nil {
		return nil
	}
	delta := make(map[string]*api.DRAResource)

	// Create union of keys
	keys := make(map[string]struct{})
	if newAttr.allocated != nil {
		for k := range newAttr.allocated {
			keys[k] = struct{}{}
		}
	}
	if oldAttr != nil && oldAttr.allocated != nil {
		for k := range oldAttr.allocated {
			keys[k] = struct{}{}
		}
	}

	for k := range keys {
		var newRes, oldRes *api.DRAResource
		if newAttr.allocated != nil {
			newRes = newAttr.allocated[k]
		}
		if oldAttr != nil && oldAttr.allocated != nil {
			oldRes = oldAttr.allocated[k]
		}

		res := &api.DRAResource{Capacity: make(map[string]resource.Quantity)}
		if newRes != nil {
			res.Count = newRes.Count
			for dim, val := range newRes.Capacity {
				res.Capacity[dim] = val.DeepCopy()
			}
		}
		if oldRes != nil {
			res.Count -= oldRes.Count
			for dim, val := range oldRes.Capacity {
				q := res.Capacity[dim]
				q.Sub(val)
				res.Capacity[dim] = q
			}
		}
		delta[k] = res
	}
	return delta
}

// checkDRAAllocatable checks if DRA requests fit within queue's DRA capability.
// consumableCapacityEnabled controls whether the Capacity dimensions are also checked.
// includeInqueue is true for enqueue checks, where already admitted jobs reserve queue quota.
func checkDRAAllocatable(dra *draQuotaAttr, taskDRA map[string]*api.DRAResource, consumableCapacityEnabled bool, includeInqueue bool) bool {
	if dra == nil || taskDRA == nil {
		return true // no DRA quota configured or no DRA requests
	}

	for deviceClass, request := range taskDRA {
		capability, exists := dra.capability[deviceClass]
		if !exists {
			// No capability configured for this DeviceClass → no quota limit, allow
			klog.V(5).Infof("checkDRAAllocatable: No capability for %s, passing through", deviceClass)
			continue
		}

		allocated := dra.allocated[deviceClass]
		allocatedCount := int64(0)
		if allocated != nil {
			allocatedCount = allocated.Count
		}
		var inqueue *api.DRAResource
		if includeInqueue {
			inqueue = dra.inqueue[deviceClass]
		}
		inqueueCount := int64(0)
		if inqueue != nil {
			inqueueCount = inqueue.Count
		}

		klog.V(5).Infof("checkDRAAllocatable: deviceClass=%s, allocated=%d, inqueue=%d, request=%d, capability=%d",
			deviceClass, allocatedCount, inqueueCount, request.Count, capability.Count)

		if capability.Count > 0 && allocatedCount+inqueueCount+request.Count > capability.Count {
			klog.V(3).Infof("checkDRAAllocatable: count exceeded for %s: allocated=%d, inqueue=%d, requested=%d, capability=%d",
				deviceClass, allocatedCount, inqueueCount, request.Count, capability.Count)
			return false
		}

		// Check capacity dimensions (only when DRAConsumableCapacity feature is enabled)
		if consumableCapacityEnabled {
			for dim, reqQty := range request.Capacity {
				capQty, ok := capability.Capacity[dim]
				if !ok {
					return false // dimension not in capability
				}
				var allocQty resource.Quantity
				if allocated != nil {
					allocQty = allocated.Capacity[dim]
				}
				futureUsed := allocQty.DeepCopy()
				if inqueue != nil {
					futureUsed.Add(inqueue.Capacity[dim])
				}
				futureUsed.Add(reqQty)
				if futureUsed.Cmp(capQty) > 0 {
					return false
				}
			}
		}
	}
	return true
}

func mergeTaskDRA(dst map[string]*api.DRAResource, src map[string]*api.DRAResource) {
	for deviceClass, request := range src {
		if request == nil {
			continue
		}
		if dst[deviceClass] == nil {
			dst[deviceClass] = &api.DRAResource{
				Capacity: make(map[string]resource.Quantity),
			}
		}
		dst[deviceClass].Add(request)
	}
}

func incrementalTaskDRA(attr *queueAttr, task *api.TaskInfo) map[string]*api.DRAResource {
	if task == nil {
		return nil
	}
	if len(task.ResourceClaimDRAResreq) == 0 {
		return task.DRAResreq
	}

	incremental := make(map[string]*api.DRAResource)
	for _, claimKey := range task.ResourceClaimKeys {
		if attr != nil && attr.resourceClaimRefs[claimKey] > 0 {
			continue
		}
		mergeTaskDRA(incremental, task.ResourceClaimDRAResreq[claimKey])
	}
	if len(incremental) == 0 {
		return nil
	}
	return incremental
}

// updateDRAAllocated adds task's DRA requests to allocated tracking
func updateDRAAllocated(dra *draQuotaAttr, taskDRA map[string]*api.DRAResource) {
	mergeTaskDRA(dra.allocated, taskDRA)
}

func updateDRAInqueue(dra *draQuotaAttr, jobDRA map[string]*api.DRAResource) {
	mergeTaskDRA(dra.inqueue, jobDRA)
}

func addTaskDRAAllocated(attr *queueAttr, task *api.TaskInfo) {
	if attr == nil || attr.dra == nil || task == nil {
		return
	}
	if len(task.ResourceClaimDRAResreq) == 0 {
		if task.DRAResreq != nil {
			updateDRAAllocated(attr.dra, task.DRAResreq)
		}
		return
	}
	if attr.resourceClaimRefs == nil {
		attr.resourceClaimRefs = make(map[string]int)
	}
	for _, claimKey := range task.ResourceClaimKeys {
		claimReq := task.ResourceClaimDRAResreq[claimKey]
		if len(claimReq) == 0 {
			continue
		}
		if attr.resourceClaimRefs[claimKey] == 0 {
			updateDRAAllocated(attr.dra, claimReq)
		}
		attr.resourceClaimRefs[claimKey]++
	}
}

// subtractDRAAllocated removes task's DRA requests from allocated tracking
func subtractDRAAllocated(dra *draQuotaAttr, taskDRA map[string]*api.DRAResource) {
	for deviceClass, request := range taskDRA {
		allocated := dra.allocated[deviceClass]
		if allocated == nil {
			continue
		}
		allocated.Sub(request)
	}
}

func removeTaskDRAAllocated(attr *queueAttr, task *api.TaskInfo) {
	if attr == nil || attr.dra == nil || task == nil {
		return
	}
	if len(task.ResourceClaimDRAResreq) == 0 {
		if task.DRAResreq != nil {
			subtractDRAAllocated(attr.dra, task.DRAResreq)
		}
		return
	}
	for _, claimKey := range task.ResourceClaimKeys {
		claimReq := task.ResourceClaimDRAResreq[claimKey]
		if len(claimReq) == 0 {
			continue
		}
		refCount := attr.resourceClaimRefs[claimKey]
		if refCount <= 1 {
			subtractDRAAllocated(attr.dra, claimReq)
			delete(attr.resourceClaimRefs, claimKey)
			continue
		}
		attr.resourceClaimRefs[claimKey] = refCount - 1
	}
}

// New return capacityPlugin action
func New(arguments framework.Arguments) framework.Plugin {
	// Default to k8s feature gate values, allow override via plugin arguments
	dynamicResourceAllocationEnable := utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DynamicResourceAllocation)
	draConsumableCapacityEnable := utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRAConsumableCapacity)

	arguments.GetBool(&dynamicResourceAllocationEnable, DynamicResourceAllocationEnable)
	arguments.GetBool(&draConsumableCapacityEnable, DRAConsumableCapacityEnable)

	return &capacityPlugin{
		totalResource:                   api.EmptyResource(),
		totalGuarantee:                  api.EmptyResource(),
		queueOpts:                       map[api.QueueID]*queueAttr{},
		ancestorReclaimLevel:            0,
		pluginArguments:                 arguments,
		queueGateReservedTasks:          make(map[api.QueueID]map[api.TaskID]*api.TaskInfo),
		reservedTaskQueue:               make(map[api.TaskID]api.QueueID),
		dynamicResourceAllocationEnable: dynamicResourceAllocationEnable,
		draConsumableCapacityEnable:     draConsumableCapacityEnable,
	}
}

func (cp *capacityPlugin) Name() string {
	return PluginName
}

func (cp *capacityPlugin) OnSessionOpen(ssn *framework.Session) {
	cp.parseArguments()

	// Prepare scheduling data for this session.
	cp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", cp.totalResource)

	// Rebuild reserved cache for this scheduling cycle
	if utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) {
		cp.buildQueueReservedTasksCache(ssn)
	}

	hierarchyEnabled := ssn.HierarchyEnabled(cp.Name())
	readyToSchedule := true
	if hierarchyEnabled {
		readyToSchedule = cp.buildHierarchicalQueueAttrs(ssn)
		klog.V(4).Infof("Hierarchy is enabled in capacity plugin")
	} else {
		cp.buildQueueAttrs(ssn)
	}

	// Seed each queue's reservedSubtree aggregate from the reserved cache. This must run
	// after the queue attrs (and their ancestor/children links) are built so reservations
	// recorded under a leaf queue propagate to every ancestor's subtree total.
	if utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) {
		cp.aggregateReservedSubtree()
	}

	ssn.AddReclaimableFn(cp.Name(), func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return victims, util.Reject
		}

		reclaimerJob := ssn.Jobs[reclaimer.Job]
		if reclaimerJob == nil {
			klog.Warningf("[capacity] Skip reclaim for reclaimer <%s/%s>: job <%s> not found in session",
				reclaimer.Namespace, reclaimer.Name, reclaimer.Job)
			return victims, util.Reject
		}
		reclaimerAttr := cp.queueOpts[reclaimerJob.Queue]
		if reclaimerAttr == nil {
			klog.Warningf("[capacity] Skip reclaim for reclaimer <%s/%s>: queue <%s> not found in queueOpts",
				reclaimer.Namespace, reclaimer.Name, reclaimerJob.Queue)
			return victims, util.Reject
		}

		reclaimeesQueue := ssn.BuildVictimsPriorityQueue(reclaimees, reclaimer)
		for !reclaimeesQueue.Empty() {
			reclaimee := reclaimeesQueue.Pop().(*api.TaskInfo)
			job := ssn.Jobs[reclaimee.Job]
			if job == nil {
				klog.Warningf("[capacity] Skip reclaimee <%s/%s>: job <%s> not found in session (orphaned task from deleted PodGroup)",
					reclaimee.Namespace, reclaimee.Name, reclaimee.Job)
				continue
			}

			attr := cp.queueOpts[job.Queue]
			if attr == nil {
				klog.Warningf("[capacity] Skip reclaimee <%s/%s>: queue <%s> not found in queueOpts",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			klog.V(5).Infof("[capacity] Considering reclaimee <%s/%s> from queue <%s> for reclaimer <%s/%s>.",
				reclaimee.Namespace, reclaimee.Name, attr.queueID, reclaimer.Namespace, reclaimer.Name)

			// If reclaimee doesn't have intersecting resource dimensions with reclaimer we can skip it.
			if skip, reason := cp.shouldSkipReclaimee(reclaimee, reclaimer); skip {
				klog.V(5).Infof("%s, skip it.", reason)
				continue
			}

			// allocations maps each queue to its current allocated resources (cloned) and 'allocated' points to this resource object.
			// As victims (reclaimees) are selected, their resource requests are subtracted from the corresponding queue's allocation via this pointer.
			// This ensures that subsequent victim selection for the same queue uses the updated allocation state.
			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			ancestorAllocations := make(map[api.QueueID]*api.Resource)

			// Check guarantee
			if satisfies, _ := cp.checkGuaranteeConstraint(allocated, reclaimee, attr.guarantee); !satisfies {
				continue
			}

			// A reclaimee is eligible as a victim if it is an immediate victim or
			// if its queue's allocated resources exceed deserved resources on dimensions relevant to the reclaimee.
			childEligible := false
			if isVictim, reason := cp.isImmediateVictim(reclaimee, attr.deserved); isVictim {
				// If hierarchy is enabled and ancestor reclaim is configured, even if the reclaimee is an immediate victim,
				// we still need to check if it shares any non-root ancestor within the configured level with the reclaimer and
				// both leaf deserved signals are empty for requested resources under shared ancestor scope.
				// If so, the reclaimee will not be reclaimed to avoid unnecessary preemption when there is no real contention.
				if hierarchyEnabled && cp.ancestorReclaimLevel > 0 && cp.sharesAnyNonRootAncestorWithinLevel(reclaimerAttr, attr) &&
					!hasRelevantDeserved(reclaimer, reclaimerAttr.deserved) {
					klog.V(5).Infof("[capacity] Skip reclaim for reclaimee <%s/%s> from queue <%s>: both leaf deserved signals are empty for requested resources under shared ancestor scope ancestorReclaimLevel=%d",
						reclaimee.Namespace, reclaimee.Name, attr.queueID, cp.ancestorReclaimLevel)
					continue
				}
				childEligible = true
				klog.V(5).Infof("%s. It's a victim for queue <%s>.", reason, attr.name)
			} else if exceeds, dims, reason := cp.checkDeservedExceedance(
				allocated, attr.deserved, reclaimee, reclaimer, attr.name); exceeds {
				childEligible = true
				klog.V(5).Infof("[capacity] Reclaimee <%s/%s> is a victim from queue <%s> for reclaimer <%s/%s>. "+
					"Allocated: <%v>, Deserved: <%v>, Reclaimee Resreq: <%v>, Reclaimable on dimensions: %v.",
					reclaimee.Namespace, reclaimee.Name, attr.queueID, reclaimer.Namespace, reclaimer.Name,
					allocated, attr.deserved, reclaimee.Resreq, dims)
			} else {
				klog.V(5).Infof("%s.", reason)
			}

			if !childEligible {
				continue
			}

			// If hierarchy is enabled and ancestor reclaim is configured,
			// check ancestors up to the configured level to avoid reclaiming a victim when there is no real contention.
			ancestorEligible := true
			if hierarchyEnabled && cp.ancestorReclaimLevel > 0 {
				for level := 1; level <= cp.ancestorReclaimLevel; level++ {
					ancestorAttr, needCheck := cp.getReclaimeeAncestorToCheck(reclaimerAttr, attr, level)
					if !needCheck {
						continue
					}
					if ancestorAttr == nil {
						ancestorEligible = false
						klog.Warningf("[capacity] Skip reclaimee <%s/%s>: ancestor check target at level %d is nil", reclaimee.Namespace, reclaimee.Name, level)
						break
					}
					if _, found := allocations[ancestorAttr.queueID]; !found {
						allocations[ancestorAttr.queueID] = ancestorAttr.allocated.Clone()
					}
					ancestorAllocated := allocations[ancestorAttr.queueID]
					ancestorAllocations[ancestorAttr.queueID] = ancestorAllocated

					if isVictim, reason := cp.isImmediateVictim(reclaimee, ancestorAttr.deserved); isVictim {
						klog.V(5).Infof("%s. It's a victim for ancestor queue <%s>.", reason, ancestorAttr.name)
						continue
					}

					exceeds, dims, reason := cp.checkDeservedExceedance(
						ancestorAllocated, ancestorAttr.deserved, reclaimee, reclaimer, ancestorAttr.name)
					if !exceeds {
						ancestorEligible = false
						klog.V(5).Infof("%s.", reason)
						break
					}
					klog.V(5).Infof("[capacity] Reclaimee <%s/%s> is a victim from ancestor queue <%s> for reclaimer <%s/%s>. "+
						"Allocated: <%v>, Deserved: <%v>, Reclaimee Resreq: <%v>, Reclaimable on dimensions: %v.",
						reclaimee.Namespace, reclaimee.Name, ancestorAttr.name, reclaimer.Namespace, reclaimer.Name,
						ancestorAllocated, ancestorAttr.deserved, reclaimee.Resreq, dims)
				}
			}

			if !ancestorEligible {
				continue
			}

			allocated.Sub(reclaimee.Resreq)
			for _, ancestorAllocated := range ancestorAllocations {
				ancestorAllocated.Sub(reclaimee.Resreq)
			}
			victims = append(victims, reclaimee)
			klog.V(5).Infof("[capacity] Current victims: %+v.", victims)
		}
		klog.V(4).Infof("[capacity] Victims from capacity plugin: victims=%+v reclaimer=%s.", victims, reclaimer)
		return victims, util.Permit
	})

	// Currently handles GangReclaim and GangPreempt only.
	// Support for legacy task-level preempt/reclaim will be added in the future.
	ssn.AddUnifiedEvictableFn(cp.Name(), func(evictCtx *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		if evictCtx.Kind == api.EvictionKindGangPreempt {
			// Capacity does not filter victims for preemption (same as legacy preempt
			// where no PreemptableFn is registered). Permit all candidates.
			return candidates, util.Permit
		}
		// GangReclaim path: apply guarantee and deserved checks per victim queue.
		// Unlike legacy task-level reclaim, shouldSkipReclaimee is not called here
		// because the chance of a gang having zero intersecting resource dimensions
		// with a victim is low. Can be added later if needed.
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return nil, util.Reject
		}
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}
		for _, reclaimee := range candidates {
			job := ssn.Jobs[reclaimee.Job]
			attr := cp.queueOpts[job.Queue]
			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if satisfies, _ := cp.checkGuaranteeConstraint(allocated, reclaimee, attr.guarantee); !satisfies {
				continue
			}
			if isVictim, _ := cp.isImmediateVictim(reclaimee, attr.deserved); isVictim {
				allocated.Sub(reclaimee.Resreq)
				victims = append(victims, reclaimee)
				continue
			}
			reclaimable, _ := allocated.GreaterPartlyWithRelevantDimensions(attr.deserved, reclaimee.Resreq)
			if reclaimable {
				allocated.Sub(reclaimee.Resreq)
				victims = append(victims, reclaimee)
			}
		}
		klog.V(4).Infof("[capacity] Victims from capacity UnifiedEvictableFn: victims=%+v", victims)
		return victims, util.Permit
	})

	ssn.AddPreemptiveFn(cp.Name(), func(obj interface{}, candidates []*api.TaskInfo) bool {
		if !readyToSchedule {
			klog.V(3).Infof("Capacity plugin failed to check queue's hierarchical structure!")
			return false
		}

		queue := obj.(*api.QueueInfo)
		if queue.Queue.Status.State != scheduling.QueueStateOpen {
			klog.V(3).Infof("Queue <%s> current state: %s, is not open state, can not reclaim for tasks.",
				queue.Name, queue.Queue.Status.State)
			return false
		}

		attr := cp.queueOpts[queue.UID]
		totalReq := api.EmptyResource()
		for _, task := range candidates {
			if task != nil {
				totalReq.Add(task.Resreq)
			}
		}
		futureUsed := attr.allocated.Clone().Add(totalReq)

		if allocatable, _ := futureUsed.LessEqualWithDimensionAndResourcesName(attr.realCapability, totalReq); !allocatable {
			klog.V(3).Infof("Queue <%v> cannot reclaim because futureUsed <%v> exceeds realCapability <%v>.",
				queue.Name, futureUsed, attr.realCapability)
			return false
		}

		// If there is a single dimension whose deserved is greater than allocated, current tasks can reclaim by preempting others.
		isPreemptive, resourceNames := futureUsed.LessEqualPartlyWithDimensionZeroFiltered(attr.deserved, totalReq)
		if isPreemptive {
			klog.V(3).Infof("Queue <%v> can reclaim on resource dimensions: %v. "+
				"The futureUsed: %v, deserved: %v, allocated: %v, tasks requested: %v",
				queue.Name, resourceNames, futureUsed, attr.deserved, attr.allocated, totalReq)
		} else {
			klog.V(4).Infof("Queue <%v> itself can not reclaim, futureUsed: %v, deserved: %v, requested: %v",
				queue.Name, futureUsed, attr.deserved, totalReq)
			if hierarchyEnabled && cp.ancestorReclaimLevel > 0 {
				for level := 1; level <= cp.ancestorReclaimLevel; level++ {
					ancestorID, found := queueAncestorAtDepth(attr, level)
					if !found || ancestorID == rootQueueID {
						continue
					}
					ancestorAttr := cp.queueOpts[ancestorID]
					if ancestorAttr == nil {
						continue
					}

					futureUsedAncestor := ancestorAttr.allocated.Clone().Add(totalReq)
					isPreemptive, resourceNames = futureUsedAncestor.LessEqualPartlyWithDimensionZeroFiltered(ancestorAttr.deserved, totalReq)
					if isPreemptive {
						klog.V(3).Infof("Queue's ancestor <%v> can reclaim on resource dimensions: %v. "+
							"The futureUsedAncestor: %v, deserved: %v, allocated: %v, task requested: %v",
							ancestorAttr.name, resourceNames, futureUsedAncestor, ancestorAttr.deserved, ancestorAttr.allocated, totalReq)
						break
					}
				}
			}
			if !isPreemptive {
				klog.V(4).Infof("Queue <%v> and its ancestors can not reclaim. futureUsed: %v, deserved: %v, requested: %v",
					queue.Name, futureUsed, attr.deserved, totalReq)
			}
		}

		// PreemptiveFn is the opposite of OverusedFn in proportion plugin cause as long as there is a one-dimensional
		// resource whose deserved is greater than allocated, current tasks can reclaim by preempt others.
		return isPreemptive
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

		allocatable := cp.checkQueueAllocatableHierarchically(ssn, queue, candidate)

		// If queue has capacity and task has the QueueAllocationGate annotation.
		if allocatable && utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) &&
			api.HasQueueAllocationGateAnnotation(candidate.Pod) {
			cp.addTaskToReservedCache(queue.UID, candidate)
		}

		return allocatable
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
			klog.V(3).Infof("Queue <%s> current state: %s, is not open state, reject job <%s/%s>.",
				queue.Name, queue.Queue.Status.State, job.Namespace, job.Name)
			return util.Reject
		}
		// If no capability is set, always enqueue the job.
		if attr.realCapability == nil {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil && !(cp.dynamicResourceAllocationEnable && attr.dra != nil && job.GetMinDRAResources() != nil) {
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}

		if !cp.checkJobEnqueueableHierarchically(ssn, queue, job) {
			return util.Reject
		}

		// job enqueued
		deductedResources := job.DeductSchGatedResources(job.GetMinResources())
		attr.inqueue.Add(deductedResources)
		var minDRAReq map[string]*api.DRAResource
		if cp.dynamicResourceAllocationEnable && attr.dra != nil {
			minDRAReq = job.GetMinDRAResources()
			if minDRAReq != nil {
				updateDRAInqueue(attr.dra, minDRAReq)
			}
		}
		// If enable hierarchy, update the inqueue resource for all ancestors queues
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := cp.queueOpts[ancestorID]
				ancestorAttr.inqueue.Add(deductedResources)
				if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && minDRAReq != nil {
					updateDRAInqueue(ancestorAttr.dra, minDRAReq)
				}
			}
		}
		klog.V(5).Infof("job <%s/%s> enqueued", job.Namespace, job.Name)
		return util.Permit
	})

	ssn.AddPrePredicateFn(cp.Name(), func(task *api.TaskInfo) error {
		state := &capacityState{
			queueAttrs:        make(map[api.QueueID]*queueAttr),
			reservedTaskQueue: make(map[api.TaskID]api.QueueID, len(cp.reservedTaskQueue)),
		}

		for _, queue := range cp.queueOpts {
			state.queueAttrs[queue.queueID] = queue.Clone()
		}
		// Snapshot reservation membership together with the per-queue reservedSubtree amounts
		// (cloned just above) so simulate-time capacity checks read both from this frozen
		// snapshot rather than mixing it with live plugin state that mutates later this cycle.
		for taskID, queueID := range cp.reservedTaskQueue {
			state.reservedTaskQueue[taskID] = queueID
		}

		ssn.GetCycleState(task.UID).Write(capacityStateKey, state)
		return nil
	})

	ssn.AddSimulateAddTaskFn(cp.Name(), func(ctx context.Context, cycleState fwk.CycleState, taskToSchedule *api.TaskInfo, taskToAdd *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		state, err := getCapacityState(cycleState)
		if err != nil {
			return fmt.Errorf("failed to get capacity state: %w", err)
		}

		job := ssn.Jobs[taskToAdd.Job]
		if job == nil {
			return fmt.Errorf("[capacity] job %s not found in session (orphaned task from deleted PodGroup)", taskToAdd.Job)
		}
		attr := state.queueAttrs[job.Queue]
		if attr == nil {
			return fmt.Errorf("[capacity] queue %s not found", job.Queue)
		}
		attr.allocated.Add(taskToAdd.Resreq)
		if cp.dynamicResourceAllocationEnable && attr.dra != nil && taskToAdd.DRAResreq != nil {
			addTaskDRAAllocated(attr, taskToAdd)
		}
		updateQueueAttrShare(attr)
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := state.queueAttrs[ancestorID]
				ancestorAttr.allocated.Add(taskToAdd.Resreq)
				if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && taskToAdd.DRAResreq != nil {
					addTaskDRAAllocated(ancestorAttr, taskToAdd)
				}
			}
		}
		return nil
	})

	ssn.AddSimulateRemoveTaskFn(cp.Name(), func(ctx context.Context, cycleState fwk.CycleState, taskToSchedule *api.TaskInfo, taskToRemove *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		state, err := getCapacityState(cycleState)
		if err != nil {
			return fmt.Errorf("failed to get capacity state: %w", err)
		}
		job := ssn.Jobs[taskToRemove.Job]
		if job == nil {
			return fmt.Errorf("[capacity] job %s not found in session (orphaned task from deleted PodGroup)", taskToRemove.Job)
		}
		attr := state.queueAttrs[job.Queue]
		if attr == nil {
			return fmt.Errorf("[capacity] queue %s not found", job.Queue)
		}
		attr.allocated.Sub(taskToRemove.Resreq)
		if cp.dynamicResourceAllocationEnable && attr.dra != nil && taskToRemove.DRAResreq != nil {
			removeTaskDRAAllocated(attr, taskToRemove)
		}
		updateQueueAttrShare(attr)
		if hierarchyEnabled {
			for _, ancestorID := range attr.ancestors {
				ancestorAttr := state.queueAttrs[ancestorID]
				ancestorAttr.allocated.Sub(taskToRemove.Resreq)
				if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && taskToRemove.DRAResreq != nil {
					removeTaskDRAAllocated(ancestorAttr, taskToRemove)
				}
			}
		}
		return nil
	})

	ssn.AddSimulateAllocatableFn(cp.Name(), func(ctx context.Context, cycleState fwk.CycleState, queue *api.QueueInfo, candidate *api.TaskInfo) bool {
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
			// Simulate path: attr is the per-task clone, so membership must come from the
			// snapshot taken alongside it (state.reservedTaskQueue), not the live plugin index.
			return cp.queueAllocatableWithReserved(attr, candidate, queue, cp.dynamicResourceAllocationEnable, cp.draConsumableCapacityEnable, state.reservedTaskQueue)
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
			if job == nil {
				klog.Warningf("[capacity] Skip allocate event for task <%s/%s>: job <%s> not found in session (orphaned task from deleted PodGroup)",
					event.Task.Namespace, event.Task.Name, event.Task.Job)
				return
			}
			attr := cp.queueOpts[job.Queue]
			if attr == nil {
				klog.Warningf("[capacity] Skip allocate event for task <%s/%s>: queue <%s> not found in queueOpts",
					event.Task.Namespace, event.Task.Name, job.Queue)
				return
			}
			attr.allocated.Add(event.Task.Resreq)
			if cp.dynamicResourceAllocationEnable && attr.dra != nil && event.Task.DRAResreq != nil {
				addTaskDRAAllocated(attr, event.Task)
			}
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)

			cp.updateShare(attr)
			if hierarchyEnabled {
				for _, ancestorID := range attr.ancestors {
					ancestorAttr := cp.queueOpts[ancestorID]
					ancestorAttr.allocated.Add(event.Task.Resreq)
					if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && event.Task.DRAResreq != nil {
						addTaskDRAAllocated(ancestorAttr, event.Task)
					}
				}
			}

			klog.V(4).Infof("[capacity] AllocateFunc: task <%v/%v>, resreq <%v>, share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)

			// Remove task from reserved cache when it gets allocated
			if utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) {
				cp.removeTaskFromReservedCache(event.Task.UID)
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			if job == nil {
				klog.Warningf("[capacity] Skip deallocate event for task <%s/%s>: job <%s> not found in session (orphaned task from deleted PodGroup)",
					event.Task.Namespace, event.Task.Name, event.Task.Job)
				return
			}
			attr := cp.queueOpts[job.Queue]
			if attr == nil {
				klog.Warningf("[capacity] Skip deallocate event for task <%s/%s>: queue <%s> not found in queueOpts",
					event.Task.Namespace, event.Task.Name, job.Queue)
				return
			}
			attr.allocated.Sub(event.Task.Resreq)
			if cp.dynamicResourceAllocationEnable && attr.dra != nil && event.Task.DRAResreq != nil {
				removeTaskDRAAllocated(attr, event.Task)
			}
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory, attr.allocated.ScalarResources)

			cp.updateShare(attr)
			if hierarchyEnabled {
				for _, ancestorID := range attr.ancestors {
					ancestorAttr := cp.queueOpts[ancestorID]
					ancestorAttr.allocated.Sub(event.Task.Resreq)
					if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && event.Task.DRAResreq != nil {
						removeTaskDRAAllocated(ancestorAttr, event.Task)
					}
				}
			}

			klog.V(4).Infof("[capacity] DeallocateFunc: task <%v/%v>, resreq <%v>, share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)

			// Restore task to reserved cache on rollback so capacity remains accounted for
			if utilfeature.DefaultFeatureGate.Enabled(features.SchedulingGatesQueueAdmission) &&
				api.HasQueueAllocationGateAnnotation(event.Task.Pod) {
				cp.addTaskToReservedCache(job.Queue, event.Task)
			}
		},
	})
}

func (cp *capacityPlugin) parseArguments() {
	ancestorReclaimLevel := 0
	cp.pluginArguments.GetInt(&ancestorReclaimLevel, ancestorReclaimLevelKey)

	if ancestorReclaimLevel < 0 {
		klog.Warningf("%s should be non-negative, got %d. Falling back to 0.", ancestorReclaimLevelKey, ancestorReclaimLevel)
		ancestorReclaimLevel = 0
	}

	cp.ancestorReclaimLevel = ancestorReclaimLevel
	klog.V(4).Infof("[capacity] reclaim ancestor level configured as %d", cp.ancestorReclaimLevel)
}

func (cp *capacityPlugin) getReclaimeeAncestorToCheck(reclaimerAttr, reclaimeeAttr *queueAttr, level int) (*queueAttr, bool) {
	if reclaimerAttr == nil || reclaimeeAttr == nil || level <= 0 {
		return nil, false
	}

	reclaimerAncestors := ancestorsByLevel(reclaimerAttr, level)
	reclaimeeAncestors := ancestorsByLevel(reclaimeeAttr, level)

	reclaimeeAncestorID, reclaimeeFound := reclaimeeAncestors[level]
	if !reclaimeeFound || reclaimeeAncestorID == rootQueueID {
		return nil, false
	}

	reclaimerAncestorID, reclaimerFound := reclaimerAncestors[level]
	if reclaimerFound && reclaimerAncestorID == reclaimeeAncestorID {
		return nil, false
	}

	ancestorAttr := cp.queueOpts[reclaimeeAncestorID]
	if ancestorAttr == nil {
		return nil, true
	}

	return ancestorAttr, true
}

func (cp *capacityPlugin) OnSessionClose(ssn *framework.Session) {
	for _, attr := range cp.queueOpts {
		overused := attr.share > 1
		metrics.UpdateQueueOverused(attr.name, overused)
	}
	cp.totalResource = nil
	cp.totalGuarantee = nil
	cp.queueOpts = nil
	cp.queueGateReservedTasks = nil
	cp.reservedTaskQueue = nil
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

				deserved:          api.NewResource(queue.Queue.Spec.Deserved),
				allocated:         api.EmptyResource(),
				request:           api.EmptyResource(),
				elastic:           api.EmptyResource(),
				inqueue:           api.EmptyResource(),
				guarantee:         api.EmptyResource(),
				resourceClaimRefs: make(map[string]int),
				reservedSubtree:   api.EmptyResource(),
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
			attr.dra = newDRAQuotaAttr(queue.Queue.Spec.Capability, queue.Queue.Spec.Deserved, queue.Queue.Spec.Guarantee.Resource)
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
					if cp.dynamicResourceAllocationEnable && attr.dra != nil && t.DRAResreq != nil {
						addTaskDRAAllocated(attr, t)
					}
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			// calculate inqueue resource for inqueue jobs
			// deduct already-allocated task resources from minResources to avoid double-counting:
			// tasks in Allocated/Binding state are already tracked in attr.allocated (via AllocatedStatus),
			// but the PodGroup stays Inqueue until tasks reach Running/Bound (ScheduledStatus).
			// Without this deduction, the same resources appear in both attr.allocated and attr.inqueue.
			if job.PodGroup.Spec.MinResources != nil {
				inqueued := util.GetInqueueResource(job, job.Allocated)
				attr.inqueue.Add(job.DeductSchGatedResources(inqueued))
			}
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

		return cp.compareShareWithDeserved(cp.queueOpts[lv.UID], cp.queueOpts[rv.UID])
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
		visited := make(map[api.QueueID]struct{})
		err := cp.updateAncestors(queue, ssn, visited)
		if err != nil {
			klog.Errorf("Failed to update Queue <%s> attributes, error: %v", queue.Name, err)
			return false
		}
	}

	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		attr := cp.queueOpts[job.Queue]
		if attr == nil {
			klog.Warningf("Job <%s/%s> references queue %s which is not found in session queues, skipping",
				job.Namespace, job.Name, job.Queue)
			continue
		}
		if len(attr.children) > 0 {
			klog.Errorf("The Queue <%s> of Job <%s/%s> is not leaf queue", attr.name, job.Namespace, job.Name)
			return false
		}

		oldAllocated := attr.allocated.Clone()
		oldRequest := attr.request.Clone()
		oldInqueue := attr.inqueue.Clone()
		oldElastic := attr.elastic.Clone()
		var oldDRA *draQuotaAttr
		if attr.dra != nil {
			oldDRA = attr.dra.Clone()
		}

		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
					if cp.dynamicResourceAllocationEnable && attr.dra != nil && t.DRAResreq != nil {
						addTaskDRAAllocated(attr, t)
					}
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			// same double-counting fix as buildQueueAttrs: deduct already-allocated resources
			// so tasks in Allocated/Binding state are not counted in both attr.allocated and attr.inqueue.
			if job.PodGroup.Spec.MinResources != nil {
				inqueued := util.GetInqueueResource(job, job.Allocated)
				attr.inqueue.Add(job.DeductSchGatedResources(inqueued))
			}
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

		allocatedDelta := attr.allocated.Clone().Sub(oldAllocated)
		requestDelta := attr.request.Clone().Sub(oldRequest)
		inqueueDelta := attr.inqueue.Clone().Sub(oldInqueue)
		elasticDelta := attr.elastic.Clone().Sub(oldElastic)

		var draDelta map[string]*api.DRAResource
		if attr.dra != nil {
			draDelta = getDRADelta(attr.dra, oldDRA)
		}

		for _, ancestor := range attr.ancestors {
			ancestorAttr := cp.queueOpts[ancestor]
			ancestorAttr.allocated.Add(allocatedDelta)
			ancestorAttr.request.Add(requestDelta)
			ancestorAttr.inqueue.Add(inqueueDelta)
			ancestorAttr.elastic.Add(elasticDelta)
			if cp.dynamicResourceAllocationEnable && ancestorAttr.dra != nil && draDelta != nil {
				// Only propagate Delta for DeviceClasses that are configured in ancestor's capability
				filteredDelta := make(map[string]*api.DRAResource)
				for dc, res := range draDelta {
					if _, ok := ancestorAttr.dra.capability[dc]; ok {
						filteredDelta[dc] = res
					}
				}
				if len(filteredDelta) > 0 {
					updateDRAAllocated(ancestorAttr.dra, filteredDelta)
				}
			}
		}

		klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
			attr.name, attr.allocated.String(), attr.request.String(), attr.inqueue.String(), attr.elastic.String())
	}

	// init root queue: realCapability is and capability is set to ininity if capability is not set.  deserved are also set if empty.
	// root queue is default queue and users are not aware of it. User is allowed to sumbit jobs which exceed current cluster total resources,
	// so root queue should not restrict by current total resources. User need to restrict resource through creating queue manually
	rootQueueAttr := cp.queueOpts[api.QueueID(cp.rootQueue)]
	if rootQueueAttr == nil {
		klog.Warningf("Root queue %q not found in session queues, skipping hierarchy initialization", cp.rootQueue)
		return false
	}
	infinityResource := api.InfiniteResource()
	if cp.totalResource.ScalarResources != nil {
		for k := range cp.totalResource.ScalarResources {
			infinityResource.SetScalar(k, math.MaxInt64)
		}
	}
	if rootQueueAttr.capability.IsEmpty() {
		rootQueueAttr.capability = infinityResource
	}
	rootQueueAttr.realCapability = rootQueueAttr.capability
	// checkHierarchicalQueue only logs warnings and never returns errors
	// to avoid aborting the entire scheduling cycle due to configuration issues
	cp.checkHierarchicalQueue(rootQueueAttr)

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
			return cp.compareShareWithDeserved(cp.queueOpts[lv.UID], cp.queueOpts[rv.UID])
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

		return cp.compareShareWithDeserved(cp.queueOpts[lvParentID], cp.queueOpts[rvParentID])
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

		deserved:          api.NewResource(queue.Queue.Spec.Deserved),
		allocated:         api.EmptyResource(),
		request:           api.EmptyResource(),
		elastic:           api.EmptyResource(),
		inqueue:           api.EmptyResource(),
		guarantee:         api.EmptyResource(),
		capability:        api.EmptyResource(),
		realCapability:    api.EmptyResource(),
		resourceClaimRefs: make(map[string]int),
		reservedSubtree:   api.EmptyResource(),
	}
	if len(queue.Queue.Spec.Capability) != 0 {
		attr.capability = api.NewResource(queue.Queue.Spec.Capability)
	}

	if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
		attr.guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
	}

	attr.dra = newDRAQuotaAttr(queue.Queue.Spec.Capability, queue.Queue.Spec.Deserved, queue.Queue.Spec.Guarantee.Resource)

	return attr
}

func (cp *capacityPlugin) updateAncestors(queue *api.QueueInfo, ssn *framework.Session, visited map[api.QueueID]struct{}) error {
	if queue.Name == cp.rootQueue {
		return nil
	}

	// Check for cycles in queue hierarchy
	if _, exist := visited[queue.UID]; exist {
		return fmt.Errorf("cycle detected in queue hierarchy for queue %s", queue.Name)
	}
	visited[queue.UID] = struct{}{}
	defer delete(visited, queue.UID)

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
		err := cp.updateAncestors(parentInfo, ssn, visited)
		if err != nil {
			return err
		}
	}

	cp.queueOpts[parentInfo.UID].children[queue.UID] = cp.queueOpts[queue.UID]
	cp.queueOpts[queue.UID].ancestors = append(cp.queueOpts[parentInfo.UID].ancestors, parentInfo.UID)
	return nil
}

func (cp *capacityPlugin) checkHierarchicalQueue(attr *queueAttr) {
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()
	for _, childAttr := range attr.children {
		childAttr.deserved = helpers.Max(childAttr.deserved, childAttr.guarantee)
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

		// Check if child queue's capability exceeds parent queue's capability.
		if exceeds, resources := childAttr.capability.GreaterPartly(attr.capability, api.Infinity); exceeds {
			klog.Errorf("Child queue %s capability (%s) exceeds parent queue %s capability (%s) on resources %v. "+
				"Child's effective capability will be limited by parent.",
				childAttr.name, childAttr.capability, attr.name, attr.capability, resources)
		}
	}

	if attr.name == cp.rootQueue {
		attr.guarantee = totalGuarantee
		attr.deserved = totalDeserved
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
		klog.Errorf("Sum of child queue deserved (%s) exceeds parent queue %s deserved (%s). "+
			"This may affect resource distribution during scheduling.",
			totalDeserved, attr.name, attr.deserved)
	}

	// Check if the parent queue's guarantee resources are less than the total guarantee resources of child queues
	if attr.guarantee.LessPartly(totalGuarantee, api.Zero) {
		klog.Errorf("Sum of child queue guarantees (%s) exceeds parent queue %s guarantee (%s). "+
			"Not all child guarantees can be satisfied simultaneously.",
			totalGuarantee, attr.name, attr.guarantee)
	}

	// Recursively check child queues
	for _, childAttr := range attr.children {
		cp.checkHierarchicalQueue(childAttr)
	}
}

// compareShareWithDeserved compares two queueAttr by share; when shares are equal,
// queues with non-empty deserved are prioritized over best-effort queues.
// Returns negative if l should come before r.
func (cp *capacityPlugin) compareShareWithDeserved(lattr, rattr *queueAttr) int {
	if lattr.share == rattr.share {
		lHasDeserved := !lattr.deserved.IsEmpty()
		rHasDeserved := !rattr.deserved.IsEmpty()
		if lHasDeserved == rHasDeserved {
			return 0
		}
		if lHasDeserved {
			return -1
		}
		return 1
	}

	if lattr.share < rattr.share {
		return -1
	}
	return 1
}

func (cp *capacityPlugin) updateShare(attr *queueAttr) {
	updateQueueAttrShare(attr)
	metrics.UpdateQueueShare(attr.name, attr.share)
}

func (cp *capacityPlugin) isLeafQueue(queueID api.QueueID) bool {
	return len(cp.queueOpts[queueID].children) == 0
}

func (cp *capacityPlugin) queueAllocatable(queue *api.QueueInfo, candidate *api.TaskInfo, draEnabled bool, consumableCapacityEnabled bool) bool {
	attr := cp.queueOpts[queue.UID]
	// Live path: attr is a live queueOpts entry, so its reservedSubtree and the membership
	// index cp.reservedTaskQueue share the same (live) source and stay consistent.
	return cp.queueAllocatableWithReserved(attr, candidate, queue, draEnabled, consumableCapacityEnabled, cp.reservedTaskQueue)
}

// addTaskToReservedCache adds a task to the reserved cache
// This should be called when a task passes capacity checks
func (cp *capacityPlugin) addTaskToReservedCache(queueID api.QueueID, task *api.TaskInfo) {
	if cp.queueGateReservedTasks[queueID] == nil {
		cp.queueGateReservedTasks[queueID] = make(map[api.TaskID]*api.TaskInfo)
	}
	// Adding to a map is idempotent, but adding to the reservedSubtree aggregate is not.
	// AllocatableFn may admit the same candidate more than once within a cycle (multiple
	// node attempts) and the rollback path re-adds it, so guard against double counting.
	if _, exists := cp.queueGateReservedTasks[queueID][task.UID]; exists {
		return
	}
	cp.queueGateReservedTasks[queueID][task.UID] = task
	cp.reservedTaskQueue[task.UID] = queueID
	cp.addReservedSubtree(queueID, task.Resreq)
	klog.V(4).Infof("Added task <%s/%s> to reserved cache for queue <%s>", task.Namespace, task.Name, queueID)
}

// RemoveTaskFromReservedCache removes a specific task from the reserved cache
// This should be called when a task becomes allocated (no longer needs reservation)
// It searches across all queues to find and remove the task
func (cp *capacityPlugin) removeTaskFromReservedCache(taskID api.TaskID) {
	queueID, ok := cp.reservedTaskQueue[taskID]
	if !ok {
		return
	}
	task := cp.queueGateReservedTasks[queueID][taskID]
	delete(cp.queueGateReservedTasks[queueID], taskID)
	delete(cp.reservedTaskQueue, taskID)
	if task != nil {
		cp.subReservedSubtree(queueID, task.Resreq)
	}
	// Clean up empty queue entries
	if len(cp.queueGateReservedTasks[queueID]) == 0 {
		delete(cp.queueGateReservedTasks, queueID)
	}
	klog.V(4).Infof("Removed task <%s> from reserved cache for queue <%s>", taskID, queueID)
}

// rebuildReservedCache clears and rebuilds the reserved tasks cache for this scheduling cycle.
// It scans all pending tasks and adds those that have passed capacity checks in previous cycles
// but are not yet allocated. These are identified by:
// - NO queue allocation scheduling gate (gate was removed after passing capacity)
// - HAS queue allocation scheduling gate annotation (proof they opted-in and passed capacity check)
func (cp *capacityPlugin) buildQueueReservedTasksCache(ssn *framework.Session) {
	// Initialize the cache for this session
	cp.queueGateReservedTasks = make(map[api.QueueID]map[api.TaskID]*api.TaskInfo)
	cp.reservedTaskQueue = make(map[api.TaskID]api.QueueID)

	// Scan all pending tasks and rebuild cache
	for _, job := range ssn.Jobs {
		for _, task := range job.TaskStatusIndex[api.Pending] {
			// Tasks that passed capacity have: NO gate + HAS annotation + Pending status
			if !task.SchGated && api.HasQueueAllocationGateAnnotation(task.Pod) {
				if cp.queueGateReservedTasks[job.Queue] == nil {
					cp.queueGateReservedTasks[job.Queue] = make(map[api.TaskID]*api.TaskInfo)
				}
				cp.queueGateReservedTasks[job.Queue][task.UID] = task
				cp.reservedTaskQueue[task.UID] = job.Queue
				klog.V(4).Infof("Added task <%s/%s> to reserved cache for queue <%s>",
					task.Namespace, task.Name, job.Queue)
			}
		}
	}
}

// aggregateReservedSubtree seeds every queue's reservedSubtree aggregate from the freshly
// built reserved-tasks cache. It MUST run after the queue attrs and their ancestor/children
// links are built (buildHierarchicalQueueAttrs / buildQueueAttrs), so that reservations
// recorded under a leaf queue are propagated to every ancestor's subtree total.
func (cp *capacityPlugin) aggregateReservedSubtree() {
	for leafID, tasks := range cp.queueGateReservedTasks {
		for _, task := range tasks {
			cp.addReservedSubtree(leafID, task.Resreq)
		}
	}
}

// addReservedSubtree adds res to the reservedSubtree aggregate of the given leaf queue and
// every one of its ancestors. This mirrors how `allocated` is propagated up the chain.
func (cp *capacityPlugin) addReservedSubtree(leafID api.QueueID, res *api.Resource) {
	leaf := cp.queueOpts[leafID]
	if leaf == nil {
		return
	}
	leaf.reservedSubtree.Add(res)
	for _, ancestorID := range leaf.ancestors {
		if ancestor := cp.queueOpts[ancestorID]; ancestor != nil {
			ancestor.reservedSubtree.Add(res)
		}
	}
}

// subReservedSubtree subtracts res from the reservedSubtree aggregate of the given leaf
// queue and every one of its ancestors.
func (cp *capacityPlugin) subReservedSubtree(leafID api.QueueID, res *api.Resource) {
	leaf := cp.queueOpts[leafID]
	if leaf == nil {
		return
	}
	leaf.reservedSubtree.Sub(res)
	for _, ancestorID := range leaf.ancestors {
		if ancestor := cp.queueOpts[ancestorID]; ancestor != nil {
			ancestor.reservedSubtree.Sub(res)
		}
	}
}

// reservedTasks is the membership index used for candidate exclusion. It MUST originate from
// the same snapshot as attr.reservedSubtree: the live cp.reservedTaskQueue when attr is a live
// queueOpts entry, or the capacityState snapshot when attr is a cloned simulate attr. Mixing
// the two sources lets membership and amount drift and can underflow the reserved subtraction.
func (cp *capacityPlugin) queueAllocatableWithReserved(attr *queueAttr, candidate *api.TaskInfo, queue *api.QueueInfo, draEnabled bool, consumableCapacityEnabled bool, reservedTasks map[api.TaskID]api.QueueID) bool {
	if draEnabled && attr.dra != nil {
		candidateDRA := incrementalTaskDRA(attr, candidate)
		if candidateDRA != nil {
			if !checkDRAAllocatable(attr.dra, candidateDRA, consumableCapacityEnabled, false) {
				klog.V(3).Infof("Queue <%v> DRA resource insufficient for candidate <%v>", queue.Name, candidate.Name)
				return false
			}
		}
	}
	// Reserved (gate-admitted but not yet allocated) tasks are recorded under their leaf
	// queue, but capacity is checked per queue across the whole ancestor chain (see
	// checkQueueAllocatableHierarchically). attr.reservedSubtree is the precomputed sum of
	// reservations in this queue's whole subtree, so an ancestor's check accounts for the
	// reservations of every descendant leaf; otherwise sibling leaves could collectively
	// reserve past the ancestor's realCapability (quota bypass).
	reserved := api.EmptyResource()
	if attr.reservedSubtree != nil {
		reserved = attr.reservedSubtree.Clone()
	}
	// Exclude the candidate if it is itself currently reserved (admitted in a prior cycle
	// and rebuilt into the cache); it is added separately via futureUsed below. In every
	// real call site attr is on the candidate's leaf->root path, so a reserved candidate is
	// always counted in attr.reservedSubtree and must be subtracted exactly once. Membership
	// and amount come from the same snapshot (see the reservedTasks contract above).
	if _, ok := reservedTasks[candidate.UID]; ok {
		if candidate.Resreq.LessEqual(reserved, api.Zero) {
			reserved.Sub(candidate.Resreq)
		} else {
			// A consistent snapshot guarantees reserved >= candidate here. If that invariant
			// is ever violated, skip the exclusion (conservatively over-counting the candidate)
			// rather than panic in the assertive Resource.Sub or under-count and bypass quota.
			klog.Warningf("[capacity] queue <%v>: reserved subtree <%v> does not cover reserved candidate <%v> request <%v>; skipping candidate exclusion",
				queue.Name, reserved, candidate.Name, candidate.Resreq)
		}
	}

	// Include reserved resources in capacity check
	futureUsed := attr.allocated.Clone().Add(reserved).Add(candidate.Resreq)
	allocatable, _ := futureUsed.LessEqualWithDimensionAndResourcesName(attr.realCapability, candidate.Resreq)

	if !allocatable {
		klog.V(3).Infof("Queue <%v>: realCapability <%v>, allocated <%v>, reserved <%v>; Candidate <%v>: resource request <%v>",
			queue.Name, attr.realCapability, attr.allocated, reserved, candidate.Name, candidate.Resreq)
	}

	return allocatable
}

func (cp *capacityPlugin) checkQueueAllocatableHierarchically(ssn *framework.Session, queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	// If hierarchical queue is not enabled, list will only contain the queue itself.
	list := append(cp.queueOpts[queue.UID].ancestors, queue.UID)
	// Check whether the candidate task can be allocated to the queue and all its ancestors.
	for i := len(list) - 1; i >= 0; i-- {
		if !cp.queueAllocatable(ssn.Queues[list[i]], candidate, cp.dynamicResourceAllocationEnable, cp.draConsumableCapacityEnable) {
			// If log level is 5, print the information of all queues from leaf to ancestor.
			if klog.V(5).Enabled() {
				for j := i - 1; j >= 0; j-- {
					cp.queueAllocatable(ssn.Queues[list[j]], candidate, cp.dynamicResourceAllocationEnable, cp.draConsumableCapacityEnable)
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

	valid, reasons := r.LessEqualWithDimensionAndResourcesName(attr.realCapability, minReq)
	if !valid {
		return valid, reasons
	}

	// Check DRA limits if enabled
	if cp.dynamicResourceAllocationEnable && attr.dra != nil {
		minDRAReq := job.GetMinDRAResources()
		if minDRAReq != nil {
			if !checkDRAAllocatable(attr.dra, minDRAReq, cp.draConsumableCapacityEnable, true) {
				klog.V(3).Infof("job %s exceeds queue %s DRA capability", job.Name, queue.Name)
				return false, append(reasons, "dra-resource-exceeded")
			}
		}
	}

	return true, reasons
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

func getCapacityState(cycleState fwk.CycleState) (*capacityState, error) {
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
	// reservedTaskQueue is a snapshot of the plugin's reserved-task reverse index taken at
	// the same instant as queueAttrs. It is the membership source for candidate exclusion in
	// queueAllocatableWithReserved, so that the reserved AMOUNT (queueAttrs[*].reservedSubtree)
	// and the reserved MEMBERSHIP always come from the same snapshot and cannot drift from
	// the live plugin state mutated later in the session.
	reservedTaskQueue map[api.TaskID]api.QueueID
}

func (qa *queueAttr) Clone() *queueAttr {
	if qa == nil {
		return nil
	}

	cloned := &queueAttr{
		queueID:           qa.queueID,
		name:              qa.name,
		share:             qa.share,
		deserved:          qa.deserved.Clone(),
		allocated:         qa.allocated.Clone(),
		request:           qa.request.Clone(),
		elastic:           qa.elastic.Clone(),
		inqueue:           qa.inqueue.Clone(),
		dra:               qa.dra.Clone(),
		capability:        qa.capability.Clone(),
		realCapability:    qa.realCapability.Clone(),
		guarantee:         qa.guarantee.Clone(),
		resourceClaimRefs: make(map[string]int, len(qa.resourceClaimRefs)),
		children:          make(map[api.QueueID]*queueAttr),
		reservedSubtree:   api.EmptyResource(),
	}
	if qa.reservedSubtree != nil {
		cloned.reservedSubtree = qa.reservedSubtree.Clone()
	}

	if len(qa.ancestors) > 0 {
		cloned.ancestors = make([]api.QueueID, len(qa.ancestors))
		copy(cloned.ancestors, qa.ancestors)
	}

	for childID, childNode := range qa.children {
		cloned.children[childID] = childNode.Clone()
	}
	for claimKey, refCount := range qa.resourceClaimRefs {
		cloned.resourceClaimRefs[claimKey] = refCount
	}

	return cloned
}

func (s *capacityState) Clone() fwk.StateData {
	if s == nil {
		return nil
	}

	newState := &capacityState{
		queueAttrs:        make(map[api.QueueID]*queueAttr, len(s.queueAttrs)),
		reservedTaskQueue: make(map[api.TaskID]api.QueueID, len(s.reservedTaskQueue)),
	}

	for qID, qa := range s.queueAttrs {
		newState.queueAttrs[qID] = qa.Clone()
	}
	for taskID, queueID := range s.reservedTaskQueue {
		newState.reservedTaskQueue[taskID] = queueID
	}

	return newState
}

func updateQueueAttrShare(attr *queueAttr) {
	res := float64(0)

	// If no deserved is configured for this queue, treat it as a best-effort queue.
	// Best-effort queues should have lowest scheduling priority: only when all queues
	// with deserved (i.e., resource guarantee) have share >= 1, best-effort queues
	// are scheduled. To ensure this, set share = 1 for ALL best-effort queues no matter
	// how many resources (allocated or requesting), so that they are always scheduled
	// after any queue with deserved whose share < 1.
	//
	// When share values are equal, QueueOrderFn uses tie-breaking logic to prioritize
	// queues with deserved over best-effort queues. This further prevents reclaim thrashing.
	//
	// The semantics here are:
	//   - no deserved (best-effort) -> share = 1 (always goes after share<1)
	//   - with deserved, share < 1 -> prioritized
	//   - with deserved, share >= 1 -> equal priority with best-effort
	if attr.deserved.IsEmpty() {
		attr.share = 1
		return
	}

	for _, rn := range attr.deserved.ResourceNames() {
		res = max(res, helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn)))
	}

	attr.share = res
}

// shouldSkipReclaimee checks if a reclaimee should be skipped based on whether it has
// intersecting resource dimensions with the reclaimer. Returns true if should skip, with a reason message.
func (cp *capacityPlugin) shouldSkipReclaimee(reclaimee, reclaimer *api.TaskInfo) (bool, string) {
	reclaimerIntersecting := len(api.IntersectionWithIgnoredScalarResources(reclaimee.Resreq, reclaimer.InitResreq)) > 0
	if !reclaimerIntersecting {
		return true, fmt.Sprintf("[capacity] Reclaimee <%s/%s>: <%v> does not have intersecting resource dimensions with reclaimer <%s/%s>: <%v>",
			reclaimee.Namespace, reclaimee.Name, reclaimee.Resreq, reclaimer.Namespace, reclaimer.Name, reclaimer.InitResreq)
	}
	return false, ""
}

// checkGuaranteeConstraint checks if removing the reclaimee would violate the queue's guarantee.
// Returns true if the guarantee constraint is satisfied (i.e., reclaim is allowed).
func (cp *capacityPlugin) checkGuaranteeConstraint(
	allocated *api.Resource,
	reclaimee *api.TaskInfo,
	guarantee *api.Resource,
) (bool, *api.Resource) {
	exceptReclaimee := allocated.Clone().Sub(reclaimee.Resreq)
	reclaimable := guarantee.LessEqual(exceptReclaimee, api.Zero)
	return reclaimable, exceptReclaimee
}

// isImmediateVictim checks if a reclaimee is an immediate victim because it has no
// intersecting resource dimensions with the queue's deserved resources.
// Returns true if it's an immediate victim, with a reason message.
func (cp *capacityPlugin) isImmediateVictim(
	reclaimee *api.TaskInfo,
	deserved *api.Resource,
) (bool, string) {
	if !hasRelevantDeserved(reclaimee, deserved) {
		return true, fmt.Sprintf("[capacity] No intersection between deserved: <%v> and reclaimee <%s/%s>: <%v>",
			deserved, reclaimee.Namespace, reclaimee.Name, reclaimee.Resreq)
	}
	return false, ""
}

func hasRelevantDeserved(task *api.TaskInfo, deserved *api.Resource) bool {
	if task == nil || deserved == nil {
		return false
	}
	return len(api.Intersection(task.Resreq, deserved)) > 0
}

// checkDeservedExceedance checks if the queue's allocated resources exceed its deserved resources
// on dimensions relevant to the reclaimee, making the reclaimee a valid victim.
// Returns true if exceeds, along with the relevant dimensions and a reason message.
func (cp *capacityPlugin) checkDeservedExceedance(
	allocated *api.Resource,
	deserved *api.Resource,
	reclaimee *api.TaskInfo,
	reclaimer *api.TaskInfo,
	queueName string,
) (bool, []string, string) {
	reclaimable, dims := allocated.GreaterPartlyWithRelevantDimensions(deserved, reclaimee.Resreq)
	if !reclaimable {
		reason := fmt.Sprintf(
			"[capacity] Queue <%v> allocated resources are not greater than deserved on any relevant dimension of reclaimee. "+
				"Hence reclaimee <%s/%s> cannot be reclaimed for reclaimer <%s/%s>. "+
				"Deserved: <%v>, Allocated: <%v>, Reclaimee Resreq: <%v>",
			queueName, reclaimee.Namespace, reclaimee.Name, reclaimer.Namespace, reclaimer.Name, deserved, allocated, reclaimee.Resreq,
		)
		return false, nil, reason
	}
	return true, dims, ""
}

func (cp *capacityPlugin) sharesAnyNonRootAncestorWithinLevel(a, b *queueAttr) bool {
	if cp == nil || a == nil || b == nil || cp.ancestorReclaimLevel <= 0 {
		return false
	}

	aAncestors := ancestorIDSet(ancestorsByLevel(a, cp.ancestorReclaimLevel))
	bAncestors := ancestorIDSet(ancestorsByLevel(b, cp.ancestorReclaimLevel))

	for ancestor := range aAncestors {
		if ancestor == rootQueueID {
			continue
		}
		if _, found := bAncestors[ancestor]; found {
			return true
		}
	}

	return false
}

func getQueueLevel(l *queueAttr, r *queueAttr) int {
	level := 0

	for i := range min(len(l.ancestors), len(r.ancestors)) {
		if l.ancestors[i] == r.ancestors[i] {
			level = i
		} else {
			return level
		}
	}

	return level
}

func queueAncestorAtDepth(attr *queueAttr, depth int) (api.QueueID, bool) {
	if attr == nil || depth <= 0 || len(attr.ancestors) < depth {
		return "", false
	}

	return attr.ancestors[len(attr.ancestors)-depth], true
}

func ancestorsByLevel(attr *queueAttr, maxLevel int) map[int]api.QueueID {
	ancestors := make(map[int]api.QueueID)
	if attr == nil || maxLevel <= 0 {
		return ancestors
	}

	for level := 1; level <= maxLevel; level++ {
		ancestorID, found := queueAncestorAtDepth(attr, level)
		if !found {
			continue
		}
		ancestors[level] = ancestorID
	}

	return ancestors
}

func ancestorIDSet(ancestorsByDepth map[int]api.QueueID) map[api.QueueID]struct{} {
	set := make(map[api.QueueID]struct{}, len(ancestorsByDepth))
	for _, ancestorID := range ancestorsByDepth {
		set[ancestorID] = struct{}{}
	}
	return set
}
