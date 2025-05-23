/*
Copyright 2018 The Kubernetes Authors.

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
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	k8sutil "k8s.io/kubernetes/pkg/scheduler/util"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	EnableTopologyAwarePreemptionKey = "enableTopologyAwarePreemption"

	TopologyAwarePreemptWorkerNumKey = "topologyAwarePreemptWorkerNum"

	MinCandidateNodesPercentageKey = "minCandidateNodesPercentage"
	MinCandidateNodesAbsoluteKey   = "minCandidateNodesAbsolute"
	MaxCandidateNodesAbsoluteKey   = "maxCandidateNodesAbsolute"
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
	arguments.GetInt(&pmpt.minCandidateNodesPercentage, MinCandidateNodesPercentageKey)
	arguments.GetInt(&pmpt.minCandidateNodesAbsolute, MinCandidateNodesAbsoluteKey)
	arguments.GetInt(&pmpt.maxCandidateNodesAbsolute, MaxCandidateNodesAbsoluteKey)
	pmpt.ssn = ssn
}

func (pmpt *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Preempt ...")
	defer klog.V(5).Infof("Leaving Preempt ...")

	pmpt.parseArguments(ssn)

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	var underRequest []*api.JobInfo
	queues := map[api.QueueID]*api.QueueInfo{}

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip preemption, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if queue, found := ssn.Queues[job.Queue]; !found {
			continue
		} else if _, existed := queues[queue.UID]; !existed {
			klog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)
			queues[queue.UID] = queue
		}

		// check job if starving for more resources.
		if ssn.JobStarving(job) {
			if _, found := preemptorsMap[job.Queue]; !found {
				preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			preemptorsMap[job.Queue].Push(job)
			underRequest = append(underRequest, job)
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				if task.SchGated {
					continue
				}
				preemptorTasks[job.UID].Push(task)
			}
		}
	}

	ph := util.NewPredicateHelper()
	// Preemption between Jobs within Queue.
	for _, queue := range queues {
		for {
			preemptors := preemptorsMap[queue.UID]

			// If no preemptors, no preemption.
			if preemptors == nil || preemptors.Empty() {
				klog.V(4).Infof("No preemptors in Queue <%s>, break.", queue.Name)
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)

			stmt := framework.NewStatement(ssn)
			var assigned bool
			var err error
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

				assigned, err = pmpt.preempt(ssn, stmt, preemptor, func(task *api.TaskInfo) bool {
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
				stmt.Commit()
			} else {
				stmt.Discard()
				continue
			}

			if assigned {
				preemptors.Push(preemptorJob)
			}
		}

		// Preemption between Task within Job.
		for _, job := range underRequest {
			// Fix: preemptor numbers lose when in same job
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				// Again, skip scheduling gated tasks
				if task.SchGated {
					continue
				}
				preemptorTasks[job.UID].Push(task)
			}
			for {
				if _, found := preemptorTasks[job.UID]; !found {
					break
				}

				if preemptorTasks[job.UID].Empty() {
					break
				}

				preemptor := preemptorTasks[job.UID].Pop().(*api.TaskInfo)

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
				stmt.Commit()

				// If no preemption, next job.
				if !assigned {
					break
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
	predicateNodes, _ := predicateHelper.PredicateNodes(preemptor, allNodes, ssn.PredicateForPreemptAction, pmpt.enablePredicateErrorCache)

	if pmpt.enableTopologyAwarePreemption {
		return pmpt.topologyAwarePreempt(ssn, stmt, preemptor, filter, predicateNodes)
	}

	return pmpt.normalPreempt(ssn, stmt, preemptor, filter, predicateNodes)
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

		victimsQueue := ssn.BuildVictimsPriorityQueue(victims, preemptor)
		// Preempt victims for tasks, pick lowest priority task first.
		preempted := api.EmptyResource()

		for !victimsQueue.Empty() {
			// If reclaimed enough resources, break loop to avoid Sub panic.
			// Preempt action is about preempt in same queue, which job is not allocatable in allocate action, due to:
			// 1. cluster has free resource, but queue not allocatable
			// 2. cluster has no free resource, but queue not allocatable
			// 3. cluster has no free resource, but queue allocatable
			// for case 1 and 2, high priority job/task can preempt low priority job/task in same queue;
			// for case 3, it need to do reclaim resource from other queue, in reclaim action;
			// so if current queue is not allocatable(the queue will be overused when consider current preemptor's requests)
			// or current idle resource is not enough for preemptor, it need to continue preempting
			// otherwise, break out
			if ssn.Allocatable(currentQueue, preemptor) && preemptor.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
				break
			}
			preemptee := victimsQueue.Pop().(*api.TaskInfo)
			klog.V(3).Infof("Try to preempt Task <%s/%s> for Task <%s/%s>",
				preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name)
			if err := stmt.Evict(preemptee, "preempt"); err != nil {
				klog.Errorf("Failed to preempt Task <%s/%s> for Task <%s/%s>: %v",
					preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name, err)
				continue
			}
			preempted.Add(preemptee.Resreq)
		}

		evictionOccurred := false
		if !preempted.IsEmpty() {
			evictionOccurred = true
		}

		metrics.RegisterPreemptionAttempts()
		klog.V(3).Infof("Preempted <%v> for Task <%s/%s> requested <%v>.",
			preempted, preemptor.Namespace, preemptor.Name, preemptor.InitResreq)

		// If preemptor's queue is not allocatable, it means preemptor cannot be allocated. So no need care about the node idle resource
		if ssn.Allocatable(currentQueue, preemptor) && preemptor.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
			if err := stmt.Pipeline(preemptor, node.Name, evictionOccurred); err != nil {
				klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
					preemptor.Namespace, preemptor.Name, node.Name)
				if rollbackErr := stmt.UnPipeline(preemptor); rollbackErr != nil {
					klog.Errorf("Failed to unpipeline Task %v on %v in Session %v for %v.",
						preemptor.UID, node.Name, ssn.UID, rollbackErr)
				}
			}

			// Ignore pipeline error, will be corrected in next scheduling loop.
			assigned = true

			break
		}
	}

	return assigned, nil
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

func (pmpt *Action) topologyAwarePreempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	filter func(*api.TaskInfo) bool,
	predicateNodes []*api.NodeInfo,
) (bool, error) {
	// Find all preemption candidates.
	candidates, nodeToStatusMap, err := pmpt.findCandidates(preemptor, filter, predicateNodes, stmt)
	if err != nil && len(candidates) == 0 {
		return false, err
	}

	// Return error when there are no candidates that fit the pod.
	if len(candidates) == 0 {
		// Specify nominatedNodeName to clear the pod's nominatedNodeName status, if applicable.
		return false, fmt.Errorf("no candidates that fit the pod, the status of the nodes are %v", nodeToStatusMap)
	}

	// Find the best candidate.
	bestCandidate := SelectCandidate(candidates)
	if bestCandidate == nil || len(bestCandidate.Name()) == 0 {
		return false, fmt.Errorf("no candidate node for preemption")
	}

	if status := prepareCandidate(bestCandidate, preemptor.Pod, stmt, ssn); !status.IsSuccess() {
		return false, fmt.Errorf("failed to prepare candidate: %v", status)
	}

	if err := stmt.Pipeline(preemptor, bestCandidate.Name(), true); err != nil {
		klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
			preemptor.Namespace, preemptor.Name, bestCandidate.Name())
		if rollbackErr := stmt.UnPipeline(preemptor); rollbackErr != nil {
			klog.Errorf("Failed to unpipeline Task %v on %v in Session %v for %v.",
				preemptor.UID, bestCandidate.Name(), ssn.UID, rollbackErr)
		}
	}

	return true, nil
}

func (pmpt *Action) findCandidates(preemptor *api.TaskInfo, filter func(*api.TaskInfo) bool, predicateNodes []*api.NodeInfo, stmt *framework.Statement) ([]*candidate, map[string]api.Status, error) {
	if len(predicateNodes) == 0 {
		klog.V(3).Infof("No nodes are eligible to preempt task %s/%s", preemptor.Namespace, preemptor.Name)
		return nil, nil, nil
	}
	klog.Infof("the predicateNodes number is %d", len(predicateNodes))

	nodeToStatusMap := make(map[string]api.Status)

	offset, numCandidates := pmpt.GetOffsetAndNumCandidates(len(predicateNodes))

	candidates, nodeStatuses, err := pmpt.DryRunPreemption(preemptor, predicateNodes, offset, numCandidates, filter, stmt)
	for node, nodeStatus := range nodeStatuses {
		nodeToStatusMap[node] = nodeStatus
	}

	return candidates, nodeToStatusMap, err
}

// prepareCandidate evicts the victim pods before nominating the selected candidate
func prepareCandidate(c *candidate, pod *v1.Pod, stmt *framework.Statement, ssn *framework.Session) *api.Status {
	for _, victim := range c.Victims() {
		klog.V(3).Infof("Try to preempt Task <%s/%s> for Task <%s/%s>",
			victim.Namespace, victim.Name, pod.Namespace, pod.Name)
		if err := stmt.Evict(victim, "preempt"); err != nil {
			klog.Errorf("Failed to preempt Task <%s/%s> for Task <%s/%s>: %v",
				victim.Namespace, victim.Name, pod.Namespace, pod.Name, err)
			return api.AsStatus(err)
		}
	}

	metrics.RegisterPreemptionAttempts()

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

// calculateNumCandidates returns the number of candidates the FindCandidates
// method must produce from dry running based on the constraints given by
// <minCandidateNodesPercentage> and <minCandidateNodesAbsolute>. The number of
// candidates returned will never be greater than <numNodes>.
func (pmpt *Action) calculateNumCandidates(numNodes int) int {
	n := (numNodes * pmpt.minCandidateNodesPercentage) / 100

	if n < pmpt.minCandidateNodesAbsolute {
		n = pmpt.minCandidateNodesAbsolute
	}

	if n > pmpt.maxCandidateNodesAbsolute {
		n = pmpt.maxCandidateNodesAbsolute
	}

	if n > numNodes {
		n = numNodes
	}

	return n
}

// offset is used to randomly select a starting point in the potentialNodes array.
// This helps distribute the preemption checks across different nodes and avoid
// always starting from the beginning of the node list, which could lead to
// uneven distribution of preemption attempts.
// GetOffsetAndNumCandidates chooses a random offset and calculates the number
// of candidates that should be shortlisted for dry running preemption.
func (pmpt *Action) GetOffsetAndNumCandidates(numNodes int) (int, int) {
	return rand.Intn(numNodes), pmpt.calculateNumCandidates(numNodes)
}

func (pmpt *Action) DryRunPreemption(preemptor *api.TaskInfo, potentialNodes []*api.NodeInfo, offset, numCandidates int, filter func(*api.TaskInfo) bool, stmt *framework.Statement) ([]*candidate, map[string]api.Status, error) {
	candidates := newCandidateList(numCandidates)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	nodeStatuses := make(map[string]api.Status)
	var statusesLock sync.Mutex
	var errs []error

	job, found := pmpt.ssn.Jobs[preemptor.Job]
	if !found {
		return nil, nil, fmt.Errorf("not found Job %s in Session", preemptor.Job)
	}

	currentQueue := pmpt.ssn.Queues[job.Queue]

	state := pmpt.ssn.GetCycleState(preemptor.UID)

	checkNode := func(i int) {
		nodeInfoCopy := potentialNodes[(int(offset)+i)%len(potentialNodes)].Clone()
		stateCopy := state.Clone()

		victims, status := SelectVictimsOnNode(ctx, stateCopy, preemptor, currentQueue, nodeInfoCopy, pmpt.ssn, filter, stmt)
		if status.IsSuccess() && len(victims) != 0 {
			c := &candidate{
				victims: victims,
				name:    nodeInfoCopy.Name,
			}
			candidates.add(c)
			if candidates.size() >= numCandidates {
				cancel()
			}
			return
		}
		if status.IsSuccess() && len(victims) == 0 {
			status = api.AsStatus(fmt.Errorf("expected at least one victim pod on node %q", nodeInfoCopy.Name))
		}
		statusesLock.Lock()
		if status.Code == api.Error {
			errs = append(errs, status.AsError())
		}
		nodeStatuses[nodeInfoCopy.Name] = *status
		statusesLock.Unlock()
	}

	workqueue.ParallelizeUntil(ctx, pmpt.topologyAwarePreemptWorkerNum, len(potentialNodes), checkNode)
	return candidates.get(), nodeStatuses, utilerrors.NewAggregate(errs)
}

type candidate struct {
	victims []*api.TaskInfo
	name    string
}

// Victims returns s.victims.
func (s *candidate) Victims() []*api.TaskInfo {
	return s.victims
}

// Name returns s.name.
func (s *candidate) Name() string {
	return s.name
}

type candidateList struct {
	idx   int32
	items []*candidate
}

func newCandidateList(size int) *candidateList {
	return &candidateList{idx: -1, items: make([]*candidate, size)}
}

// add adds a new candidate to the internal array atomically.
func (cl *candidateList) add(c *candidate) {
	if idx := atomic.AddInt32(&cl.idx, 1); idx < int32(len(cl.items)) {
		cl.items[idx] = c
	}
}

// size returns the number of candidate stored. Note that some add() operations
// might still be executing when this is called, so care must be taken to
// ensure that all add() operations complete before accessing the elements of
// the list.
func (cl *candidateList) size() int {
	n := int(atomic.LoadInt32(&cl.idx) + 1)
	if n >= len(cl.items) {
		n = len(cl.items)
	}
	return n
}

// get returns the internal candidate array. This function is NOT atomic and
// assumes that all add() operations have been completed.
func (cl *candidateList) get() []*candidate {
	return cl.items[:cl.size()]
}

// SelectVictimsOnNode finds minimum set of pods on the given node that should be preempted in order to make enough room
// for "pod" to be scheduled.
func SelectVictimsOnNode(
	ctx context.Context,
	state *k8sframework.CycleState,
	preemptor *api.TaskInfo,
	currentQueue *api.QueueInfo,
	nodeInfo *api.NodeInfo,
	ssn *framework.Session,
	filter func(*api.TaskInfo) bool,
	stmt *framework.Statement,
) ([]*api.TaskInfo, *api.Status) {
	var potentialVictims []*api.TaskInfo

	removeTask := func(rti *api.TaskInfo) error {
		err := ssn.SimulateRemoveTaskFn(ctx, state, preemptor, rti, nodeInfo)
		if err != nil {
			return err
		}

		if err := nodeInfo.RemoveTask(rti); err != nil {
			return err
		}
		return nil
	}

	addTask := func(ati *api.TaskInfo) error {
		err := ssn.SimulateAddTaskFn(ctx, state, preemptor, ati, nodeInfo)
		if err != nil {
			return err
		}

		if err := nodeInfo.AddTask(ati); err != nil {
			return err
		}
		return nil
	}

	var preemptees []*api.TaskInfo
	for _, task := range nodeInfo.Tasks {
		if filter == nil {
			preemptees = append(preemptees, task.Clone())
		} else if filter(task) {
			preemptees = append(preemptees, task.Clone())
		}
	}

	klog.V(3).Infof("all preemptees: %v", preemptees)

	allVictims := ssn.Preemptable(preemptor, preemptees)
	metrics.UpdatePreemptionVictimsCount(len(allVictims))

	if err := util.ValidateVictims(preemptor, nodeInfo, allVictims); err != nil {
		klog.V(3).Infof("No validated victims on Node <%s>: %v", nodeInfo.Name, err)
		return nil, api.AsStatus(fmt.Errorf("no validated victims on Node <%s>: %v", nodeInfo.Name, err))
	}

	klog.V(3).Infof("allVictims: %v", allVictims)

	// Sort potentialVictims by pod priority from high to low, which ensures to
	// reprieve higher priority pods first.
	sort.Slice(allVictims, func(i, j int) bool { return k8sutil.MoreImportantPod(allVictims[i].Pod, allVictims[j].Pod) })

	victimsQueue := ssn.BuildVictimsPriorityQueue(allVictims, preemptor)

	for !victimsQueue.Empty() {
		task := victimsQueue.Pop().(*api.TaskInfo)
		potentialVictims = append(potentialVictims, task)
		if err := removeTask(task); err != nil {
			return nil, api.AsStatus(err)
		}

		if ssn.SimulateAllocatableFn(ctx, state, currentQueue, preemptor) && preemptor.InitResreq.LessEqual(nodeInfo.FutureIdle(), api.Zero) {
			if err := ssn.SimulatePredicateFn(ctx, state, preemptor, nodeInfo); err == nil {
				klog.V(3).Infof("Pod %v/%v can be scheduled on node %v after preempt %v/%v, stop evicting more pods", preemptor.Namespace, preemptor.Name, nodeInfo.Name, task.Namespace, task.Name)
				break
			}
		}
	}

	// No potential victims are found, and so we don't need to evaluate the node again since its state didn't change.
	if len(potentialVictims) == 0 {
		return nil, api.AsStatus(fmt.Errorf("no preemption victims found for incoming pod"))
	}

	// If the new pod does not fit after removing all potential victim pods,
	// we are almost done and this node is not suitable for preemption. The only
	// condition that we could check is if the "pod" is failing to schedule due to
	// inter-pod affinity to one or more victims, but we have decided not to
	// support this case for performance reasons. Having affinity to lower
	// priority pods is not a recommended configuration anyway.
	if err := ssn.SimulatePredicateFn(ctx, state, preemptor, nodeInfo); err != nil {
		return nil, api.AsStatus(fmt.Errorf("failed to predicate pod %s/%s on node %s: %v", preemptor.Namespace, preemptor.Name, nodeInfo.Name, err))
	}

	var victims []*api.TaskInfo

	klog.V(3).Infof("potentialVictims---: %v, nodeInfo: %v", potentialVictims, nodeInfo.Name)

	// TODO: consider the PDB violation here

	reprievePod := func(pi *api.TaskInfo) (bool, error) {
		if err := addTask(pi); err != nil {
			klog.ErrorS(err, "Failed to add task", "task", klog.KObj(pi.Pod))
			return false, err
		}

		var fits bool
		if ssn.SimulateAllocatableFn(ctx, state, currentQueue, preemptor) && preemptor.InitResreq.LessEqual(nodeInfo.FutureIdle(), api.Zero) {
			err := ssn.SimulatePredicateFn(ctx, state, preemptor, nodeInfo)
			fits = err == nil
		}

		if !fits {
			if err := removeTask(pi); err != nil {
				return false, err
			}
			victims = append(victims, pi)
			klog.V(3).Info("Pod is a potential preemption victim on node", "pod", klog.KObj(pi.Pod), "node", klog.KObj(nodeInfo.Node))
		}
		klog.Infof("reprievePod for task: %v, fits: %v", pi.Name, fits)
		return fits, nil
	}

	// Now we try to reprieve non-violating victims.
	for _, p := range potentialVictims {
		if _, err := reprievePod(p); err != nil {
			return nil, api.AsStatus(err)
		}
	}

	klog.Infof("victims: %v", victims)

	return victims, &api.Status{
		Reason: "",
	}
}

// SelectCandidate chooses the best-fit candidate from given <candidates> and return it.
// NOTE: This method is exported for easier testing in default preemption.
func SelectCandidate(candidates []*candidate) *candidate {
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	victimsMap := CandidatesToVictimsMap(candidates)
	scoreFuncs := OrderedScoreFuncs(victimsMap)
	candidateNode := pickOneNodeForPreemption(victimsMap, scoreFuncs)

	// Same as candidatesToVictimsMap, this logic is not applicable for out-of-tree
	// preemption plugins that exercise different candidates on the same nominated node.
	if victims := victimsMap[candidateNode]; victims != nil {
		return &candidate{
			victims: victims,
			name:    candidateNode,
		}
	}

	// We shouldn't reach here.
	klog.Error(errors.New("no candidate selected"), "Should not reach here", "candidates", candidates)
	// To not break the whole flow, return the first candidate.
	return candidates[0]
}

func CandidatesToVictimsMap(candidates []*candidate) map[string][]*api.TaskInfo {
	m := make(map[string][]*api.TaskInfo, len(candidates))
	for _, c := range candidates {
		m[c.Name()] = c.Victims()
	}
	return m
}

// TODO: Consider exposing score functions to plugins in the future
func OrderedScoreFuncs(nodesToVictims map[string][]*api.TaskInfo) []func(node string) int64 {
	return nil
}

// pickOneNodeForPreemption chooses one node among the given nodes.
// It assumes pods in each map entry are ordered by decreasing priority.
// If the scoreFuncs is not empty, It picks a node based on score scoreFuncs returns.
// If the scoreFuncs is empty,
// It picks a node based on the following criteria:
// 1. A node with minimum number of PDB violations.
// 2. A node with minimum highest priority victim is picked.
// 3. Ties are broken by sum of priorities of all victims.
// 4. If there are still ties, node with the minimum number of victims is picked.
// 5. If there are still ties, node with the latest start time of all highest priority victims is picked.
// 6. If there are still ties, the first such node is picked (sort of randomly).
// The 'minNodes1' and 'minNodes2' are being reused here to save the memory
// allocation and garbage collection time.
func pickOneNodeForPreemption(nodesToVictims map[string][]*api.TaskInfo, scoreFuncs []func(node string) int64) string {
	if len(nodesToVictims) == 0 {
		return ""
	}

	allCandidates := make([]string, 0, len(nodesToVictims))
	for node := range nodesToVictims {
		allCandidates = append(allCandidates, node)
	}

	if len(scoreFuncs) == 0 {
		minHighestPriorityScoreFunc := func(node string) int64 {
			// highestPodPriority is the highest priority among the victims on this node.
			highestPodPriority := PodPriority(nodesToVictims[node][0].Pod)
			// The smaller the highestPodPriority, the higher the score.
			return -int64(highestPodPriority)
		}
		minSumPrioritiesScoreFunc := func(node string) int64 {
			var sumPriorities int64
			for _, task := range nodesToVictims[node] {
				// We add MaxInt32+1 to all priorities to make all of them >= 0. This is
				// needed so that a node with a few pods with negative priority is not
				// picked over a node with a smaller number of pods with the same negative
				// priority (and similar scenarios).
				sumPriorities += int64(PodPriority(task.Pod)) + int64(math.MaxInt32+1)
			}
			// The smaller the sumPriorities, the higher the score.
			return -sumPriorities
		}
		minNumPodsScoreFunc := func(node string) int64 {
			// The smaller the length of pods, the higher the score.
			return -int64(len(nodesToVictims[node]))
		}
		latestStartTimeScoreFunc := func(node string) int64 {
			// Get the earliest start time of all pods on the current node.
			earliestStartTimeOnNode := GetEarliestPodStartTime(nodesToVictims[node])
			if earliestStartTimeOnNode == nil {
				klog.Error(errors.New("earliestStartTime is nil for node"), "Should not reach here", "node", node)
				return int64(math.MinInt64)
			}
			// The bigger the earliestStartTimeOnNode, the higher the score.
			return earliestStartTimeOnNode.UnixNano()
		}

		// Each scoreFunc scores the nodes according to specific rules and keeps the name of the node
		// with the highest score. If and only if the scoreFunc has more than one node with the highest
		// score, we will execute the other scoreFunc in order of precedence.
		scoreFuncs = []func(string) int64{
			// A node with a minimum highest priority victim is preferable.
			minHighestPriorityScoreFunc,
			// A node with the smallest sum of priorities is preferable.
			minSumPrioritiesScoreFunc,
			// A node with the minimum number of pods is preferable.
			minNumPodsScoreFunc,
			// A node with the latest start time of all highest priority victims is preferable.
			latestStartTimeScoreFunc,
			// If there are still ties, then the first Node in the list is selected.
		}
	}

	for _, f := range scoreFuncs {
		selectedNodes := []string{}
		maxScore := int64(math.MinInt64)
		for _, node := range allCandidates {
			score := f(node)
			if score > maxScore {
				maxScore = score
				selectedNodes = []string{}
			}
			if score == maxScore {
				selectedNodes = append(selectedNodes, node)
			}
		}
		if len(selectedNodes) == 1 {
			return selectedNodes[0]
		}
		allCandidates = selectedNodes
	}

	return allCandidates[0]
}

// GetEarliestPodStartTime returns the earliest start time of all pods that
// have the highest priority among all victims.
func GetEarliestPodStartTime(tasks []*api.TaskInfo) *metav1.Time {
	if len(tasks) == 0 {
		// should not reach here.
		klog.Background().Error(nil, "victims.Pods is empty. Should not reach here")
		return nil
	}

	earliestPodStartTime := GetPodStartTime(tasks[0].Pod)
	maxPriority := PodPriority(tasks[0].Pod)

	for _, task := range tasks {
		if podPriority := PodPriority(task.Pod); podPriority == maxPriority {
			if podStartTime := GetPodStartTime(task.Pod); podStartTime.Before(earliestPodStartTime) {
				earliestPodStartTime = podStartTime
			}
		} else if podPriority > maxPriority {
			maxPriority = podPriority
			earliestPodStartTime = GetPodStartTime(task.Pod)
		}
	}

	return earliestPodStartTime
}

// GetPodStartTime returns start time of the given pod or current timestamp
// if it hasn't started yet.
func GetPodStartTime(pod *v1.Pod) *metav1.Time {
	if pod.Status.StartTime != nil {
		return pod.Status.StartTime
	}
	// Assumed pods and bound pods that haven't started don't have a StartTime yet.
	return &metav1.Time{Time: time.Now()}
}
