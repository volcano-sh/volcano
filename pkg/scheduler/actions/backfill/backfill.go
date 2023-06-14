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

package backfill

import (
	"fmt"
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (backfill *Action) Name() string {
	return "backfill"
}

func (backfill *Action) Initialize() {}

func (backfill *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Backfill ...")
	defer klog.V(5).Infof("Leaving Backfill ...")

	// TODO (k82cn): When backfill, it's also need to balance between Queues.
	for _, job := range ssn.Jobs {
		if job.IsPending() {
			continue
		}

		if vr := ssn.JobValid(job); vr != nil && !vr.Pass {
			klog.V(4).Infof("Job <%s/%s> Queue <%s> skip backfill, reason: %v, message %v", job.Namespace, job.Name, job.Queue, vr.Reason, vr.Message)
			continue
		}

		for _, task := range job.TaskStatusIndex[api.Pending] {
			if task.InitResreq.IsEmpty() {
				allocated := false
				if err := prePredicateforBackfill(ssn, task, job); err != nil {
					klog.V(3).Infof("backfill %s", err.Error())
					continue
				}

				fe := api.NewFitErrors()
				// As task did not request resources, so it only need to meet predicates.
				// TODO (k82cn): need to prioritize nodes to avoid pod hole.
				for _, node := range ssn.Nodes {
					// TODO (k82cn): predicates did not consider pod number for now, there'll
					// be ping-pong case here.
					err := predicateforBackfill(ssn, task, node, fe)
					if err != nil {
						klog.V(3).Infof("backfill %s", err.Error())
						continue
					}

					klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
					if err := ssn.Allocate(task, node); err != nil {
						klog.Errorf("Failed to bind Task %v on %v in Session %v", task.UID, node.Name, ssn.UID)
						fe.SetNodeError(node.Name, err)
						continue
					}

					metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
					metrics.UpdateE2eSchedulingLastTimeByJob(job.Name, string(job.Queue), job.Namespace, time.Now())
					allocated = true
					break
				}

				if !allocated {
					job.NodesFitErrors[task.UID] = fe
				}
			}
			// TODO (k82cn): backfill for other case.
		}
	}
}

func predicateforBackfill(ssn *framework.Session, task *api.TaskInfo, node *api.NodeInfo, fe *api.FitErrors) error {
	predicateStatus, err := ssn.PredicateFn(task, node)
	if err != nil {
		fe.SetNodeError(node.Name, err)
		return fmt.Errorf("Predicates failed for task <%s/%s> on node <%s>: %v",
			task.Namespace, task.Name, node.Name, err)
	}
	for _, status := range predicateStatus {
		if status != nil && status.Code != api.Success {
			return fmt.Errorf("Predicates failed for task <%s/%s> on node <%s>: %s",
				task.Namespace, task.Name, node.Name, status.Reason)
		}
	}
	return nil
}

func prePredicateforBackfill(ssn *framework.Session, task *api.TaskInfo, job *api.JobInfo) error {
	prePredicateStatus, err := ssn.PrePredicateFn(task)
	if err != nil {
		fitErrors := api.NewFitErrors()
		for _, ni := range ssn.Nodes {
			fitErrors.SetNodeError(ni.Name, err)
		}
		job.NodesFitErrors[task.UID] = fitErrors
		return fmt.Errorf("PrePredicate for task %s/%s failed for: %v", task.Namespace, task.Name, err)
	}

	for _, status := range prePredicateStatus {
		if status != nil && status.Code != api.Success {
			fitErrors := api.NewFitErrors()
			err := fmt.Errorf("PrePredicate for task %s/%s failed, %s", task.Namespace, task.Name, status.Reason)
			for _, ni := range ssn.Nodes {
				fitErrors.SetNodeError(ni.Name, err)
			}
			job.NodesFitErrors[task.UID] = fitErrors
			return err
		}
	}
	return nil
}

func (backfill *Action) UnInitialize() {}
