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

	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)
		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		// Pick the next starving job in this queue.
		jobsQ, found := preemptorsMap[queue.UID]
		if !found || jobsQ.Empty() {
			// This queue has no starving jobs now, push it back for later consideration.
			queues.Push(queue)
			continue
		}
		job := jobsQ.Pop().(*api.JobInfo)

		// Pick up all its candidate tasks.
		tasksQ, ok := preemptorTasks[job.UID]
		if !ok || tasksQ.Empty() || !ssn.JobStarving(job) {
			jobsQ.Push(job)
			queues.Push(queue)
			continue
		}

		klog.V(3).Infof("Try reclaim for %d tasks of job <%s/%s>", tasksQ.Len(), job.Namespace, job.Name)
		assignedInJob := false

		for !tasksQ.Empty() {
			task := tasksQ.Pop().(*api.TaskInfo)

			if task.Pod.Spec.PreemptionPolicy != nil && *task.Pod.Spec.PreemptionPolicy == v1.PreemptNever {
				klog.V(3).Infof("Task %s/%s cannot preempt (policy Never)", task.Namespace, task.Name)
				continue
			}

			if !ssn.Preemptive(queue, task) {
				klog.V(3).Infof("Queue <%s> cannot reclaim for task <%s>, skip", queue.Name, task.Name)
				continue
			}

			if err := ssn.PrePredicateFn(task); err != nil {
				klog.V(3).Infof("PrePredicate failed for task %s/%s: %v", task.Namespace, task.Name, err)
				continue
			}

			taskReclaimed := false
			totalNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
			for _, n := range totalNodes {
				if err := ssn.PredicateForPreemptAction(task, n); err != nil {
					klog.V(4).Infof("Reclaim predicate for task %s/%s on node %s return error %v ", task.Namespace, task.Name, n.Name, err)
					continue
				}

				klog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.", task.Namespace, task.Name, n.Name)

				var reclaimees []*api.TaskInfo
				for _, taskOnNode := range n.Tasks {
					if taskOnNode.Status != api.Running || !taskOnNode.Preemptable {
						continue
					}

					if j, found := ssn.Jobs[taskOnNode.Job]; !found {
						continue
					} else if j.Queue != job.Queue {
						q := ssn.Queues[j.Queue]
						if !q.Reclaimable() {
							continue
						}
						reclaimees = append(reclaimees, taskOnNode.Clone())
					}
				}

				if len(reclaimees) == 0 {
					klog.V(4).Infof("No reclaimees on Node <%s>.", n.Name)
					continue
				}

				victims := ssn.Reclaimable(task, reclaimees)
				if err := util.ValidateVictims(task, n, victims); err != nil {
					klog.V(3).Infof("No validated victims on Node <%s>: %v", n.Name, err)
					continue
				}

				victimsQueue := ssn.BuildVictimsPriorityQueue(victims, task)
				resreq := task.InitResreq.Clone()
				reclaimed := api.EmptyResource()

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
					if resreq.LessEqual(reclaimed, api.Zero) {
						break
					}
				}

				klog.V(3).Infof("Reclaimed <%v> for task <%s/%s> requested <%v>.",
					reclaimed, task.Namespace, task.Name, task.InitResreq)

				if task.InitResreq.LessEqual(reclaimed, api.Zero) {
					if err := ssn.Pipeline(task, n.Name); err != nil {
						klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
							task.Namespace, task.Name, n.Name)
					}
					taskReclaimed = true
					assignedInJob = true
					break
				}
			}

			if taskReclaimed {
				break
			}
		}

		// Put job and queue back if there’s more to do.
		if assignedInJob {
			jobsQ.Push(job)
		}
		queues.Push(queue)
	}
}

func (ra *Action) UnInitialize() {
}
