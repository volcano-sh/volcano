/*
 Copyright 2021 The Volcano Authors.

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
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct {
	session *framework.Session
}

func New() *Action {
	return &Action{}
}

func (alloc *Action) Name() string {
	return "allocate"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Allocate ...")
	defer klog.V(5).Infof("Leaving Allocate ...")

	// the allocation for pod may have many stages
	// 1. pick a queue named Q (using ssn.QueueOrderFn)
	// 2. pick a job named J from Q (using ssn.JobOrderFn)
	// 3. pick a task T from J (using ssn.TaskOrderFn)
	// 4. use predicateFn to filter out node that T can not be allocated on.
	// 5. use ssn.NodeOrderFn to judge the best node and assign it to T

	// queues sort queues by QueueOrderFn.
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	// jobsMap is used to find job with the highest priority in given queue.
	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	alloc.session = ssn
	alloc.pickUpQueuesAndJobs(queues, jobsMap)
	klog.V(3).Infof("Try to allocate resource to %d Queues", len(jobsMap))
	alloc.allocateResources(queues, jobsMap)
}

func (alloc *Action) pickUpQueuesAndJobs(queues *util.PriorityQueue, jobsMap map[api.QueueID]*util.PriorityQueue) {
	ssn := alloc.session
	for _, job := range ssn.Jobs {
		// If not config enqueue action, change Pending pg into Inqueue statue to avoid blocking job scheduling.
		if job.IsPending() {
			if conf.EnabledActionMap["enqueue"] {
				klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: job status is pending.",
					job.Namespace, job.Name, job.Queue)
				continue
			} else {
				klog.V(4).Infof("Job <%s/%s> Queue <%s> status update from pending to inqueue, reason: no enqueue action is configured.",
					job.Namespace, job.Name, job.Queue)
				job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			}
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if _, found := ssn.Queues[job.Queue]; !found {
			klog.Warningf("Skip adding Job <%s/%s> because its queue %s is not found",
				job.Namespace, job.Name, job.Queue)
			continue
		}

		if _, found := jobsMap[job.Queue]; !found {
			jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			queues.Push(ssn.Queues[job.Queue])
		}

		klog.V(4).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
		jobsMap[job.Queue].Push(job)
	}
}

// allocateResources primarily accomplishes two steps:
// 1. picks up tasks.
// 2. allocates resources to these tasks. (this step is carried out by the allocateResourcesForTasks method.)
func (alloc *Action) allocateResources(queues *util.PriorityQueue, jobsMap map[api.QueueID]*util.PriorityQueue) {
	ssn := alloc.session
	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	allNodes := ssn.NodeList

	// To pick <namespace, queue> tuple for job, we choose to pick namespace firstly.
	// Because we believe that number of queues would less than namespaces in most case.
	// And, this action would make the resource usage among namespace balanced.
	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		klog.V(3).Infof("Try to allocate resource to Jobs in Queue <%s>", queue.Name)

		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			klog.V(4).Infof("Can not find jobs for queue %s.", queue.Name)
			continue
		}

		job := jobs.Pop().(*api.JobInfo)
		if _, found = pendingTasks[job.UID]; !found {
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				// Skip BestEffort task in 'allocate' action.
				if task.Resreq.IsEmpty() {
					klog.V(4).Infof("Task <%v/%v> is BestEffort task, skip it.",
						task.Namespace, task.Name)
					continue
				}

				tasks.Push(task)
			}
			pendingTasks[job.UID] = tasks
		}
		tasks := pendingTasks[job.UID]

		if tasks.Empty() {
			// put queue back again and try other jobs in this queue
			queues.Push(queue)
			continue
		}

		klog.V(3).Infof("Try to allocate resource to %d tasks of Job <%v/%v>",
			tasks.Len(), job.Namespace, job.Name)

		alloc.allocateResourcesForTasks(tasks, job, jobs, queue, allNodes)

		// Put back the queue to priority queue after job's resource allocating finished,
		// To ensure that the priority of the queue is calculated based on the latest resource allocation situation.
		queues.Push(queue)
	}
}

func (alloc *Action) allocateResourcesForTasks(tasks *util.PriorityQueue, job *api.JobInfo, jobs *util.PriorityQueue, queue *api.QueueInfo, allNodes []*api.NodeInfo) {
	ssn := alloc.session
	stmt := framework.NewStatement(ssn)
	ph := util.NewPredicateHelper()

	for !tasks.Empty() {
		task := tasks.Pop().(*api.TaskInfo)

		if !ssn.Allocatable(queue, task) {
			klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
			continue
		}

		klog.V(3).Infof("There are <%d> nodes for Job <%v/%v>", len(ssn.Nodes), job.Namespace, job.Name)

		if err := ssn.PrePredicateFn(task); err != nil {
			klog.V(3).Infof("PrePredicate for task %s/%s failed for: %v", task.Namespace, task.Name, err)
			fitErrors := api.NewFitErrors()
			for _, ni := range allNodes {
				fitErrors.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fitErrors
			break
		}

		predicateNodes, fitErrors := ph.PredicateNodes(task, allNodes, alloc.predicate, true)
		if len(predicateNodes) == 0 {
			job.NodesFitErrors[task.UID] = fitErrors
			// Assume that  all left tasks is allocable, can not meet gang-scheduling min member, we should break from continuously allocating
			// otherwise, should continue to find other allocable task
			if job.CheckJobNeedContinueAllocating() {
				continue
			} else {
				break
			}
		}

		// Candidate nodes are divided into two gradients:
		// - the first gradient node: a list of free nodes that satisfy the task resource request;
		// - The second gradient node: the node list whose sum of node idle resources and future idle meets the task resource request;
		// Score the first gradient node first. If the first gradient node meets the requirements, ignore the second gradient node list,
		// otherwise, score the second gradient node and select the appropriate node.
		var candidateNodes [][]*api.NodeInfo
		var idleCandidateNodes []*api.NodeInfo
		var futureIdleCandidateNodes []*api.NodeInfo
		for _, n := range predicateNodes {
			if task.InitResreq.LessEqual(n.Idle, api.Zero) {
				idleCandidateNodes = append(idleCandidateNodes, n)
			} else if task.InitResreq.LessEqual(n.FutureIdle(), api.Zero) {
				futureIdleCandidateNodes = append(futureIdleCandidateNodes, n)
			} else {
				klog.V(5).Infof("Predicate filtered node %v, idle: %v and future idle: %v do not meet the requirements of task: %v",
					n.Name, n.Idle, n.FutureIdle(), task.Name)
			}
		}
		candidateNodes = append(candidateNodes, idleCandidateNodes)
		candidateNodes = append(candidateNodes, futureIdleCandidateNodes)

		var bestNode *api.NodeInfo
		for index, nodes := range candidateNodes {
			if klog.V(5).Enabled() {
				for _, node := range nodes {
					klog.V(5).Infof("node %v, idle: %v, future idle: %v", node.Name, node.Idle, node.FutureIdle())
				}
			}
			switch {
			case len(nodes) == 0:
				klog.V(5).Infof("Task: %v, no matching node is found in the candidateNodes（index: %d） list.", task.Name, index)
			case len(nodes) == 1: // If only one node after predicate, just use it.
				bestNode = nodes[0]
			case len(nodes) > 1: // If more than one node after predicate, using "the best" one
				nodeScores := util.PrioritizeNodes(task, nodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

				bestNode = ssn.BestNodeFn(task, nodeScores)
				if bestNode == nil {
					bestNode = util.SelectBestNode(nodeScores)
				}
			}

			// If a proper node is found in idleCandidateNodes, skip futureIdleCandidateNodes and directly return the node information.
			if bestNode != nil {
				break
			}
		}

		// Allocate idle resource to the task.
		if task.InitResreq.LessEqual(bestNode.Idle, api.Zero) {
			klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, bestNode.Name)
			if err := stmt.Allocate(task, bestNode); err != nil {
				klog.Errorf("Failed to bind Task %v on %v in Session %v, err: %v",
					task.UID, bestNode.Name, ssn.UID, err)
			} else {
				metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
				metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
			}
		} else {
			klog.V(3).Infof("Predicates failed in allocate for task <%s/%s> on node <%s> with limited resources",
				task.Namespace, task.Name, bestNode.Name)

			// Allocate releasing resource to the task if any.
			if task.InitResreq.LessEqual(bestNode.FutureIdle(), api.Zero) {
				klog.V(3).Infof("Pipelining Task <%v/%v> to node <%v> for <%v> on <%v>",
					task.Namespace, task.Name, bestNode.Name, task.InitResreq, bestNode.Releasing)
				if err := stmt.Pipeline(task, bestNode.Name, false); err != nil {
					klog.Errorf("Failed to pipeline Task %v on %v in Session %v for %v.",
						task.UID, bestNode.Name, ssn.UID, err)
				} else {
					metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
					metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
				}
			}
		}

		if ssn.JobReady(job) && !tasks.Empty() {
			jobs.Push(job)
			break
		}
	}

	if ssn.JobReady(job) {
		stmt.Commit()
	} else {
		if !ssn.JobPipelined(job) {
			stmt.Discard()
		}
	}
}

func (alloc *Action) predicate(task *api.TaskInfo, node *api.NodeInfo) ([]*api.Status, error) {
	// Check for Resource Predicate
	if ok, resources := task.InitResreq.LessEqualWithResourcesName(node.FutureIdle(), api.Zero); !ok {
		return nil, api.NewFitError(task, node, api.WrapInsufficientResourceReason(resources))
	}
	var statusSets util.StatusSets
	statusSets, err := alloc.session.PredicateFn(task, node)
	if err != nil {
		return nil, api.NewFitError(task, node, err.Error())
	}

	if statusSets.ContainsUnschedulable() || statusSets.ContainsUnschedulableAndUnresolvable() ||
		statusSets.ContainsErrorSkipOrWait() {
		return nil, api.NewFitError(task, node, statusSets.Message())
	}
	return nil, nil
}

func (alloc *Action) UnInitialize() {}
