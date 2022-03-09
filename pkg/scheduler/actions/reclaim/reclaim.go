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
	"k8s.io/klog"

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
	klog.V(3).Infof("Enter Reclaim ...")
	defer klog.V(3).Infof("Leaving Reclaim ...")

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
			klog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else if _, existed := queueMap[queue.UID]; !existed {
			klog.V(4).Infof("Added Queue <%s> for Job <%s/%s>", queue.Name, job.Namespace, job.Name)
			queueMap[queue.UID] = queue
			queues.Push(queue)
		}

		if len(job.TaskStatusIndex[api.Pending]) != 0 {
			if _, found := preemptorsMap[job.Queue]; !found {
				preemptorsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			preemptorsMap[job.Queue].Push(job)
			preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			for _, task := range job.TaskStatusIndex[api.Pending] {
				preemptorTasks[job.UID].Push(task)
			}
		}
	}

	for {
		// If no queues, break
		if queues.Empty() {
			break
		}

		var job *api.JobInfo
		var task *api.TaskInfo

		queue := queues.Pop().(*api.QueueInfo)
		if ssn.Overused(queue) {
			klog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		// Found "high" priority job
		jobs, found := preemptorsMap[queue.UID]
		if !found || jobs.Empty() {
			continue
		} else {
			job = jobs.Pop().(*api.JobInfo)
		}

		// Found "high" priority task to reclaim others
		if tasks, found := preemptorTasks[job.UID]; !found || tasks.Empty() {
			continue
		} else {
			task = tasks.Pop().(*api.TaskInfo)
		}

		if !ssn.Allocatable(queue, task) {
			klog.V(3).Infof("Queue <%s> is overused when considering task <%s>, ignore it.", queue.Name, task.Name)
			continue
		}

		assigned := false
		for _, n := range ssn.Nodes {
			// If predicates failed, next node.
			if err := ssn.PredicateFn(task, n); err != nil {
				continue
			}

			klog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.",
				task.Namespace, task.Name, n.Name)

			var reclaimees []*api.TaskInfo
			for _, task := range n.Tasks {
				// Ignore non running task.
				if task.Status != api.Running {
					continue
				}

				if j, found := ssn.Jobs[task.Job]; !found {
					continue
				} else if j.Queue != job.Queue {
					q := ssn.Queues[j.Queue]
					if !q.Reclaimable() {
						continue
					}
					// Clone task to avoid modify Task's status on node.
					reclaimees = append(reclaimees, task.Clone())
				}
			}
			victims := ssn.Reclaimable(task, reclaimees)

			if err := util.ValidateVictims(task, n, victims); err != nil {
				klog.V(3).Infof("No validated victims on Node <%s>: %v", n.Name, err)
				continue
			}

			resreq := task.InitResreq.Clone()
			reclaimed := api.EmptyResource()

			// Reclaim victims for tasks.
			for _, reclaimee := range victims {
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
			}

			klog.V(3).Infof("Reclaimed <%v> for task <%s/%s> requested <%v>.",
				reclaimed, task.Namespace, task.Name, task.InitResreq)

			if task.InitResreq.LessEqual(reclaimed, api.Zero) {
				if err := ssn.Pipeline(task, n.Name); err != nil {
					klog.Errorf("Failed to pipeline Task <%s/%s> on Node <%s>",
						task.Namespace, task.Name, n.Name)
				}

				// Ignore error of pipeline, will be corrected in next scheduling loop.
				assigned = true

				break
			}
		}

		if assigned {
			jobs.Push(job)
		}
		queues.Push(queue)
	}
}

func (ra *Action) UnInitialize() {
}
