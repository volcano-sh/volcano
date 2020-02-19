/*
Copyright 2017 The Kubernetes Authors.

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
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type allocateAction struct {
	ssn *framework.Session
}

func New() *allocateAction {
	return &allocateAction{}
}

func (alloc *allocateAction) Name() string {
	return "allocate"
}

func (alloc *allocateAction) Initialize() {}

func (alloc *allocateAction) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Allocate ...")
	defer klog.V(3).Infof("Leaving Allocate ...")

	jobGroups := buildJobGroups(ssn.Jobs)
	namespaces := util.NewPriorityQueue(ssn.NamespaceOrderFn)
	jobGroupMap := map[api.NamespaceName]map[api.QueueID]*util.PriorityQueue{}
	// the allocation for pod may have many stages
	// 1. pick a namespace named N (using ssn.NamespaceOrderFn)
	// 2. pick a queue named Q from N (using ssn.QueueOrderFn)
	// 3. pick a jobGroup named G from Q (using ssn.JobOrderFn)
	// 4. pick a job named J from G (using ssn.JobOrderFn)
	// 4. pick a task T from J (using ssn.TaskOrderFn)
	// 5. use predicateFn to filter out node that T can not be allocated on.
	// 6. use ssn.NodeOrderFn to judge the best node and assign it to T

	for _, jobGroup := range jobGroups {
		ignore := false
		for _, job := range jobGroup.Jobs {
			if job.PodGroup.Status.Phase == scheduling.PodGroupPending {
				ignore = true
				break
			}
			if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
				klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
				ignore = true
				break
			}

			if _, found := ssn.Queues[job.Queue]; !found {
				klog.Warningf("Skip adding Job <%s/%s> because its queue %s is not found",
					job.Namespace, job.Name, job.Queue)
				ignore = true
				break
			}
		}
		if ignore {
			continue
		}
		namespace := api.NamespaceName(jobGroup.Namespace)
		queueMap, found := jobGroupMap[namespace]
		if !found {
			namespaces.Push(namespace)
			queueMap = make(map[api.QueueID]*util.PriorityQueue)
			jobGroupMap[namespace] = queueMap
		}

		groups, found := queueMap[jobGroup.Queue]
		if !found {
			groups = util.NewPriorityQueue(ssn.JobOrderFn)
			queueMap[jobGroup.Queue] = groups
		}
		klog.V(4).Infof("Added JobGroup %s", jobGroup.UID)
		groups.Push(jobGroup)
	}

	klog.V(3).Infof("Try to allocate resource to %d Namespaces", len(jobGroupMap))

	pendingTasks := map[api.JobID]*util.PriorityQueue{}

	allNodes := util.GetNodeList(ssn.Nodes)

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		// Check for Resource Predicate
		if !task.InitResreq.LessEqual(node.FutureIdle()) {
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
		queueInNamespace := jobGroupMap[namespace]

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
			if queue == nil || ssn.QueueOrderFn(currentQueue, queue) {
				queue = currentQueue
			}
		}
		if queue == nil {
			klog.V(3).Infof("Namespace <%s> have no queue, skip it", namespace)
			continue
		}

		klog.V(3).Infof("Try to allocate resource to Jobs in Namespace <%s> Queue <%v>", namespace, queue.Name)
		jobGroupQueue, found := queueInNamespace[queue.UID]
		if !found || jobGroupQueue.Empty() {
			klog.V(4).Infof("Can not find jobs for queue %s.", queue.Name)
			continue
		}

		jobGroup := jobGroupQueue.Pop().(*api.JobGroupInfo)
		stmt := ssn.Statement()
		taskAllAllocated := true
		for _, job := range jobGroup.Jobs {
			if _, found := pendingTasks[job.UID]; !found {
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

			for !tasks.Empty() {
				task := tasks.Pop().(*api.TaskInfo)

				klog.V(3).Infof("There are <%d> nodes for Job <%v/%v>",
					len(ssn.Nodes), job.Namespace, job.Name)

				//any task that doesn't fit will be the last processed
				//within this loop context so any existing contents of
				//NodesFitDelta are for tasks that eventually did fit on a
				//node
				if len(job.NodesFitDelta) > 0 {
					job.NodesFitDelta = make(api.NodeResourceMap)
				}

				predicateNodes, fitErrors := util.PredicateNodes(task, allNodes, predicateFn)
				if len(predicateNodes) == 0 {
					job.NodesFitErrors[task.UID] = fitErrors
					break
				}

				nodeScores := util.PrioritizeNodes(task, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)
				node := util.SelectBestNode(nodeScores)
				// Allocate idle resource to the task.
				if task.InitResreq.LessEqual(node.Idle) {
					klog.V(3).Infof("Binding Task <%v/%v> to node <%v>",
						task.Namespace, task.Name, node.Name)
					if err := stmt.Allocate(task, node.Name); err != nil {
						klog.Errorf("Failed to bind Task %v on %v in Session %v, err: %v",
							task.UID, node.Name, ssn.UID, err)
					}
				} else {
					//store information about missing resources
					job.NodesFitDelta[node.Name] = node.Idle.Clone()
					job.NodesFitDelta[node.Name].FitDelta(task.InitResreq)
					klog.V(3).Infof("Predicates failed for task <%s/%s> on node <%s> with limited resources",
						task.Namespace, task.Name, node.Name)
					// Allocate releasing resource to the task if any.
					if task.InitResreq.LessEqual(node.FutureIdle()) {
						klog.V(3).Infof("Pipelining Task <%v/%v> to node <%v> for <%v> on <%v>",
							task.Namespace, task.Name, node.Name, task.InitResreq, node.Releasing)
						if err := stmt.Pipeline(task, node.Name); err != nil {
							klog.Errorf("Failed to pipeline Task %v on %v",
								task.UID, node.Name)
						}
					}
				}
				if ssn.JobReady(job) {
					taskAllAllocated = false
					break
				}
			}

		}

		ready := true
		for _, job := range jobGroup.Jobs {
			if !ssn.JobReady(job) {
				ready = false
				break
			}
		}
		if ready {
			stmt.Commit()
			if !taskAllAllocated {
				// push back the JobGroup for the remaining tasks' allocation
				jobGroupQueue.Push(jobGroup)
			}

		} else {
			stmt.Discard()
		}
		// Added Namespace back until no job in Namespace.
		namespaces.Push(namespace)

	}
}

func (alloc *allocateAction) UnInitialize() {}

// buildJobGroups builds list of JobGroupInfo based on the `SubGroup` of JobInfo
// if JobInfo.SubGroup is empty, itself makes a JobGroupInfo
func buildJobGroups(jobs map[api.JobID]*api.JobInfo) []*api.JobGroupInfo {
	jobGroups := make(map[string]*api.JobGroupInfo)
	remainingGroups := make(map[string]*api.JobGroupInfo)
	for _, job := range jobs {
		if len(job.SubGroup) > 0 {
			if _, found := jobGroups[job.SubGroup]; !found {
				jobGroups[job.SubGroup] = api.NewJobGroupInfo(job)
			}
			group := jobGroups[job.SubGroup]
			if group.Namespace != job.Namespace || group.Queue != job.Queue {
				klog.Warningf("Job <%s> within a Group must be in the same namespace and queue. Ignore its SubGroup Spec", job.UID)
				job.SubGroup = ""
				remainingGroups[string(job.UID)] = api.NewJobGroupInfo(job)
				remainingGroups[string(job.UID)].AddJob(job)
				continue
			}
			group.AddJob(job)
		} else {
			remainingGroups[string(job.UID)] = api.NewJobGroupInfo(job)
			remainingGroups[string(job.UID)].AddJob(job)
		}
	}
	groups := make([]*api.JobGroupInfo, 0)
	for _, job := range jobGroups {
		groups = append(groups, job)
	}
	for _, job := range remainingGroups {
		groups = append(groups, job)
	}
	return groups
}
