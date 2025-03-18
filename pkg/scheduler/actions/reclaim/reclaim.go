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

package reclaim

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (ra *Action) Name() string {
	return "reclaim"
}

func (ra *Action) Initialize() {}

func (ra *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Reclaim ...")
	defer klog.V(5).Infof("Leaving Reclaim ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	klog.V(3).Infof("There are <%d> Jobs and <%d> Queues in total for scheduling.",
		len(ssn.Jobs), len(ssn.Queues))

	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip reclaim, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if queue, found := ssn.Queues[job.Queue]; !found {
			klog.Errorf("Failed to find Queue <%s> for Job <%s/%s>", job.Queue, job.Namespace, job.Name)
			continue
		} else if _, existed := queueMap[queue.UID]; !existed {
			klog.V(4).Infof("Added Queue <%s> for Job <%s/%s>", queue.Name, job.Namespace, job.Name)
			queueMap[queue.UID] = queue
			queues.Push(queue)
		}

		if ssn.JobStarving(job) {
			if _, found := preemptorsMap[job.Queue]; !found {
				preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			preemptorsMap[job.Queue].Push(job)
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				if task.SchGated {
					continue
				}
				preemptorTasks[job.UID].Push(task)
			}
		}
	}

	for !queues.Empty() {

		queue := queues.Pop().(*api.QueueInfo)
		jobs, found := preemptorsMap[queue.UID]
		if !found || jobs.Empty() {
			continue
		}
		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}
		jobsToReQueue := util.NewPriorityQueue(ssn.JobOrderFn)
		reclaimResources := false
		for !jobs.Empty() {
			job := jobs.Pop().(*api.JobInfo)
			tasks, found := preemptorTasks[job.UID]
			if !found || tasks.Empty() || !ssn.JobStarving(job) {
				continue
			}
			taskToReQueue := util.NewPriorityQueue(ssn.TaskOrderFn)
			jobAssigned := false
			for !tasks.Empty() {

				task := tasks.Pop().(*api.TaskInfo)
				if task.Pod.Spec.PreemptionPolicy != nil && *task.Pod.Spec.PreemptionPolicy == v1.PreemptNever {
					klog.V(3).Infof("Task %s/%s is not eligible to preempt other tasks due to preemptionPolicy is Never", task.Namespace, task.Name)
					taskToReQueue.Push(task)
					continue
				}
				// In allocate action we need check all the ancestor queues' capability but in reclaim action we should just check current queue's capability, and reclaim happens when queue not allocatable so we just need focus on the reclaim here.
				// So it's more descriptive to user preempt related semantics.
				if !ssn.Preemptive(queue, task) {
					klog.V(3).Infof("Queue <%s> can not reclaim by preempt others when considering task <%s> , ignore it.", queue.Name, task.Name)
					taskToReQueue.Push(task)
					continue
				}
				if assigned := ra.reclaimFromTask(ssn, job, task); assigned {
					jobAssigned = true
					reclaimResources = true
				} else {
					taskToReQueue.Push(task)
				}

			}
			if !jobAssigned {
				// 将未处理的任务放回原队列
				for !taskToReQueue.Empty() {
					preemptorTasks[job.UID].Push(taskToReQueue.Pop().(*api.TaskInfo))
				}
				jobsToReQueue.Push(job)
			}
		}
		if !reclaimResources && !jobsToReQueue.Empty() {
			for !jobsToReQueue.Empty() {
				preemptorsMap[queue.UID].Push(jobsToReQueue.Pop())
			}
			// only if there are still unprocessed jobs in the queue
			if !preemptorsMap[queue.UID].Empty() {
				queues.Push(queue)
			}
		}
	}

}

func (ra *Action) reclaimFromTask(ssn *framework.Session, jobInfo *api.JobInfo, task *api.TaskInfo) bool {
	if err := ssn.PrePredicateFn(task); err != nil {
		klog.V(3).Infof("PrePredicate for task %s/%s failed for: %v", task.Namespace, task.Name, err)
		return false
	}
	totalNodes := ssn.GetUnschedulableAndUnresolvableNodesForTask(task)
	for _, node := range totalNodes {
		if assigned := ra.reclaimFromNode(ssn, task, jobInfo, node); assigned {
			return true
		}
	}
	return false
}
func (ra *Action) reclaimFromNode(ssn *framework.Session, task *api.TaskInfo, jobInfo *api.JobInfo, node *api.NodeInfo) bool {
	if err := ssn.PredicateForPreemptAction(task, node); err != nil {
		klog.V(4).Infof("Reclaim predicate for task %s/%s on node %s return error %v ", task.Namespace, task.Name, node.Name, err)
		return false
	}
	klog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.", task.Namespace, task.Name, node.Name)
	var reclaimees = ra.findReclaimableTasks(ssn, jobInfo, node)
	if len(reclaimees) == 0 {
		klog.V(4).Infof("No reclaimees on Node <%s>.", node.Name)
	}
	victims := ssn.Reclaimable(task, reclaimees)
	if err := util.ValidateVictims(task, node, victims); err != nil {
		klog.V(3).Infof("No validated victims on Node <%s>: %v", node.Name, err)
		return false
	}
	if success := ra.performReclaim(ssn, task, node, victims); success {
		klog.V(3).Infof("Reclaimed <%v> for task <%s/%s> successed.", node.Name, task.Name, task.Job)
		return true
	}
	return false

}
func (ra *Action) findReclaimableTasks(ssn *framework.Session, jobInfo *api.JobInfo, node *api.NodeInfo) []*api.TaskInfo {
	var reclaimees []*api.TaskInfo
	for _, task := range node.Tasks {
		// Ignore non running task.
		if task.Status != api.Running || !task.Preemptable {
			continue
		}
		if job, exists := ssn.Jobs[task.Job]; exists && job.Queue != jobInfo.Queue {
			if queue := ssn.Queues[job.Queue]; queue != nil && queue.Reclaimable() {
				reclaimee := task.Clone()
				reclaimee.Resreq = task.Resreq.Clone()
				reclaimees = append(reclaimees, reclaimee)
				klog.V(4).Infof("Find Reclaimable Task <%s/%s> on Node <%s>.", task.Namespace, task.Name, node.Name)
			}
		}
	}
	if len(reclaimees) == 0 {
		klog.V(4).Infof("No reclaimees on Node <%s>.", node.Name)
		return nil
	}
	return reclaimees
}
func (ra *Action) performReclaim(ssn *framework.Session, task *api.TaskInfo, node *api.NodeInfo, victims []*api.TaskInfo) bool {
	if err := util.ValidateVictims(task, node, victims); err != nil {
		klog.V(3).Infof("No validated victims on Node <%s>: %v", node.Name, err)
		return false
	}
	victimsQueue := ssn.BuildVictimsPriorityQueue(victims, task)
	resreq := task.InitResreq.Clone()
	reclaimed := api.EmptyResource()
	success := false
	for !victimsQueue.Empty() {
		reclaimee := victimsQueue.Pop().(*api.TaskInfo)
		klog.Errorf("Try to reclaim Task <%s/%s> for Tasks <%s/%s>",
			reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name)
		if err := ssn.Evict(reclaimee, "reclaim"); err != nil {
			klog.Errorf("Failed to reclaim Task <%s/%s> for Tasks <%s/%s>: %v",
				reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name, err)
			continue
		}
		reclaimed.Add(reclaimee.Resreq)
		// If reclaimed enough resources, break loop to avoid Sub panic.
		if resreq.LessEqual(reclaimed, api.Zero) {
			break
		}
		klog.V(3).Infof("Reclaimed <%v> for task <%s/%s> requested <%v>.",
			reclaimed, task.Namespace, task.Name, task.InitResreq)
		if task.InitResreq.LessEqual(reclaimed, api.Zero) {
			if err := ssn.Pipeline(task, node.Name); err != nil {
				klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
					task.Namespace, task.Name, node.Name)
				return false
			}
			// Ignore error of pipeline, will be corrected in next scheduling loop.
			success = true
			break
		}
	}
	return success
}

func (ra *Action) UnInitialize() {
}
