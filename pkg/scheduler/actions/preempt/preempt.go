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
	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/util"
)

type preemptAction struct {
	ssn *framework.Session
}

func New() *preemptAction {
	return &preemptAction{}
}

func (alloc *preemptAction) Name() string {
	return "preempt"
}

func (alloc *preemptAction) Initialize() {}

func (alloc *preemptAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Preempt ...")
	defer glog.V(3).Infof("Leaving Preempt ...")

	jobRevOrderFn := func(l, r interface{}) bool {
		return !ssn.JobOrderFn(l, r)
	}

	taskRevOrderFn := func(l, r interface{}) bool {
		return !ssn.TaskOrderFn(l, r)
	}

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preempteesMap := map[api.QueueID]*util.PriorityQueue{}

	preemptorTasks := map[api.JobID]*util.PriorityQueue{}
	preempteeTasks := map[api.JobID]*util.PriorityQueue{}

	var underRequest []*api.JobInfo
	for _, job := range ssn.Jobs {
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

		// If no running tasks in job, skip it as preemptee.
		if len(job.TaskStatusIndex[api.Running]) != 0 {
			if _, found := preempteesMap[job.Queue]; !found {
				preempteesMap[job.Queue] = util.NewPriorityQueue(jobRevOrderFn)
			}
			preempteesMap[job.Queue].Push(job)

			preempteeTasks[job.UID] = util.NewPriorityQueue(taskRevOrderFn)
			// TODO (k82cn): it's better to also includes Binding/Bound tasks.
			for _, task := range job.TaskStatusIndex[api.Running] {
				preempteeTasks[job.UID].Push(task)
			}

			glog.V(3).Infof("Put job <%s/%s> as preemptee with %d tasks.",
				job.Namespace, job.Name, preempteeTasks[job.UID].Len())
		}
	}

	// Preemption between Jobs within Queue.
	for _, queue := range ssn.Queues {
		for {
			preemptors := preemptorsMap[queue.UID]
			preemptees := preempteesMap[queue.UID]

			// If no preemptors nor preemptees, no preemption.
			if (preemptors == nil || preemptors.Empty()) ||
				(preemptees == nil || preemptees.Empty()) {
				glog.V(3).Infof("No preemptors nor preemptees in Queue <%s>, break.", queue.Name)
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)

			// If not preemptor tasks, next job.
			if preemptorTasks[preemptorJob.UID].Empty() {
				glog.V(3).Infof("No preemptor task in job <%s/%s>.",
					preemptorJob.Namespace, preemptorJob.Name)
				continue
			}

			// Find the preemptee job:
			//   - More than one running tasks
			//   - Different job
			var preempteeJob *api.JobInfo
			for !preemptees.Empty() {
				preempteeJob = preemptees.Pop().(*api.JobInfo)

				// If found itself, then no preemptees anymore.
				if preemptorJob.UID == preempteeJob.UID {
					glog.V(3).Infof("Can not preempt itself <%s/%s>.",
						preemptorJob.Namespace, preemptorJob.Name)
					preempteeJob = nil
					break
				}

				if preempteeTasks[preempteeJob.UID].Empty() {
					glog.V(3).Infof("No preemptable tasks in job <%s/%s>, next",
						preempteeJob.Namespace, preempteeJob.Name)

					preempteeJob = nil
					continue
				}

				// If found a preemptee job, break
				break
			}

			if preempteeJob == nil {
				glog.V(3).Infof("Can not found preemptee job for %v/%v",
					preemptorJob.Namespace, preemptorJob.Name)

				break
			}

			glog.V(3).Infof("The preemptor is %v/%v, the preemptee is %v/%v",
				preemptorJob.Namespace, preemptorJob.Name,
				preempteeJob.Namespace, preempteeJob.Name)

			preemptor := preemptorTasks[preemptorJob.UID].Pop().(*api.TaskInfo)
			preemptee := preempteeTasks[preempteeJob.UID].Pop().(*api.TaskInfo)

			preempted := false

			if ssn.Preemptable(preemptor, preemptee) {
				if err := ssn.Preempt(preemptor, preemptee); err != nil {
					glog.Errorf("Failed to evict task %v/%v for task %v/%v: %v",
						preemptee.Namespace, preemptee.Name,
						preemptor.Namespace, preemptor.Name, err)
				} else {
					preempted = true
				}
			} else {
				glog.V(3).Infof("Can not preempt task <%v/%v> for task <%v/%v>",
					preemptee.Namespace, preemptee.Name,
					preemptor.Namespace, preemptor.Name)
			}

			// If preempted resource, put it back to the queue.
			if preempted {
				preemptors.Push(preemptorJob)
			} else {
				// If the preemptee is not preempted, push it back for other to preempt.
				preempteeTasks[preempteeJob.UID].Push(preemptee)
			}

			preemptees.Push(preempteeJob)
		}

		// Preemption between Task within Job.
		for _, job := range underRequest {
			for {
				if _, found := preempteeTasks[job.UID]; !found {
					break
				}

				if _, found := preemptorTasks[job.UID]; !found {
					break
				}

				if preemptorTasks[job.UID].Empty() || preempteeTasks[job.UID].Empty() {
					break
				}

				preemptor := preemptorTasks[job.UID].Pop().(*api.TaskInfo)
				preemptee := preempteeTasks[job.UID].Pop().(*api.TaskInfo)

				if ssn.TaskCompareFns(preemptor, preemptee) < 0 {
					if err := ssn.Preempt(preemptor, preemptee); err != nil {
						glog.Errorf("Failed to rebalance tasks in job <%v/%v>: %v",
							job.Namespace, job.Name, err)
						break
					}
					// If preempted, continue to check next pair.
					continue
				}

				// If no preemption, next job.
				break
			}
		}
	}
}

func (alloc *preemptAction) UnInitialize() {}
