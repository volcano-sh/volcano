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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct {
	enablePredicateErrorCache bool
}

func New() *Action {
	return &Action{
		enablePredicateErrorCache: true,
	}
}

func (pmpt *Action) Name() string {
	return "preempt"
}

func (pmpt *Action) Initialize() {}

func (pmpt *Action) parseArguments(ssn *framework.Session) {
	arguments := framework.GetArgOfActionFromConf(ssn.Configurations, pmpt.Name())
	arguments.GetBool(&pmpt.enablePredicateErrorCache, conf.EnablePredicateErrCacheKey)
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

	assigned := false

	if err := ssn.PrePredicateFn(preemptor); err != nil {
		return false, fmt.Errorf("PrePredicate for task %s/%s failed for: %v", preemptor.Namespace, preemptor.Name, err)
	}

	predicateFn := ssn.PredicateForPreemptAction
	// we should filter out those nodes that are UnschedulableAndUnresolvable status got in allocate action
	allNodes := ssn.GetUnschedulableAndUnresolvableNodesForTask(preemptor)
	predicateNodes, _ := predicateHelper.PredicateNodes(preemptor, allNodes, predicateFn, pmpt.enablePredicateErrorCache)

	nodeScores := util.PrioritizeNodes(preemptor, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

	selectedNodes := util.SortNodes(nodeScores)

	job, found := ssn.Jobs[preemptor.Job]
	if !found {
		return false, fmt.Errorf("not found Job %s in Session", preemptor.Job)
	}

	currentQueue := ssn.Queues[job.Queue]

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

		// If preemptor's queue is overused, it means preemptor can not be allocated. So no need care about the node idle resource
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

	return nil
}
