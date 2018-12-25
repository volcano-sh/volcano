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

	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/util"
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

	preemptorsMap := map[api.QueueID]*util.PriorityQueue{}
	preemptorTasks := map[api.JobID]*util.PriorityQueue{}

	var underRequest []*api.JobInfo
	var queues []*api.QueueInfo
	for _, job := range ssn.Jobs {
		if queue, found := ssn.QueueIndex[job.Queue]; !found {
			continue
		} else {
			glog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)
			queues = append(queues, queue)
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

	// Preemption between Jobs within Queue.
	for _, queue := range queues {
		for {
			preemptors := preemptorsMap[queue.UID]

			// If no preemptors, no preemption.
			if preemptors == nil || preemptors.Empty() {
				glog.V(3).Infof("No preemptors in Queue <%s>, break.", queue.Name)
				break
			}

			preemptorJob := preemptors.Pop().(*api.JobInfo)

			stmt := ssn.Statement()
			assigned := false
			for {
				// If not preemptor tasks, next job.
				if preemptorTasks[preemptorJob.UID].Empty() {
					glog.V(3).Infof("No preemptor task in job <%s/%s>.",
						preemptorJob.Namespace, preemptorJob.Name)
					break
				}

				preemptor := preemptorTasks[preemptorJob.UID].Pop().(*api.TaskInfo)

				if preempted, _ := preempt(ssn, stmt, preemptor, ssn.Nodes, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if task.Status != api.Running {
						return false
					}

					job, found := ssn.JobIndex[task.Job]
					if !found {
						return false
					}
					// Preempt other jobs within queue
					return job.Queue == preemptorJob.Queue && preemptor.Job != task.Job
				}); preempted {
					assigned = true
				}

				// If job not ready, keep preempting
				if ssn.JobReady(preemptorJob) {
					stmt.Commit()
					break
				}
			}

			// If job not ready after try all tasks, next job.
			if !ssn.JobReady(preemptorJob) {
				stmt.Discard()
				continue
			}

			if assigned {
				preemptors.Push(preemptorJob)
			}
		}

		// Preemption between Task within Job.
		for _, job := range underRequest {
			for {
				if _, found := preemptorTasks[job.UID]; !found {
					break
				}

				if preemptorTasks[job.UID].Empty() {
					break
				}

				preemptor := preemptorTasks[job.UID].Pop().(*api.TaskInfo)

				stmt := ssn.Statement()
				assigned, _ := preempt(ssn, stmt, preemptor, ssn.Nodes, func(task *api.TaskInfo) bool {
					// Ignore non running task.
					if task.Status != api.Running {
						return false
					}

					// Preempt tasks within job.
					return preemptor.Job == task.Job
				})
				stmt.Commit()

				// If no preemption, next job.
				if !assigned {
					break
				}
			}
		}
	}
}

func (alloc *preemptAction) UnInitialize() {}

func preempt(
	ssn *framework.Session,
	stmt *framework.Statement,
	preemptor *api.TaskInfo,
	nodes []*api.NodeInfo,
	filter func(*api.TaskInfo) bool,
) (bool, error) {
	resreq := preemptor.Resreq.Clone()
	preempted := api.EmptyResource()

	assigned := false

	for _, node := range nodes {
		if err := ssn.PredicateFn(preemptor, node); err != nil {
			continue
		}

		glog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.",
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

		if err := validateVictims(victims, resreq); err != nil {
			glog.V(3).Infof("No validated victims on Node <%s>: %v", node.Name, err)
			continue
		}

		// Preempt victims for tasks.
		for _, preemptee := range victims {
			glog.Errorf("Try to preempt Task <%s/%s> for Tasks <%s/%s>",
				preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name)
			if err := stmt.Evict(preemptee, "preempt"); err != nil {
				glog.Errorf("Failed to preempt Task <%s/%s> for Tasks <%s/%s>: %v",
					preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name, err)
				continue
			}
			preempted.Add(preemptee.Resreq)
			// If reclaimed enough resources, break loop to avoid Sub panic.
			if resreq.LessEqual(preemptee.Resreq) {
				break
			}
			resreq.Sub(preemptee.Resreq)
		}

		glog.V(3).Infof("Preempted <%v> for task <%s/%s> requested <%v>.",
			preempted, preemptor.Namespace, preemptor.Name, preemptor.Resreq)

		if err := stmt.Pipeline(preemptor, node.Name); err != nil {
			glog.Errorf("Failed to pipline Task <%s/%s> on Node <%s>",
				preemptor.Namespace, preemptor.Name, node.Name)
		}

		// Ignore pipeline error, will be corrected in next scheduling loop.
		assigned = true

		break
	}

	return assigned, nil
}

func validateVictims(victims []*api.TaskInfo, resreq *api.Resource) error {
	if len(victims) == 0 {
		return fmt.Errorf("no victims")
	}

	// If not enough resource, continue
	allRes := api.EmptyResource()
	for _, v := range victims {
		allRes.Add(v.Resreq)
	}
	if allRes.Less(resreq) {
		return fmt.Errorf("not enough resources")
	}

	return nil
}
