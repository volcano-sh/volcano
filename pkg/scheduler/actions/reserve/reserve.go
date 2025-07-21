/*
Copyright 2019 The Kubernetes Authors.

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

package reserve

import (
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (reserve *Action) Name() string {
	return "reserve"
}

func (reserve *Action) Initialize() {}

func (reserve *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Reserve ...")
	defer klog.V(5).Infof("Leaving Reserve ...")

	for _, job := range ssn.Jobs {
		if !job.IsUseReservation() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip allocate, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		jobs := util.NewPriorityQueue(ssn.JobOrderFn)
		jobs.Push(job)

		klog.V(3).Infof("Attempting to swap reservation for actual tasks in job <%s/%s>", job.Namespace, job.Name)
		stmt := framework.NewStatement(ssn)

		pendingTasks := util.NewPriorityQueue(ssn.TaskOrderFn)
		for _, task := range job.TaskStatusIndex[api.Pending] {
			pendingTasks.Push(task)
		}

		for !pendingTasks.Empty() {
			actualTask := pendingTasks.Pop().(*api.TaskInfo)

			if job.TaskHasFitErrors(actualTask) {
				klog.V(5).Infof("Task %s with role spec %s has already predicated failed, skip", actualTask.Name, actualTask.TaskRole)
				continue
			}

			if err := ssn.PrePredicateFn(actualTask); err != nil {
				klog.V(3).Infof("PrePredicate for task %s/%s failed for: %v", actualTask.Namespace, actualTask.Name, err)
				fitErrors := api.NewFitErrors()
				for _, ni := range ssn.NodeList {
					fitErrors.SetNodeError(ni.Name, err)
				}
				job.NodesFitErrors[actualTask.UID] = fitErrors
				break
			}

			reservationTask := actualTask.ReservationTaskInfo
			if reservationTask == nil {
				klog.Warningf("Task <%s/%s> wants to use reservation but has no ReservationTaskInfo", actualTask.Namespace, actualTask.Name)
				continue
			}
			reservedNodeName := reservationTask.NodeName

			if reservedNodeName == "" {
				klog.Warningf("Reservation info for task <%s/%s> does not specify a node", actualTask.Namespace, actualTask.Name)
				continue
			}

			reservedNode, found := ssn.Nodes[reservedNodeName]
			if !found {
				klog.Warningf("Reserved node '%s' for task <%s/%s> not found in current session", reservedNodeName, actualTask.Namespace, actualTask.Name)
				continue
			}

			if err := stmt.UnAllocate(reservationTask); err != nil {
				klog.Errorf("Failed to unAllocate reservation task %v from node %v, err: %v", reservationTask.UID, reservedNode.Name, err)
				continue
			}

			if err := stmt.Allocate(actualTask, reservedNode); err != nil {
				klog.Errorf("Failed to allocate actual task %v to its reserved node %v, err: %v", actualTask.UID, reservedNode.Name, err)
			} else {
				klog.V(4).Infof("Allocated actual task <%s/%s> to node <%s>, effectively replacing the reservation.", actualTask.Namespace, actualTask.Name, reservedNode.Name)
				metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
				metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
			}

			if ssn.JobReady(job) && !pendingTasks.Empty() {
				jobs.Push(job)
				klog.V(3).Infof("Job <%s/%s> is ready, but still has pending tasks. Pipelining.", job.Namespace, job.Name)
				break
			}
		}

		if ssn.JobReady(job) {
			stmt.Commit()
		} else {
			if !ssn.JobPipelined(job) {
				stmt.Discard()
			} else {
				stmt.Commit()
			}
		}
	}
}

func (reserve *Action) UnInitialize() {}
