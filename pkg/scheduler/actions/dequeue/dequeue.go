/*
Copyright 2026 The Volcano Authors.

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

package dequeue

import (
	"time"

	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const actionName = "dequeue"

type Action struct{}

func New() *Action {
	return &Action{}
}

func (dequeue *Action) Name() string {
	return actionName
}

func (dequeue *Action) Initialize() {}

// Execute moves Inqueue jobs that made no scheduling progress back to Pending.
// Dequeue must run after all actions that can allocate or pipeline tasks.
func (dequeue *Action) Execute(ssn *framework.Session) {
	start := time.Now()
	dequeuedCount := 0
	klog.V(5).Info("Enter Dequeue ...")
	defer func() {
		klog.V(5).InfoS("Leaving Dequeue ...",
			"dequeued", dequeuedCount, "duration", time.Since(start))
	}()

	for _, job := range ssn.Jobs {
		if job.PodGroup == nil || job.PodGroup.Status.Phase != scheduling.PodGroupInqueue {
			continue
		}

		// An Inqueue job may be observed before its Pods have been created.
		if len(job.Tasks) == 0 {
			continue
		}

		if ssn.JobReady(job) || ssn.JobPipelined(job) {
			continue
		}

		if _, found := ssn.Queues[job.Queue]; !found {
			klog.Warningf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		}

		ssn.ReleaseJobReservedTasks(job, dequeue.Name())
		job.PodGroup.Status.Phase = scheduling.PodGroupPending
		ssn.AddUnschedulableJob(job.UID, dequeue.Name())
		dequeuedCount++
		klog.V(3).InfoS("Dequeued unschedulable job",
			"job", klog.KRef(job.Namespace, job.Name),
			"from", scheduling.PodGroupInqueue,
			"to", scheduling.PodGroupPending)
	}
}

func (dequeue *Action) UnInitialize() {}
