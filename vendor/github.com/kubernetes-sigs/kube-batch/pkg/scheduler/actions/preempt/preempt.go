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

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/metrics"
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
	queues := map[api.QueueID]*api.QueueInfo{}

	for _, job := range ssn.Jobs {
		if job.PodGroup.Status.Phase == v1alpha1.PodGroupPending {
			continue
		}
		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			glog.V(4).Infof("Job <%s/%s> Queue <%s> skip preemption, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		if queue, found := ssn.Queues[job.Queue]; !found {
			continue
		} else if _, existed := queues[queue.UID]; !existed {
			glog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)
			queues[queue.UID] = queue
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
				glog.V(4).Infof("No preemptors in Queue <%s>, break.", queue.Name)
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

					job, found := ssn.Jobs[task.Job]
					if !found {
						return false
					}
					// Preempt other jobs within queue
					return job.Queue == preemptorJob.Queue && preemptor.Job != task.Job
				}); preempted {
					assigned = true
				}

				// If job is not pipelined, keep preempting
				if ssn.JobPipelined(preemptorJob) {
					stmt.Commit()
					break
				}
			}

			// If job is not pipelined after try all tasks, next job.
			if !ssn.JobPipelined(preemptorJob) {
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
	nodes map[string]*api.NodeInfo,
	filter func(*api.TaskInfo) bool,
) (bool, error) {
	assigned := false

	allNodes := util.GetNodeList(nodes)

	predicateNodes, _ := util.PredicateNodes(preemptor, allNodes, ssn.PredicateFn)

	nodeScores := util.PrioritizeNodes(preemptor, predicateNodes, ssn.BatchNodeOrderFn, ssn.NodeOrderMapFn, ssn.NodeOrderReduceFn)

	selectedNodes := util.SortNodes(nodeScores)
	for _, node := range selectedNodes {
		glog.V(3).Infof("Considering Task <%s/%s> on Node <%s>.",
			preemptor.Namespace, preemptor.Name, node.Name)

		var preemptees []*api.TaskInfo
		preempted := api.EmptyResource()
		resreq := preemptor.InitResreq.Clone()

		for _, task := range node.Tasks {
			if filter == nil {
				preemptees = append(preemptees, task.Clone())
			} else if filter(task) {
				preemptees = append(preemptees, task.Clone())
			}
		}
		victims := ssn.Preemptable(preemptor, preemptees)
		metrics.UpdatePreemptionVictimsCount(len(victims))

		if err := validateVictims(victims, resreq); err != nil {
			glog.V(3).Infof("No validated victims on Node <%s>: %v", node.Name, err)
			continue
		}

		victimsQueue := util.NewPriorityQueue(func(l, r interface{}) bool {
			return !ssn.TaskOrderFn(l, r)
		})
		for _, victim := range victims {
			victimsQueue.Push(victim)
		}
		// Preempt victims for tasks, pick lowest priority task first.
		for !victimsQueue.Empty() {
			preemptee := victimsQueue.Pop().(*api.TaskInfo)
			glog.Errorf("Try to preempt Task <%s/%s> for Tasks <%s/%s>",
				preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name)
			if err := stmt.Evict(preemptee, "preempt"); err != nil {
				glog.Errorf("Failed to preempt Task <%s/%s> for Tasks <%s/%s>: %v",
					preemptee.Namespace, preemptee.Name, preemptor.Namespace, preemptor.Name, err)
				continue
			}
			preempted.Add(preemptee.Resreq)
			// If reclaimed enough resources, break loop to avoid Sub panic.
			if resreq.LessEqual(preempted) {
				break
			}
		}

		metrics.RegisterPreemptionAttempts()
		glog.V(3).Infof("Preempted <%v> for task <%s/%s> requested <%v>.",
			preempted, preemptor.Namespace, preemptor.Name, preemptor.InitResreq)

		if preemptor.InitResreq.LessEqual(preempted) {
			if err := stmt.Pipeline(preemptor, node.Name); err != nil {
				glog.Errorf("Failed to pipline Task <%s/%s> on Node <%s>",
					preemptor.Namespace, preemptor.Name, node.Name)
			}

			// Ignore pipeline error, will be corrected in next scheduling loop.
			assigned = true

			break
		}
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
