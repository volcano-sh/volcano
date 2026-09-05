/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Added topology-aware preemption
- Enhanced with predicate error caching and BestEffort constraints
- Added victim selection algorithms with scoring and ordering

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

package preempt

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	EnableTopologyAwarePreemptionKey = "enableTopologyAwarePreemption"

	TopologyAwarePreemptWorkerNumKey = "topologyAwarePreemptWorkerNum"
)

type Action struct {
	ssn *framework.Session

	enablePredicateErrorCache bool

	enableTopologyAwarePreemption bool

	topologyAwarePreemptWorkerNum int
	minCandidateNodesPercentage   int
	minCandidateNodesAbsolute     int
	maxCandidateNodesAbsolute     int
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache:     true,
		enableTopologyAwarePreemption: false,
		topologyAwarePreemptWorkerNum: 16,
		minCandidateNodesPercentage:   10,
		minCandidateNodesAbsolute:     1,
		maxCandidateNodesAbsolute:     100,
	}
}

func (pmpt *Action) Name() string {
	return "preempt"
}

func (pmpt *Action) Initialize() {}

func (pmpt *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, pmpt.Name())
	arguments.GetBool(&pmpt.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
	arguments.GetBool(&pmpt.enableTopologyAwarePreemption, EnableTopologyAwarePreemptionKey)
	arguments.GetInt(&pmpt.topologyAwarePreemptWorkerNum, TopologyAwarePreemptWorkerNumKey)
	arguments.GetInt(&pmpt.minCandidateNodesPercentage, utils.MinCandidateNodesPercentageKey)
	arguments.GetInt(&pmpt.minCandidateNodesAbsolute, utils.MinCandidateNodesAbsoluteKey)
	arguments.GetInt(&pmpt.maxCandidateNodesAbsolute, utils.MaxCandidateNodesAbsoluteKey)
	pmpt.ssn = ssn
}

func (pmpt *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Preempt ...")
	defer klog.V(5).Infof("Leaving Preempt ...")

	pmpt.parseArguments(ssn)

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	underRequestByQueue := map[api.QueueID][]*api.JobInfo{}

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip preemption, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if _, found := ssn.Queues[job.Queue]; !found {
			klog.V(3).Infof("Queue <%s> not found for Job <%s/%s>, skip preemption", job.Queue, job.Namespace, job.Name)
			continue
		}

		// check job if starving for more resources.
		if !ssn.JobStarving(job) {
			continue
		}

		// TODO: Currently, jobs containing networkTopology do not support preemption. Related issue: https://github.com/volcano-sh/volcano/issues/4374
		if job.ContainsNetworkTopology() {
			klog.V(3).Infof("Job <%s/%s> Queue <%s> skip preemption, reason: jobs containing networkTopology do not support preemption",
				job.Namespace, job.Name, job.Queue)
			continue
		}

		if _, found := preemptorsMap[job.Queue]; !found {
			preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
		}
		preemptorsMap[job.Queue].Push(job)
		underRequestByQueue[job.Queue] = append(underRequestByQueue[job.Queue], job)
		preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
		for _, task := range job.TaskStatusIndex[api.Pending] {
			if task.SchGated {
				continue
			}
			preemptorTasks[job.UID].Push(task)
		}
	}

	// If plugin defines queue order function, use it to order queues.
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	for queueID := range preemptorsMap {
		if queue, found := ssn.Queues[queueID]; found {
			queues.Push(queue)
		}
	}

	ph := util.NewPredicateHelper()
	// Preemption between Jobs within Queue.
	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)
		for {
			preemptors := preemptorsMap[queue.UID]

			// If no preemptors, no preemption.
			if preemptors == nil || preemptors.Empty() {
				klog.V(4).Infof("No preemptors in Queue <%s>, break.", queue.Name)
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)

			stmt := framework.NewStatement(ssn)
			for {
				// If job is not request more resource, then stop preempting.
				if !ssn.JobStarving(preemptorJob) {
					break
				}

				// If not preemptor tasks, next job.
				if preemptorTasks[preemptorJob.UID].Empty() {
					klog.V(3).Infof("No preemptor task in job <%s/%s>.",
						preemptorJob.Namespace, preemptorJob.Name)
					break
				}

				preemptor := preemptorTasks[preemptorJob.UID].Pop().(*api.TaskInfo)

				_, err := pmpt.preempt(ssn, stmt, preemptor, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if !api.PreemptableStatus(task.Status) {
						return false
					}
					// BestEffort pod is not supported to preempt unBestEffort pod.
					if preemptor.BestEffort && !task.BestEffort {
						return false
					}
					if !task.Preemptable {
						return false
					}
					job, found := ssn.Jobs[task.Job]
					if !found {
						return false
					}
					// Preempt other jobs within queue
					return job.Queue == preemptorJob.Queue && preemptor.Job != task.Job
				}, ph)
				if err != nil {
					klog.V(3).Infof("Preemptor <%s/%s> failed to preempt Task , err: %s", preemptor.Namespace, preemptor.Name, err)
				}
			}

			// Commit changes only if job is pipelined, otherwise try next job.
			if ssn.JobPipelined(preemptorJob) {
				hasEvictions := stmt.HasEvictions()
				stmt.Commit()
				if hasEvictions {
					metrics.RegisterEvictionTransaction(pmpt.Name())
				}
			} else {
				stmt.Discard()
				continue
			}
		}

		// Preemption between Task within Job.
		for _, job := range underRequestByQueue[queue.UID] {
			// Here we need to use a scoped intraJob priority queue instead of overwriting preemptorTasks[job.UID].
			// The original preemptorTasks map is populated during job discovery (lines above)
			// and consumed by the "Preemption between Jobs within Queue" loop.
			// Overwriting it here causes preemptors from other queues' starving jobs to be
			// lost due to non-deterministic Go map iteration order in multi-queue scenarios.
			intraJobPreemptors := util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				// Again, skip scheduling gated tasks
				if task.SchGated {
					continue
				}
				intraJobPreemptors.Push(task)
			}
			for {
				if intraJobPreemptors.Empty() {
					break
				}

				preemptor := intraJobPreemptors.Pop().(*api.TaskInfo)

				stmt := framework.NewStatement(ssn)
				assigned, err := pmpt.preempt(ssn, stmt, preemptor, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if !api.PreemptableStatus(task.Status) {
						return false
					}
					// BestEffort pod is not supported to preempt unBestEffort pod.
					if preemptor.BestEffort && !task.BestEffort {
						return false
					}
					// should skip not preemptable pod
					if !task.Preemptable {
						return false
					}

					// Preempt tasks within job.
					return preemptor.Job == task.Job
				}, ph)
				if err != nil {
					klog.V(3).Infof("Preemptor <%s/%s> failed to preempt Task , err: %s", preemptor.Namespace, preemptor.Name, err)
				}

				// Only commit if preemption was successful, otherwise discard to rollback evictions.
				// This is consistent with between-job preemption which checks JobPipelined before committing.
				if !assigned {
					stmt.Discard()
					break
				}
				hasEvictions := stmt.HasEvictions()
				stmt.Commit()
				if hasEvictions {
					metrics.RegisterEvictionTransaction(pmpt.Name())
				}
			}
		}
	}
}

func (pmpt *Action) UnInitialize() {}

func (pmpt *Action) preempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	filter func(*api.TaskInfo) bool,
	predicateHelper util.PredicateHelper,
) (bool, error) {
	// Check whether the task is eligible to preempt others, e.g., check preemptionPolicy is `Never` or not
	if err := pmpt.taskEligibleToPreempt(preemptor); err != nil {
		return false, err
	}

	if err := ssn.PrePredicateFn(preemptor); err != nil {
		return false, fmt.Errorf("PrePredicate for task %s/%s failed for: %v", preemptor.Namespace, preemptor.Name, err)
	}

	// we should filter out those nodes that are UnschedulableAndUnresolvable status got in allocate action
	allNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(preemptor)
	predicateNodes, _ := predicateHelper.PredicateNodes(preemptor, allNodes, ssn.PredicateForPreemptAction, pmpt.enablePredicateErrorCache, ssn.NodesInShard)

	candidateNodes := util.GetPredicatedNodeByShard(predicateNodes, ssn.NodesInShard)
	var preemptSuccess bool
	var err error
	//try to preempt in order if multiple candidate Nodes group with priority exist
	for _, nodes := range candidateNodes {
		if pmpt.enableTopologyAwarePreemption {
			if preemptSuccess, err = pmpt.topologyAwarePreempt(ssn, stmt, preemptor, filter, nodes); preemptSuccess {
				break
			}
		} else if preemptSuccess, err = pmpt.normalPreempt(ssn, stmt, preemptor, filter, nodes); preemptSuccess {
			break
		}
	}
	return preemptSuccess, err
}

func (pmpt *Action) normalPreempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	filter func(*api.TaskInfo) bool,
	predicateNodes []*api.NodeInfo,
) (bool, error) {
	nodeScores := util.PrioritizeNodes(preemptor, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

	selectedNodes := util.SortNodes(nodeScores)

	job, found := ssn.Jobs[preemptor.Job]
	if !found {
		return false, fmt.Errorf("not found Job %s in Session", preemptor.Job)
	}

	currentQueue := ssn.Queues[job.Queue]

	assigned := false

	for _, node := range selectedNodes {
		klog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.",
			preemptor.Namespace, preemptor.Name, node.Name)

		var preemptees []*api.TaskInfo
		for _, task := range node.Tasks {
			if filter == nil {
				preemptees = append(preemptees, task.Clone())
			} else if filter(task) {
				preemptees = append(preemptees, task.Clone())
			}
		}
		victims := ssn.Preemptable(preemptor, preemptees)
		metrics.UpdatePreemptionVictimsCount(len(victims))

		if err := util.ValidateVictims(preemptor, node, victims); err != nil {
			klog.V(3).Infof("No validated victims on Node <%s>: %v", node.Name, err)
			continue
		}

		// Use a temporary statement per node attempt so that eviction operations
		// are isolated. On success the operations are merged into the caller's
		// statement; on failure they are discarded, so evictions are only committed
		// when preemption succeeds.
		nodeStmt := framework.NewStatement(ssn)

		victimsQueue := ssn.BuildVictimsPriorityQueue(victims, preemptor)
		// Preempt victims for tasks, pick lowest priority task first.
		preempted := api.EmptyResource()
		preemptorFits := preemptorFitsOnNode(ssn, currentQueue, preemptor, node)

		for !victimsQueue.Empty() {
			if preemptorFits {
				break
			}
			preemptee := victimsQueue.Pop().(*api.TaskInfo)
			klog.V(3).Infof("Try to preempt Task <%s/%s> for Task <%s/%s>",
				preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name)
			nodeStmt.Evict(preemptee, "preempt")
			preempted.Add(preemptee.Resreq)
			preemptorFits = preemptorFitsOnNode(ssn, currentQueue, preemptor, node)
		}

		evictionOccurred := false
		if !preempted.IsEmpty() {
			evictionOccurred = true
		}

		metrics.RegisterPreemptionAttempts()
		klog.V(3).Infof("Preempted <%v> for Task <%s/%s> requested <%v>.",
			preempted, preemptor.Namespace, preemptor.Name, preemptor.InitResreq)

		// If preemptor's queue is not allocatable, it means preemptor cannot be allocated. So no need care about the node idle resource
		if preemptorFits {
			if err := nodeStmt.Pipeline(preemptor, node.Name, evictionOccurred); err != nil {
				klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
					preemptor.Namespace, preemptor.Name, node.Name)
				// Pipeline failed: discard all evictions for this node and try the next one.
				nodeStmt.Discard()
				continue
			}

			// Pipeline succeeded: merge this node's operations into the caller's statement.
			stmt.Merge(nodeStmt)
			assigned = true
			break
		}

		// Not enough resources on this node even after evictions: discard and try next node.
		nodeStmt.Discard()
	}

	return assigned, nil
}

// preemptorFitsOnNode checks whether the preemptor fits the node after tentative evictions.
// A job may not be allocatable because:
// 1. The cluster has free resources, but the queue is not allocatable.
// 2. The cluster has no free resources, and the queue is not allocatable.
// 3. The cluster has no free resources, but the queue is allocatable.
// 4. The node has sufficient aggregate resources, but PredicateFn fails because vNPU or vGPU resources are fragmented.
// Same-queue preemption handles cases 1 and 2. Reclaim handles case 3. For case 4, normal preemption continues until
// the predicate passes after enough victims are evicted.
func preemptorFitsOnNode(ssn *framework.Session, queue *api.QueueInfo, preemptor *api.TaskInfo, node *api.NodeInfo) bool {
	return ssn.Allocatable(queue, preemptor) &&
		preemptor.InitResreq.LessEqual(node.FutureIdle(), api.Zero) &&
		ssn.PredicateFn(preemptor, node) == nil
}

func (pmpt *Action) taskEligibleToPreempt(preemptor *api.TaskInfo) error {
	if preemptor.Pod.Spec.PreemptionPolicy != nil && *preemptor.Pod.Spec.PreemptionPolicy == v1.PreemptNever {
		return fmt.Errorf("not eligible to preempt other tasks due to preemptionPolicy is Never")
	}

	nomNodeName := preemptor.Pod.Status.NominatedNodeName
	if len(nomNodeName) > 0 {
		nodeInfo, ok := pmpt.ssn.Nodes[nomNodeName]
		if !ok {
			return fmt.Errorf("not eligible due to the pod's nominated node is not found in the session")
		}

		err := pmpt.ssn.PredicateFn(preemptor, nodeInfo)
		if err == nil {
			return fmt.Errorf("not eligible due to the pod's nominated node is already schedulable, which should not happen as preemption means no node is schedulable")
		}

		fitError, ok := err.(*api.FitError)
		if !ok {
			return fmt.Errorf("not eligible due to the predicate returned a non-FitError error, the error is: %v", err)
		}

		// If the pod's nominated node is considered as UnschedulableAndUnresolvable by the predicate,
		// then the pod should be considered for preempting again.
		if fitError.Status.ContainsUnschedulableAndUnresolvable() {
			return nil
		}

		preemptorPodPriority := PodPriority(preemptor.Pod)
		for _, p := range nodeInfo.Pods() {
			if PodPriority(p) < preemptorPodPriority && podTerminatingByPreemption(p) {
				// There is a terminating pod on the nominated node.
				return fmt.Errorf("not eligible due to a terminating pod caused by preemption on the nominated node")
			}
		}
	}
	return nil
}

// podTerminatingByPreemption returns true if the pod is in the termination state caused by preempt action.
func podTerminatingByPreemption(p *v1.Pod) bool {
	if p.DeletionTimestamp == nil {
		return false
	}

	for _, condition := range p.Status.Conditions {
		if condition.Type == v1.DisruptionTarget {
			return condition.Status == v1.ConditionTrue && condition.Reason == v1.PodReasonPreemptionByScheduler
		}
	}
	return false
}

// PodPriority returns priority of the given pod.
func PodPriority(pod *v1.Pod) int32 {
	if pod.Spec.Priority != nil {
		return *pod.Spec.Priority
	}
	// When priority of a running pod is nil, it means it was created at a time
	// that there was no global default priority class and the priority class
	// name of the pod was empty. So, we resolve to the static default priority.
	return 0
}
