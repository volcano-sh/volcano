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

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (alloc *Action) Name() string {
	return "allocate"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Allocate ...")
	defer klog.V(3).Infof("Leaving Allocate ...")

	// the allocation for pod may have many stages
	// 1. pick a namespace named N (using ssn.NamespaceOrderFn)
	// 2. pick a queue named Q from N (using ssn.QueueOrderFn)
	// 3. pick a job named J from Q (using ssn.JobOrderFn)
	// 4. pick a task T from J (using ssn.TaskOrderFn)
	// 5. use predicateFn to filter out node that T can not be allocated on.
	// 6. use ssn.NodeOrderFn to judge the best node and assign it to T

	namespaces := util.NewPriorityQueue(ssn.NamespaceOrderFn)

	// jobsMap is map[api.NamespaceName]map[api.QueueID]PriorityQueue(*api.JobInfo)
	// used to find job with highest priority in given queue and namespace
	jobsMap := map[api.NamespaceName]map[api.QueueID]*util.PriorityQueue{}

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: job status is pending.",
				job.Namespace, job.Name, job.Queue)
			continue
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

		namespace := api.NamespaceName(job.Namespace)
		queueMap, found := jobsMap[namespace]
		if !found {
			namespaces.Push(namespace)

			queueMap = make(map[api.QueueID]*util.PriorityQueue)
			jobsMap[namespace] = queueMap
		}

		jobs, found := queueMap[job.Queue]
		if !found {
			jobs = util.NewPriorityQueue(ssn.JobOrderFn)
			queueMap[job.Queue] = jobs
		}

		klog.V(4).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
		jobs.Push(job)
	}

	klog.V(3).Infof("Try to allocate resource to %d Namespaces", len(jobsMap))

	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	allNodes := ssn.NodeList
	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		// Check for Resource Predicate
		if !task.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
			return api.NewFitError(task, node, api.NodeResourceFitFailed)
		}

		return ssn.PredicateFn(task, node)
	}

	// To pick <namespace, queue> tuple for job, we choose to pick namespace firstly.
	// Because we believe that number of queues would less than namespaces in most case.
	// And, this action would make the resource usage among namespace balanced.
	for {
		if namespaces.Empty() {
			break
		}

		// pick namespace from namespaces PriorityQueue
		namespace := namespaces.Pop().(api.NamespaceName)

		queueInNamespace := jobsMap[namespace]

		// pick queue for given namespace
		//
		// This block use an algorithm with time complex O(n).
		// But at least PriorityQueue could not be used here,
		// because the allocation of job would change the priority of queue among all namespaces,
		// and the PriorityQueue have no ability to update priority for a special queue.
		var queue *api.QueueInfo
		for queueID := range queueInNamespace {
			currentQueue := ssn.Queues[queueID]
			if ssn.Overused(currentQueue) {
				klog.V(3).Infof("Namespace <%s> Queue <%s> is overused, ignore it.", namespace, currentQueue.Name)
				delete(queueInNamespace, queueID)
				continue
			}
			if jobs, found := queueInNamespace[currentQueue.UID]; found && jobs.Empty() {
				continue
			}

			if queue == nil || ssn.QueueOrderFn(currentQueue, queue) {
				queue = currentQueue
			}
		}

		if queue == nil {
			klog.V(3).Infof("Namespace <%s> have no queue, skip it", namespace)
			continue
		}

		klog.V(3).Infof("Try to allocate resource to Jobs in Namespace <%s> Queue <%v>", namespace, queue.Name)

		jobs, found := queueInNamespace[queue.UID]
		if !found || jobs.Empty() {
			delete(queueInNamespace, queue.UID)
			namespaces.Push(namespace)
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

		klog.V(3).Infof("Try to allocate resource to %d tasks of Job <%v/%v>",
			tasks.Len(), job.Namespace, job.Name)

		stmt := framework.NewStatement(ssn)
		ph := util.NewPredicateHelper()
		for !tasks.Empty() {
			task := tasks.Pop().(*api.TaskInfo)

			if !ssn.Allocatable(queue, task) {
				klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
				continue
			}

			klog.V(3).Infof("There are <%d> nodes for Job <%v/%v>", len(ssn.Nodes), job.Namespace, job.Name)

			predicateNodes, fitErrors := ph.PredicateNodes(task, allNodes, predicateFn)
			if len(predicateNodes) == 0 {
				job.NodesFitErrors[task.UID] = fitErrors
				break
			}

			var candidateNodes []*api.NodeInfo
			for _, n := range predicateNodes {
				if task.InitResreq.LessEqual(n.Idle, api.Zero) || task.InitResreq.LessEqual(n.FutureIdle(), api.Zero) {
					candidateNodes = append(candidateNodes, n)
				}
			}

			var node *api.NodeInfo
			switch {
			case len(candidateNodes) == 0: // If not candidate nodes for this task, skip it.
				continue
			case len(candidateNodes) == 1: // If only one node after predicate, just use it.
				node = candidateNodes[0]
			case len(candidateNodes) > 1: // If more than one node after predicate, using "the best" one
				nodeScores := util.PrioritizeNodes(task, candidateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

				node = ssn.BestNodeFn(task, nodeScores)
				if node == nil {
					node = util.SelectBestNode(nodeScores)
				}
			}

			// Allocate idle resource to the task.
			if task.InitResreq.LessEqual(node.Idle, api.Zero) {
				klog.V(3).Infof("Binding Task <%v/%v> to node <%v>",
					task.Namespace, task.Name, node.Name)
				if err := stmt.Allocate(task, node); err != nil {
					klog.Errorf("Failed to bind Task %v on %v in Session %v, err: %v",
						task.UID, node.Name, ssn.UID, err)
				} else {
					metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
					metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
				}
			} else {
				klog.V(3).Infof("Predicates failed for task <%s/%s> on node <%s> with limited resources",
					task.Namespace, task.Name, node.Name)

				// Allocate releasing resource to the task if any.
				if task.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
					klog.V(3).Infof("Pipelining Task <%v/%v> to node <%v> for <%v> on <%v>",
						task.Namespace, task.Name, node.Name, task.InitResreq, node.Releasing)
					if err := stmt.Pipeline(task, node.Name); err != nil {
						klog.Errorf("Failed to pipeline Task %v on %v in Session %v for %v.",
							task.UID, node.Name, ssn.UID, err)
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

		// Added Namespace back until no job in Namespace.
		namespaces.Push(namespace)
	}
}

func (alloc *Action) UnInitialize() {}
