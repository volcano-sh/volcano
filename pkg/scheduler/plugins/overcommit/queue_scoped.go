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

package overcommit

import (
	"fmt"
	"math"
	"strconv"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const rootQueueName = "root"

type queueAdmissionState struct {
	queueID api.QueueID
	name    string

	ancestors []api.QueueID
	children  []api.QueueID

	capability     *api.Resource
	guarantee      *api.Resource
	realCapability *api.Resource
	deserved       *api.Resource
	budget         *api.Resource
	allocated      *api.Resource
	inqueue        *api.Resource

	annotated bool
	valid     bool
}

// buildQueueAdmissionStates initializes per-queue admission state for the scheduling session.
func (op *overcommitPlugin) buildQueueAdmissionStates(ssn *framework.Session) {
	op.queueStates = make(map[api.QueueID]*queueAdmissionState, len(ssn.Queues))
	for queueID, queue := range ssn.Queues {
		state := &queueAdmissionState{
			queueID:   queueID,
			name:      queue.Name,
			guarantee: api.NewResource(queue.Queue.Spec.Guarantee.Resource),
			allocated: api.EmptyResource(),
			inqueue:   api.EmptyResource(),
			valid:     true,
		}
		if len(queue.Queue.Spec.Capability) != 0 {
			state.capability = api.NewResource(queue.Queue.Spec.Capability)
		}
		op.queueStates[queueID] = state
	}

	if op.hierarchyEnabled {
		op.buildQueueHierarchy(ssn)
		op.buildHierarchicalQueueBudgets()
	} else {
		op.buildFlatQueueBudgets()
	}

	for queueID, queue := range ssn.Queues {
		state := op.queueStates[queueID]
		factor, annotated, err := queueOvercommitFactor(queue)
		if err != nil {
			state.annotated = true
			state.valid = false
			continue
		}
		if !annotated {
			continue
		}
		state.annotated = true
		factor = math.Min(factor, op.maxQueueOverCommitFactor)
		state.deserved = effectiveDeserved(queue.Queue)
		state.budget = state.deserved.Clone().Multi(factor)
		if state.realCapability != nil {
			state.budget.MinDimensionResource(state.realCapability, api.Infinity)
		}
	}
}

// buildQueueHierarchy records parent-child relationships for hierarchical queues.
func (op *overcommitPlugin) buildQueueHierarchy(ssn *framework.Session) {
	for queueID, queue := range ssn.Queues {
		if queue.Name == rootQueueName {
			continue
		}

		parentName := queue.Queue.Spec.Parent
		if parentName == "" {
			parentName = rootQueueName
		}
		parentID := api.QueueID(parentName)
		parentState, found := op.queueStates[parentID]
		if !found {
			op.queueStates[queueID].valid = false
			continue
		}
		parentState.children = append(parentState.children, queueID)
	}

	rootState := op.queueStates[api.QueueID(rootQueueName)]
	if rootState == nil {
		return
	}
	op.populateQueueAncestors(rootState, nil)
}

// populateQueueAncestors assigns each queue the ordered path to its ancestors.
func (op *overcommitPlugin) populateQueueAncestors(parent *queueAdmissionState, ancestors []api.QueueID) {
	for _, childID := range parent.children {
		child := op.queueStates[childID]
		child.ancestors = append(append([]api.QueueID{}, ancestors...), parent.queueID)
		op.populateQueueAncestors(child, child.ancestors)
	}
}

// buildFlatQueueBudgets calculates real capabilities when queue hierarchy is disabled.
func (op *overcommitPlugin) buildFlatQueueBudgets() {
	totalGuarantee := api.EmptyResource()
	for _, state := range op.queueStates {
		totalGuarantee.Add(state.guarantee)
	}

	for _, state := range op.queueStates {
		capability := state.capability
		if capability != nil {
			capability = standaloneCapability(capability)
		}
		state.realCapability = util.CalculateQueueRealCapability(op.totalResource, totalGuarantee, capability, state.guarantee)
	}
}

// buildHierarchicalQueueBudgets calculates real capabilities for a hierarchical queue tree.
func (op *overcommitPlugin) buildHierarchicalQueueBudgets() {
	rootState := op.queueStates[api.QueueID(rootQueueName)]
	if rootState == nil {
		for _, state := range op.queueStates {
			state.valid = false
		}
		return
	}

	if rootState.capability == nil || rootState.capability.IsEmpty() {
		rootState.capability = infiniteResource(op.totalResource)
	}
	rootState.realCapability = rootState.capability.Clone()
	op.buildChildQueueBudgets(rootState)
}

// buildChildQueueBudgets derives child queue capabilities from their parent state.
func (op *overcommitPlugin) buildChildQueueBudgets(parent *queueAdmissionState) {
	totalGuarantee := api.EmptyResource()
	for _, childID := range parent.children {
		totalGuarantee.Add(op.queueStates[childID].guarantee)
	}

	for _, childID := range parent.children {
		child := op.queueStates[childID]
		child.capability = inheritedCapability(parent.capability, child.capability)
		child.realCapability = util.CalculateQueueRealCapability(parent.realCapability, totalGuarantee, child.capability, child.guarantee)
		op.buildChildQueueBudgets(child)
	}
}

// addExistingJobToQueueStates accounts for an existing job's allocated and inqueue resources.
func (op *overcommitPlugin) addExistingJobToQueueStates(job *api.JobInfo) {
	if len(op.queueStates) == 0 || job.PodGroup == nil {
		return
	}
	state := op.queueStates[job.Queue]
	if state == nil {
		return
	}

	allocated := api.EmptyResource()
	for status, tasks := range job.TaskStatusIndex {
		if !api.AllocatedStatus(status) {
			continue
		}
		for _, task := range tasks {
			allocated.Add(task.Resreq)
		}
	}
	op.addQueueAllocatedResource(job.Queue, allocated)

	if job.PodGroup.Spec.MinResources == nil {
		return
	}

	var inqueue *api.Resource
	switch job.PodGroup.Status.Phase {
	case scheduling.PodGroupInqueue:
		inqueue = util.GetInqueueResource(job, job.Allocated)
	case scheduling.PodGroupRunning:
		if int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueue = util.GetInqueueResource(job, job.Allocated)
		}
	}
	if inqueue != nil {
		op.addQueueInqueueResource(job, job.DeductSchGatedResources(inqueue))
	}
}

// addQueueAllocatedResource adds allocated resources to a queue and all of its ancestors.
func (op *overcommitPlugin) addQueueAllocatedResource(queueID api.QueueID, resource *api.Resource) {
	if state := op.queueStates[queueID]; state != nil {
		state.allocated.Add(resource)
		for _, ancestorID := range state.ancestors {
			op.queueStates[ancestorID].allocated.Add(resource)
		}
	}
}

// addQueueInqueueResource adds inqueue resources to a job's queue and all of its ancestors.
func (op *overcommitPlugin) addQueueInqueueResource(job *api.JobInfo, resource *api.Resource) {
	if state := op.queueStates[job.Queue]; state != nil {
		state.inqueue.Add(resource)
		for _, ancestorID := range state.ancestors {
			op.queueStates[ancestorID].inqueue.Add(resource)
		}
	}
}

// jobQueueEnqueueable verifies that a job fits every annotated queue on its ancestor path.
func (op *overcommitPlugin) jobQueueEnqueueable(job *api.JobInfo) (string, []string, bool) {
	state := op.queueStates[job.Queue]
	if state == nil {
		return string(job.Queue), nil, false
	}

	queueIDs := append(append([]api.QueueID{}, state.ancestors...), job.Queue)
	for index := len(queueIDs) - 1; index >= 0; index-- {
		queueState := op.queueStates[queueIDs[index]]
		if queueState == nil {
			return string(queueIDs[index]), nil, false
		}
		if !queueState.annotated {
			continue
		}
		if !queueState.valid || queueState.budget == nil {
			return queueState.name, nil, false
		}

		request := queueScopedRequest(job.GetMinResources(), queueState.deserved)
		used := queueState.allocated.Clone().Add(queueState.inqueue).Add(request)
		if permitted, resourceNames := used.LessEqualWithDimensionAndResourcesName(queueState.budget, request); !permitted {
			return queueState.name, resourceNames, false
		}
	}
	return "", nil, true
}

// queueOvercommitFactor parses the optional queue overcommit-factor annotation.
func queueOvercommitFactor(queue *api.QueueInfo) (float64, bool, error) {
	value, found := queue.Queue.Annotations[schedulingv1beta1.QueueOvercommitFactorAnnotationKey]
	if !found {
		return 0, false, nil
	}
	factor, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(factor) || math.IsInf(factor, 0) || factor < 1 {
		return 0, true, fmt.Errorf("invalid queue overcommit factor %q", value)
	}
	return factor, true, nil
}

// effectiveDeserved returns the static deserved resources raised to matching guarantees.
func effectiveDeserved(queue *scheduling.Queue) *api.Resource {
	resources := v1.ResourceList{}
	for resourceName, deserved := range queue.Spec.Deserved {
		if deserved.Sign() <= 0 {
			continue
		}
		effective := deserved.DeepCopy()
		if guarantee, found := queue.Spec.Guarantee.Resource[resourceName]; found && guarantee.Cmp(effective) > 0 {
			effective = guarantee.DeepCopy()
		}
		resources[resourceName] = effective
	}
	return api.NewResource(resources)
}

// queueScopedRequest keeps only requested resource dimensions managed by queue overcommit.
func queueScopedRequest(request, deserved *api.Resource) *api.Resource {
	filtered := api.EmptyResource()
	if deserved.MilliCPU > 0 {
		filtered.MilliCPU = request.MilliCPU
	}
	if deserved.Memory > 0 {
		filtered.Memory = request.Memory
	}
	for resourceName, quantity := range deserved.ScalarResources {
		if quantity > 0 {
			filtered.SetScalar(resourceName, request.Get(resourceName))
		}
	}
	return filtered
}

// standaloneCapability fills omitted standalone capability dimensions with no limit.
func standaloneCapability(capability *api.Resource) *api.Resource {
	result := capability.Clone()
	if result.MilliCPU <= 0 {
		result.MilliCPU = math.MaxFloat64
	}
	if result.Memory <= 0 {
		result.Memory = math.MaxFloat64
	}
	return result
}

// inheritedCapability fills omitted child capability dimensions from its parent capability.
func inheritedCapability(parentCapability *api.Resource, capability *api.Resource) *api.Resource {
	if capability == nil {
		return parentCapability.Clone()
	}
	result := capability.Clone()
	if result.MilliCPU <= 0 {
		result.MilliCPU = parentCapability.MilliCPU
	}
	if result.Memory <= 0 {
		result.Memory = parentCapability.Memory
	}
	for resourceName, quantity := range parentCapability.ScalarResources {
		if _, found := result.ScalarResources[resourceName]; !found {
			result.SetScalar(resourceName, quantity)
		}
	}
	return result
}

// infiniteResource returns an unbounded resource with all cluster scalar dimensions included.
func infiniteResource(total *api.Resource) *api.Resource {
	result := api.InfiniteResource()
	for resourceName := range total.ScalarResources {
		result.SetScalar(resourceName, float64(math.MaxInt64))
	}
	return result
}
