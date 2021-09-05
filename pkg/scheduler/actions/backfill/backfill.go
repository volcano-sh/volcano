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
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (alloc *Action) Name() string {
	return "backfill"
}

func (alloc *Action) Initialize() {}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Backfill ...")
	defer klog.V(3).Infof("Leaving Backfill ...")

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
				fe := api.NewFitErrors()

				// As task did not request resources, so it only need to meet predicates.
				// TODO (k82cn): need to prioritize nodes to avoid pod hole.
				for _, node := range ssn.Nodes {
					// TODO (k82cn): predicates did not consider pod number for now, there'll
					// be ping-pong case here.
					if err := ssn.PredicateFn(task, node); err != nil {
						klog.V(3).Infof("Predicates failed for task <%s/%s> on node <%s>: %v",
							task.Namespace, task.Name, node.Name, err)
						fe.SetNodeError(node.Name, err)
						continue
					}

					klog.V(3).Infof("Binding Task <%v/%v> to node <%v>", task.Namespace, task.Name, node.Name)
					if err := ssn.Allocate(task, node); err != nil {
						klog.Errorf("Failed to bind Task %v on %v in Session %v", task.UID, node.Name, ssn.UID)
						fe.SetNodeError(node.Name, err)
						continue
					}

					metrics.UpdateE2eSchedulingDurationByJob(job.Name, string(job.Queue), job.Namespace, metrics.Duration(job.CreationTimestamp.Time))
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

func (alloc *Action) UnInitialize() {}
