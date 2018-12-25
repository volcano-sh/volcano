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
	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/util"
)

type reclaimAction struct {
	ssn *framework.Session
}

func New() *reclaimAction {
	return &reclaimAction{}
}

func (alloc *reclaimAction) Name() string {
	return "reclaim"
}

func (alloc *reclaimAction) Initialize() {}

func (alloc *reclaimAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Reclaim ...")
	defer glog.V(3).Infof("Leaving Reclaim ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	glog.V(3).Infof("There are <%d> Jobs and <%d> Queues in total for scheduling.",
		len(ssn.Jobs), len(ssn.Queues))

	var underRequest []*api.JobInfo
	for _, job := range ssn.Jobs {
		if queue, found := ssn.QueueIndex[job.Queue]; !found {
			glog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else {
			glog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)
			queues.Push(queue)
		}

		if len(job.TaskStatusIndex[api.Pending]) != 0 {
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

	for {
		// If no queues, break
		if queues.Empty() {
			break
		}

		var job *api.JobInfo
		var task *api.TaskInfo

		queue := queues.Pop().(*api.QueueInfo)
		if ssn.Overused(queue) {
			glog.V(3).Infof("Queue <%s> is overused, ignore it.", queue.Name)
			continue
		}

		// Found "high" priority job
		if jobs, found := preemptorsMap[queue.UID]; !found || jobs.Empty() {
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

		resreq := task.Resreq.Clone()
		reclaimed := api.EmptyResource()

		assigned := false

		for _, n := range ssn.Nodes {
			// If predicates failed, next node.
			if err := ssn.PredicateFn(task, n); err != nil {
				continue
			}

			glog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.",
				task.Namespace, task.Name, n.Name)

			var reclaimees []*api.TaskInfo
			for _, task := range n.Tasks {
				// Ignore non running task.
				if task.Status != api.Running {
					continue
				}

				if j, found := ssn.JobIndex[task.Job]; !found {
					continue
				} else if j.Queue != job.Queue {
					// Clone task to avoid modify Task's status on node.
					reclaimees = append(reclaimees, task.Clone())
				}
			}
			victims := ssn.Reclaimable(task, reclaimees)

			if len(victims) == 0 {
				glog.V(3).Infof("No victims on Node <%s>.", n.Name)
				continue
			}

			// If not enough resource, continue
			allRes := api.EmptyResource()
			for _, v := range victims {
				allRes.Add(v.Resreq)
			}
			if allRes.Less(resreq) {
				glog.V(3).Infof("Not enough resource from victims on Node <%s>.", n.Name)
				continue
			}

			// Reclaim victims for tasks.
			for _, reclaimee := range victims {
				glog.Errorf("Try to reclaim Task <%s/%s> for Tasks <%s/%s>",
					reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name)
				if err := ssn.Evict(reclaimee, "reclaim"); err != nil {
					glog.Errorf("Failed to reclaim Task <%s/%s> for Tasks <%s/%s>: %v",
						reclaimee.Namespace, reclaimee.Name, task.Namespace, task.Name, err)
					continue
				}
				reclaimed.Add(reclaimee.Resreq)
				// If reclaimed enough resources, break loop to avoid Sub panic.
				if resreq.LessEqual(reclaimee.Resreq) {
					break
				}
				resreq.Sub(reclaimee.Resreq)
			}

			glog.V(3).Infof("Reclaimed <%v> for task <%s/%s> requested <%v>.",
				reclaimed, task.Namespace, task.Name, task.Resreq)

			if err := ssn.Pipeline(task, n.Name); err != nil {
				glog.Errorf("Failed to pipline Task <%s/%s> on Node <%s>",
					task.Namespace, task.Name, n.Name)
			}

			// Ignore error of pipeline, will be corrected in next scheduling loop.
			assigned = true

			break
		}

		if assigned {
			queues.Push(queue)
		}
	}

}

func (ra *reclaimAction) UnInitialize() {
}
