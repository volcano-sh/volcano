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

package allocate

import (
	"fmt"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agentscheduler/cache"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	vcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	vfwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	DefaultCandidateNodeCount = 3
)

type Action struct {
	fwk *framework.Framework
	// configured flag for error cache
	enablePredicateErrorCache bool
	candidateNodeCount        int
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true, // default to enable it
		candidateNodeCount:        DefaultCandidateNodeCount,
	}
}

func (alloc *Action) Name() string {
	return "allocate"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) parseArguments(fwk *framework.Framework) {
	arguments := vfwk.GetArgOfActionFromConf(fwk.Configurations, alloc.Name())
	arguments.GetBool(&alloc.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
}

func (alloc *Action) Execute(fwk *framework.Framework, schedCtx *framework.SchedulingContext) {
	klog.V(5).Infof("Enter Allocate ...")
	defer klog.V(5).Infof("Leaving Allocate ...")

	alloc.parseArguments(fwk)

	// the allocation for pod may have many stages
	// 1. use predicateFn to filter out node that T can not be allocated on.
	// 2. use ssn.NodeOrderFn to judge the best node and assign it to T

	alloc.fwk = fwk
	if err := alloc.allocateTask(schedCtx.Task); err != nil {
		alloc.recordTaskFailStatus(schedCtx, err)
		alloc.failureHandler(schedCtx)
	}
}

func (alloc *Action) allocateTask(task *api.TaskInfo) error {
	nodes := alloc.fwk.VolcanoNodeInfos()

	// TODO: check is pod allocatable
	klog.V(3).Infof("There are <%d> nodes for task <%v/%v>", len(nodes), task.Namespace, task.Name)

	if err := alloc.fwk.PrePredicateFn(task); err != nil {
		err = fmt.Errorf("pre-predicate failed for task %s/%s: %w", task.Namespace, task.Name, err)
		klog.ErrorS(err, "PrePredicate failed", "task", klog.KRef(task.Namespace, task.Name))
		return err
	}

	predicatedNodes, err := alloc.predicateFeasibleNodes(task, nodes)
	if err != nil {
		klog.ErrorS(err, "Predicate failed", "task", klog.KRef(task.Namespace, task.Name))
		return err
	}
	bestNodes := alloc.prioritizeNodes(task, predicatedNodes)
	result := &cache.PodScheduleResult{
		SuggestedNodes: bestNodes,
		Task:           task,
		BindContext:    alloc.CreateBindContext(task),
	}
	alloc.EnqueueSchedulerResultForTask(result)

	return nil
}

func (alloc *Action) predicateFeasibleNodes(task *api.TaskInfo, allNodes []*api.NodeInfo) ([]*api.NodeInfo, error) {
	var predicateNodes []*api.NodeInfo
	var fitErrors *api.FitErrors
	ph := util.NewPredicateHelper()

	// "NominatedNodeName" can potentially be set in a previous scheduling cycle as a result of preemption.
	// This node is likely the only candidate that will fit the pod, and hence we try it first before iterating over all nodes.
	if len(task.Pod.Status.NominatedNodeName) > 0 {
		nominatedNodeInfo, err := alloc.fwk.GetVolcanoNodeInfo(task.Pod.Status.NominatedNodeName)
		if err != nil {
			fitErrors = api.NewFitErrors()
			fitErrors.SetNodeError(task.Pod.Status.NominatedNodeName, err)
			klog.ErrorS(fitErrors, "Failed to get nominated node info", "node", task.Pod.Status.NominatedNodeName)
			// Continue to find suitable nodes from all nodes.
		}

		if nominatedNodeInfo != nil {
			predicateNodes, fitErrors = ph.PredicateNodes(task, []*api.NodeInfo{nominatedNodeInfo}, alloc.predicate, alloc.enablePredicateErrorCache)
			if fitErrors != nil {
				klog.ErrorS(fitErrors, "Predicate failed on nominated node", "node", task.Pod.Status.NominatedNodeName)
				// Continue to find suitable nodes from all nodes.
			}
			if len(predicateNodes) > 0 {
				return predicateNodes, nil
			}
		}
	}

	// If the nominated node is not found or the nominated node is not suitable for the task, we need to find a suitable node for the task from other nodes.
	if len(predicateNodes) == 0 {
		predicateNodes, fitErrors = ph.PredicateNodes(task, allNodes, alloc.predicate, alloc.enablePredicateErrorCache)
		if fitErrors != nil {
			return predicateNodes, fitErrors
		}
	}

	return predicateNodes, nil
}

// prioritizeNodes selects the highest score node that idle resource meet task requirement.
func (alloc *Action) prioritizeNodes(task *api.TaskInfo, predicateNodes []*api.NodeInfo) []*api.NodeInfo {
	var idleCandidateNodes []*api.NodeInfo
	for _, n := range predicateNodes {
		if task.InitResreq.LessEqual(n.Idle, api.Zero) {
			idleCandidateNodes = append(idleCandidateNodes, n)
		} else {
			klog.V(5).Infof("Predicate filtered node %v, idle: %v do not meet the requirements of task: %v",
				n.Name, n.Idle, task.Name)
		}
	}

	var bestNodes = []*api.NodeInfo{}
	if klog.V(5).Enabled() {
		for _, node := range idleCandidateNodes {
			klog.V(5).Infof("node %v, idle: %v", node.Name, node.Idle)
		}
	}
	switch {
	case len(idleCandidateNodes) == 0:
		klog.V(5).Infof("Task: %v, no matching node is found in the idleCandidateNodes list.", task.Name)
	case len(idleCandidateNodes) == 1: // If only one node after predicate, just use it.
		bestNodes = append(bestNodes, idleCandidateNodes[0])
	case len(idleCandidateNodes) > 1: // If more than one node after predicate, using "the best" one
		nodeScores := util.PrioritizeNodes(task, idleCandidateNodes, alloc.fwk.BatchNodeOrderFn, alloc.fwk.NodeOrderMapFn, alloc.fwk.NodeOrderReduceFn)

		bestNodes, _ = util.SelectBestNodesAndScores(nodeScores, alloc.candidateNodeCount)
	}
	return bestNodes
}

func (alloc *Action) CreateBindContext(task *api.TaskInfo) *vcache.BindContext {
	bindContext := &vcache.BindContext{
		TaskInfo:   task,
		Extensions: make(map[string]vcache.BindContextExtension),
	}

	for _, plugin := range alloc.fwk.Plugins {
		// If the plugin implements the BindContextHandler interface, call the SetupBindContextExtension method.
		if handler, ok := plugin.(vfwk.BindContextHandler); ok {
			state := alloc.fwk.CurrentCycleState
			handler.SetupBindContextExtension(state, bindContext)
		}
	}

	return bindContext
}

func (alloc *Action) EnqueueSchedulerResultForTask(result *cache.PodScheduleResult) {
	alloc.fwk.Cache.EnqueueScheduleResult(result)
}

func (alloc *Action) recordTaskFailStatus(schedCtx *framework.SchedulingContext, err error) {
	taskInfo := schedCtx.Task
	// The pod of a scheduling gated task is given
	// the ScheduleGated condition by the api-server. Do not change it.
	if taskInfo.SchGated {
		return
	}
	reason := api.PodReasonUnschedulable
	var msg string
	if fitErrors, ok := err.(*api.FitErrors); ok {
		msg = fitErrors.Error()
	} else {
		msg = err.Error()
	}

	if len(msg) == 0 {
		msg = api.AllNodeUnavailableMsg
	}
	if err := alloc.fwk.Cache.TaskUnschedulable(taskInfo, reason, msg); err != nil {
		klog.ErrorS(err, "Failed to update unschedulable task status", "task", klog.KRef(taskInfo.Namespace, taskInfo.Name),
			"reason", reason, "message", msg)
	}
}

func (alloc *Action) failureHandler(schedCtx *framework.SchedulingContext) {
	schedulingQueue := alloc.fwk.Cache.SchedulingQueue() // schedulingQueue will not be nil since we already checked in generateSchedulingContext before
	if err := schedulingQueue.AddUnschedulableIfNotPresent(klog.Background(), schedCtx.QueuedPodInfo, schedulingQueue.SchedulingCycle()); err != nil {
		klog.ErrorS(err, "Failed to add pod back to scheduling queue", "pod", klog.KObj(schedCtx.QueuedPodInfo.Pod))
	}
}

func (alloc *Action) predicate(task *api.TaskInfo, node *api.NodeInfo) error {
	// Check for Resource Predicate
	var statusSets api.StatusSets
	if ok, resources := task.InitResreq.LessEqualWithResourcesName(node.Idle, api.Zero); !ok {
		statusSets = append(statusSets, &api.Status{Code: api.Unschedulable, Reason: api.WrapInsufficientResourceReason(resources)})
		return api.NewFitErrWithStatus(task, node, statusSets...)
	}
	return alloc.PredicateForAllocateAction(task, node)
}

func (alloc *Action) PredicateForAllocateAction(task *api.TaskInfo, node *api.NodeInfo) error {
	err := alloc.fwk.PredicateFn(task, node)
	if err == nil {
		return nil
	}

	fitError, ok := err.(*api.FitError)
	if !ok {
		return api.NewFitError(task, node, err.Error())
	}

	statusSets := fitError.Status
	if statusSets.ContainsUnschedulable() || statusSets.ContainsUnschedulableAndUnresolvable() ||
		statusSets.ContainsErrorSkipOrWait() {
		return fitError
	}
	return nil
}

func (alloc *Action) UnInitialize() {}
