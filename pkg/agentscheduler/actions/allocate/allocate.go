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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	vcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	vfwk "volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	DefaultCandidateNodeCount = 3
	CandidateNodeCountKey     = "candidateNodeCount"
)

type Action struct {
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

// OnActionInit initializes the plugin. It is called once when the framework is created.
func (alloc *Action) OnActionInit(configurations []conf.Configuration) {
	alloc.parseArguments(configurations)
}

func (alloc *Action) Initialize() {}

func (alloc *Action) parseArguments(configurations []conf.Configuration) {
	arguments := vfwk.GetArgOfActionFromConf(configurations, alloc.Name())
	arguments.GetBool(&alloc.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
	arguments.GetInt(&alloc.candidateNodeCount, CandidateNodeCountKey)
}

func (alloc *Action) Execute(fwk *framework.Framework, schedCtx *agentapi.SchedulingContext) {
	klog.V(5).Infof("Enter Allocate ...")
	defer klog.V(5).Infof("Leaving Allocate ...")

	// the allocation for pod may have many stages
	// 1. use predicateFn to filter out node that T can not be allocated on.
	// 2. use ssn.NodeOrderFn to judge the best node and assign it to T

	if err := alloc.allocateTask(fwk, schedCtx); err != nil {
		alloc.recordTaskFailStatus(fwk, schedCtx, err)
		alloc.failureHandler(fwk, schedCtx)
	}
}

func (alloc *Action) allocateTask(fwk *framework.Framework, schedCtx *agentapi.SchedulingContext) error {
	task := schedCtx.Task

	nodes := fwk.VolcanoNodeInfos()

	// TODO: check is pod allocatable
	klog.V(3).Infof("There are <%d> nodes for task <%v/%v>", len(nodes), task.Namespace, task.Name)

	if err := fwk.PrePredicateFn(task); err != nil {
		err = fmt.Errorf("pre-predicate failed for task %s/%s: %w", task.Namespace, task.Name, err)
		klog.ErrorS(err, "PrePredicate failed", "task", klog.KRef(task.Namespace, task.Name))
		return err
	}

	predicatedNodes, err := alloc.predicateFeasibleNodes(fwk, task, nodes, schedCtx.NodesInShard)
	if len(predicatedNodes) == 0 {
		klog.ErrorS(err, "Predicate failed", "task", klog.KRef(task.Namespace, task.Name))
		if err == nil {
			return fmt.Errorf(api.AllNodeUnavailableMsg)
		}
		return err
	}
	bestNodes := alloc.prioritizeNodes(fwk, task, predicatedNodes, schedCtx.NodesInShard)
	result := &agentapi.PodScheduleResult{
		SuggestedNodes: bestNodes,
		SchedCtx:       schedCtx,
		BindContext:    alloc.CreateBindContext(fwk, schedCtx),
	}
	alloc.SendResultToBinder(fwk, result)

	return nil
}

func (alloc *Action) predicateFeasibleNodes(fwk *framework.Framework, task *api.TaskInfo, allNodes []*api.NodeInfo, nodesInShard sets.Set[string]) ([]*api.NodeInfo, *api.FitErrors) {
	var predicateNodes []*api.NodeInfo
	var fitErrors *api.FitErrors
	ph := util.NewPredicateHelper()

	// Create a predicate closure that captures fwk for this scheduling cycle.
	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		return alloc.predicate(fwk, task, node)
	}

	// This node is likely the only candidate that will fit the pod, and hence we try it first before iterating over all nodes.
	if len(task.Pod.Status.NominatedNodeName) > 0 {
		nominatedNodeInfo, err := fwk.GetVolcanoNodeInfo(task.Pod.Status.NominatedNodeName)
		if err != nil {
			fitErrors = api.NewFitErrors()
			fitErrors.SetNodeError(task.Pod.Status.NominatedNodeName, err)
			klog.ErrorS(fitErrors, "Failed to get nominated node info", "node", task.Pod.Status.NominatedNodeName)
			// Continue to find suitable nodes from all nodes.
		}

		if nominatedNodeInfo != nil {
			predicateNodes, fitErrors = ph.PredicateNodes(task, []*api.NodeInfo{nominatedNodeInfo}, predicateFn, alloc.enablePredicateErrorCache, nodesInShard)
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
		predicateNodes, fitErrors = ph.PredicateNodes(task, allNodes, predicateFn, alloc.enablePredicateErrorCache, nodesInShard)
		if fitErrors != nil {
			return predicateNodes, fitErrors
		}
	}

	return predicateNodes, nil
}

// prioritizeNodes selects the highest score node that idle resource meet task requirement.
func (alloc *Action) prioritizeNodes(fwk *framework.Framework, task *api.TaskInfo, predicateNodes []*api.NodeInfo, nodesInShard sets.Set[string]) []*api.NodeInfo {
	var candidateNodes [2][]*api.NodeInfo
	var candidateNodesInShard []*api.NodeInfo
	var candidateNodesInOtherShards []*api.NodeInfo
	shardingMode := options.ServerOpts.ShardingMode
	for _, n := range predicateNodes {
		if ok, _ := task.InitResreq.LessEqual(n.Idle, api.Zero); ok {
			switch shardingMode {
			case commonutil.NoneShardingMode, commonutil.HardShardingMode:
				candidateNodesInShard = append(candidateNodesInShard, n) //in hardmode, all predicates nodes are in shard
			case commonutil.SoftShardingMode:
				if nodesInShard.Has(n.Name) {
					candidateNodesInShard = append(candidateNodesInShard, n)
				} else {
					candidateNodesInOtherShards = append(candidateNodesInOtherShards, n)
				}
			}
		} else {
			klog.V(5).Infof("Predicate filtered node %v, idle: %v do not meet the requirements of task: %v",
				n.Name, n.Idle, task.Name)
		}
	}
	candidateNodes[0] = candidateNodesInShard
	candidateNodes[1] = candidateNodesInOtherShards

	var bestNodes = []*api.NodeInfo{}
	for index, nodes := range candidateNodes {
		if index > 0 && shardingMode != commonutil.SoftShardingMode {
			//only SoftShardingMode need check nodes in other shard
			break
		}
		if klog.V(5).Enabled() {
			for _, node := range nodes {
				klog.V(5).Infof("node %v, idle: %v", node.Name, node.Idle)
			}
		}
		switch {
		case len(nodes) == 0:
			klog.V(5).Infof("Task: %v, no matching node is found in the idleCandidateNodes list.", task.Name)
		case len(nodes) == 1: // If only one node after predicate, just use it.
			bestNodes = append(bestNodes, nodes[0])
		case len(nodes) > 1: // If more than one node after predicate, using "the best" one
			nodeScores := util.PrioritizeNodes(task, nodes, fwk.BatchNodeOrderFn, fwk.NodeOrderMapFn, fwk.NodeOrderReduceFn)
			bestNodes, _ = util.SelectBestNodesAndScores(nodeScores, alloc.candidateNodeCount)
		}
		if len(bestNodes) > 0 {
			break
		}
	}
	return bestNodes
}

func (alloc *Action) CreateBindContext(fwk *framework.Framework, schedCtx *agentapi.SchedulingContext) *agentapi.BindContext {
	bindContext := &agentapi.BindContext{
		SchedCtx:   schedCtx,
		Extensions: make(map[string]vcache.BindContextExtension),
	}

	for _, plugin := range fwk.Plugins {
		// If the plugin implements the BindContextHandler interface, call the SetupBindContextExtension method.
		if handler, ok := plugin.(framework.BindContextHandler); ok {
			state := fwk.CurrentCycleState
			handler.SetupBindContextExtension(state, bindContext)
		}
	}

	return bindContext
}

func (alloc *Action) SendResultToBinder(fwk *framework.Framework, result *agentapi.PodScheduleResult) {
	fwk.Cache.EnqueueScheduleResult(result)
}

func (alloc *Action) recordTaskFailStatus(fwk *framework.Framework, schedCtx *agentapi.SchedulingContext, err error) {
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
	if err := fwk.Cache.TaskUnschedulable(taskInfo, reason, msg); err != nil {
		klog.ErrorS(err, "Failed to update unschedulable task status", "task", klog.KRef(taskInfo.Namespace, taskInfo.Name),
			"reason", reason, "message", msg)
	}
}

func (alloc *Action) failureHandler(fwk *framework.Framework, schedCtx *agentapi.SchedulingContext) {
	schedulingQueue := fwk.Cache.SchedulingQueue() // schedulingQueue will not be nil since we already checked in generateSchedulingContext before
	if err := schedulingQueue.AddUnschedulableIfNotPresent(klog.Background(), schedCtx.QueuedPodInfo, schedulingQueue.SchedulingCycle()); err != nil {
		klog.ErrorS(err, "Failed to add pod back to scheduling queue", "pod", klog.KObj(schedCtx.QueuedPodInfo.Pod))
	}
}

func (alloc *Action) predicate(fwk *framework.Framework, task *api.TaskInfo, node *api.NodeInfo) error {
	// Check for Resource Predicate
	var statusSets api.StatusSets
	if ok, resources := task.InitResreq.LessEqual(node.Idle, api.Zero); !ok {
		statusSets = append(statusSets, &api.Status{Code: api.Unschedulable, Reason: api.WrapInsufficientResourceReason(resources)})
		return api.NewFitErrWithStatus(task, node, statusSets...)
	}
	return alloc.predicateForAllocateAction(fwk, task, node)
}

func (alloc *Action) predicateForAllocateAction(fwk *framework.Framework, task *api.TaskInfo, node *api.NodeInfo) error {
	err := fwk.PredicateFn(task, node)
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
