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
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/cli/job"
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
	return "preempt"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Preempt ...")
	defer klog.V(3).Infof("Leaving Preempt ...")

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	var underRequest []*api.JobInfo
	queues := map[api.QueueID]*api.QueueInfo{}

	clearElasticTaskForPreemptorJobs(ssn)

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

		// check job if starting for more resources.
		if ssn.JobStarving(job) {
			if _, found := preemptorsMap[job.Queue]; !found {
				preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			preemptorsMap[job.Queue].Push(job)
			underRequest = append(underRequest, job)
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
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
			assigned := false
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

				if preempted, _ := preempt(ssn, stmt, preemptor, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if task.Status != api.Running {
						return false
					}
					// Ignore task with empty resource request.
					if task.Resreq.IsEmpty() {
						return false
					}
					job, found := ssn.Jobs[task.Job]
					if !found {
						return false
					}
					// Preempt other jobs within queue
					return job.Queue == preemptorJob.Queue && preemptor.Job != task.Job
				}, ph); preempted {
					assigned = true
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
				assigned, _ := preempt(ssn, stmt, preemptor, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if task.Status != api.Running {
						return false
					}
					// Ignore task with empty resource request.
					if task.Resreq.IsEmpty() {
						return false
					}
					// Preempt tasks within job.
					return preemptor.Job == task.Job
				}, ph)
				stmt.Commit()

				// If no preemption, next job.
				if !assigned {
					break
				}
			}
		}
	}

	// call victimTasksFn to evict tasks
	victimTasks(ssn)
}

func (alloc *Action) UnInitialize() {}

func preempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	filter func(*api.TaskInfo) bool,
	predicateHelper util.PredicateHelper,
) (bool, error) {
	assigned := false

	allNodes := ssn.NodeList

	predicateNodes, _ := predicateHelper.PredicateNodes(preemptor, allNodes, ssn.PredicateFn)

	nodeScores := util.PrioritizeNodes(preemptor, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

	selectedNodes := util.SortNodes(nodeScores)
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

		victimsQueue := util.NewPriorityQueue(func(l, r interface{}) bool {
			return !ssn.TaskOrderFn(l, r)
		})
		for _, victim := range victims {
			victimsQueue.Push(victim)
		}
		// Preempt victims for tasks, pick lowest priority task first.
		preempted := api.EmptyResource()

		for !victimsQueue.Empty() {
			// If reclaimed enough resources, break loop to avoid Sub panic.
			if preemptor.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
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

		metrics.RegisterPreemptionAttempts()
		klog.V(3).Infof("Preempted <%v> for Task <%s/%s> requested <%v>.",
			preempted, preemptor.Namespace, preemptor.Name, preemptor.InitResreq)

		if preemptor.InitResreq.LessEqual(node.FutureIdle(), api.Zero) {
			if err := stmt.Pipeline(preemptor, node.Name); err != nil {
				klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
					preemptor.Namespace, preemptor.Name, node.Name)
			}

			// Ignore pipeline error, will be corrected in next scheduling loop.
			assigned = true

			break
		}
	}

	return assigned, nil
}

func victimTasks(ssn *framework.Session) {
	stmt := framework.NewStatement(ssn)
	victimTasks := ssn.VictimTasks()
	for _, victim := range victimTasks {
		if err := stmt.Evict(victim.Clone(), "evict"); err != nil {
			klog.Errorf("Failed to evict Task <%s/%s>: %v",
				victim.Namespace, victim.Name, err)
			continue
		}
	}
	stmt.Commit()
}

func clearElasticTaskForPreemptorJobs(ssn *framework.Session) {
	klog.V(3).Infof("Enter clearElasticTaskInElasticJob ...")
	defer klog.V(3).Infof("Leaving clearElasticTaskInElasticJob ...")

	starvingJobs := map[api.QueueID]*util.PriorityQueue{}
	readyJobs := map[api.QueueID]*util.PriorityQueue{}

	queues := map[api.QueueID]*api.QueueInfo{}

	for _, job := range ssn.Jobs {
		//if job.IsPending() {
		//	continue
		//}
		if queue, found := ssn.Queues[job.Queue]; !found {
			continue
		} else if _, existed := queues[queue.UID]; !existed {
			klog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)
			queues[queue.UID] = queue
		}

		// check job if starting for more resources.
		if ssn.JobStarving(job) {
			if _, found := starvingJobs[job.Queue]; !found {
				starvingJobs[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			klog.V(5).Infof("session id <%v>, job <%v> Allocated <%v>  JobStarving <%v>.", ssn.UID, job.Name, job.Allocated, ssn.JobStarving(job))
			klog.V(5).Infof("add <%v> into starvingJobs, job.CheckTaskMinAvailablePipelined <%v> job.WaitingTaskNum <%v> job.ReadyTaskNum <%v>.", job.Name, job.CheckTaskMinAvailablePipelined(), job.WaitingTaskNum(), job.ReadyTaskNum())
			starvingJobs[job.Queue].Push(job)
		} else if ssn.JobReady(job) {
			if _, found := readyJobs[job.Queue]; !found {
				readyJobs[job.Queue] = util.NewPriorityQueue(func(l interface{}, r interface{}) bool {
					return !ssn.JobOrderFn(l, r)
				})
			}
			klog.V(5).Infof("add <%v> into readyJobs, job.CheckTaskMinAvailablePipelined <%v> job.WaitingTaskNum <%v> job.ReadyTaskNum <%v>.", job.Name, job.CheckTaskMinAvailablePipelined(), job.WaitingTaskNum(), job.ReadyTaskNum())
			readyJobs[job.Queue].Push(job)
		}
	}

	for _, queue := range queues {
		for {
			preemptorJobs := starvingJobs[queue.UID]
			preempteeJobs := readyJobs[queue.UID]

			// If no preemptors, no preemption.
			if preemptorJobs == nil || preemptorJobs.Empty() {
				klog.V(4).Infof("No preemptors in Queue <%s>, break.", queue.Name)
				break
			}
			// If no preemptees, no preemption.
			if preempteeJobs == nil || preempteeJobs.Empty() {
				klog.V(4).Infof("No preemptees in Queue <%s>, break.", queue.Name)
				break
			}

			preemptorJob := preemptorJobs.Pop().(*api.JobInfo)
			klog.V(4).Infof("try clear elastic task for preemptor <%v> in Queue <%s>.", preemptorJob.Name, preemptorJob.Queue)
			elasticResourceInQueue := ssn.UnderElasticResources(queue)
			klog.V(4).Infof("elasticResourceInQueue <%v> in Queue <%s>.", elasticResourceInQueue, queue.Name)
			if preemptorJob.Allocated.IsEmpty() && elasticResourceInQueue.Less(preemptorJob.GetMinResources(), api.Zero) {
				klog.V(3).Infof("elastic resource in Queue <%s> is <%v>, can not run for Job <%s/%s> minResources <%v>",
					queue.Name, elasticResourceInQueue, preemptorJob.Namespace, preemptorJob.Name, preemptorJob.GetMinResources())
				continue
			}
			clearElasticTaskForPreemptorJob(ssn, preemptorJob, preempteeJobs)
		}
	}

}

func clearElasticTaskForPreemptorJob(ssn *framework.Session, preemptorJob *api.JobInfo, preempteeJobs *util.PriorityQueue) {
	klog.V(4).Infof("try clear elastic task for preemptor <%v> in Queue <%s>.", preemptorJob.Name, preemptorJob.Queue)
	preempteeResources := api.EmptyResource()
	for !preempteeJobs.Empty() {
		if preemptorJob.GetMinResources().LessEqual(preempteeResources, api.Zero) {
			break
		}
		preempteeJob := preempteeJobs.Pop().(*api.JobInfo)
		defer preempteeJobs.Push(preempteeJob)
		preempteeResource := clearElasticJob(ssn, preemptorJob, preempteeJob)
		klog.V(3).Infof("collect elastic resource <%v> from job <%v>",
			preempteeResource, preemptorJob.Name)
		preempteeResources.Add(preempteeResource)
	}
}
func clearElasticJob(ssn *framework.Session, preemptorJob *api.JobInfo, preempteeJob *api.JobInfo) *api.Resource {
	preempteeResources := api.EmptyResource()
	elasticNum := preempteeJob.ElasticTaskNum()
	klog.V(4).Infof("session id %v, try preempt job <%v> task for <%v>, elasticNum <%v>", ssn.UID, preempteeJob.Name, preemptorJob.Name, elasticNum)
	if elasticNum <= 0 {
		klog.V(3).Infof("job <%v> elasticNum is <%v>,ignore clear elastic task", preempteeJob.Name, elasticNum)
		return preempteeResources
	}
	preempteeTasks := util.NewPriorityQueue(func(l interface{}, r interface{}) bool {
		return !ssn.TaskOrderFn(l, r)
	})
	// sort task
	for status, tasks := range preempteeJob.TaskStatusIndex {
		if !api.AllocatedStatus(status) {
			continue
		}
		for _, t := range tasks {
			preempteeTasks.Push(t)
		}
	}

	for elasticNum > 0 && !preempteeTasks.Empty() {
		klog.V(4).Infof("preemptorJob <%v> MinResources <%v> <= preempteeResources <%v> = %v",
			preemptorJob.Name, preemptorJob.GetMinResources(), preempteeResources, preemptorJob.GetMinResources().LessEqual(preempteeResources, api.Zero))
		if preemptorJob.GetMinResources().LessEqual(preempteeResources, api.Zero) {
			break
		}
		t := preempteeTasks.Pop().(*api.TaskInfo)
		klog.V(3).Infof("Try to preempt Task <%s/%s> because job <%v> is elastic and queue <%v> is overused",
			t.Namespace, t.Name, job.Name, preemptorJob.Queue)
		if err := ssn.Evict(t, "preempt"); err != nil {
			klog.Errorf("Failed to preempt Task <%s/%s>: %v",
				t.Namespace, t.Name, err)
			continue
		}
		elasticNum--
		preempteeResources.Add(t.InitResreq)
	}
	return preempteeResources
}
