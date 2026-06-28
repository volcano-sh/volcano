/*
Copyright 2019 The Volcano Authors.

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

package enqueue

import (
	"strconv"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// DefaultInqueueTimeout is the default duration a job is allowed to stay in Inqueue state
	// before being downgraded back to Pending.
	DefaultInqueueTimeout = 5 * time.Minute
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (enqueue *Action) Name() string {
	return "enqueue"
}

func (enqueue *Action) Initialize() {}

func (enqueue *Action) Execute(ssn *framework.Session) {
	klog.V(5).Infof("Enter Enqueue ...")
	defer klog.V(5).Infof("Leaving Enqueue ...")

	// Phase 1: Evict timed-out Inqueue jobs back to Pending to release quota.
	enqueue.evictTimedOutInqueueJobs(ssn)

	// Phase 2: Normal enqueue logic.
	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueSet := sets.NewString()
	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	for _, job := range ssn.Jobs {
		if job.ScheduleStartTimestamp.IsZero() {
			ssn.Jobs[job.UID].ScheduleStartTimestamp = metav1.Time{
				Time: time.Now(),
			}
		}
		if queue, found := ssn.Queues[job.Queue]; !found {
			klog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else if !queueSet.Has(string(queue.UID)) {
			klog.V(5).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)

			queueSet.Insert(string(queue.UID))
			queues.Push(queue)
		}

		if job.IsPending() {
			if _, found := jobsMap[job.Queue]; !found {
				jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			klog.V(5).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
			jobsMap[job.Queue].Push(job)
		}
	}

	klog.V(3).Infof("Try to enqueue PodGroup to %d Queues", len(jobsMap))

	for {
		if queues.Empty() {
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		// skip the Queue that has no pending job
		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			continue
		}
		job := jobs.Pop().(*api.JobInfo)

		if job.PodGroup.Spec.MinResources == nil || ssn.JobEnqueueable(job) {
			ssn.JobEnqueued(job)
			job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			ssn.Jobs[job.UID] = job

			// Record the enqueue timestamp for timeout tracking.
			if job.EnqueueTimestamp.IsZero() {
				job.EnqueueTimestamp = metav1.Time{Time: time.Now()}
			}
		}

		// Added Queue back until no job in Queue.
		queues.Push(queue)
	}
}

// evictTimedOutInqueueJobs iterates over all jobs in the Inqueue state and downgrades
// those that have exceeded their inqueue timeout back to Pending, releasing quota.
func (enqueue *Action) evictTimedOutInqueueJobs(ssn *framework.Session) {
	for _, job := range ssn.Jobs {
		if job.PodGroup == nil || job.PodGroup.Status.Phase != scheduling.PodGroupInqueue {
			continue
		}

		// Initialize the enqueue timestamp if it's not set (e.g. for jobs that were
		// already Inqueue before this feature was introduced).
		if job.EnqueueTimestamp.IsZero() {
			job.EnqueueTimestamp = metav1.Time{Time: time.Now()}
			ssn.Jobs[job.UID] = job
			ssn.MarkJobDirty(job.UID)
			continue
		}

		timeout := getInqueueTimeout(job)
		elapsed := time.Since(job.EnqueueTimestamp.Time)
		if elapsed < timeout {
			continue
		}

		klog.V(3).Infof("Job <%s/%s> exceeded inqueue timeout (%v > %v), downgrading to Pending",
			job.Namespace, job.Name, elapsed, timeout)

		// Downgrade to Pending.
		job.PodGroup.Status.Phase = scheduling.PodGroupPending
		job.EnqueueTimestamp = metav1.Time{} // reset timestamp

		// Notify plugins to release the inqueue quota.
		ssn.JobInqueueEvicted(job)

		ssn.Jobs[job.UID] = job
	}
}

// getInqueueTimeout returns the inqueue timeout for a job. It checks for an annotation
// override first, and falls back to DefaultInqueueTimeout.
func getInqueueTimeout(job *api.JobInfo) time.Duration {
	if job.PodGroup == nil {
		return DefaultInqueueTimeout
	}
	annotations := job.PodGroup.GetAnnotations()
	if annotations == nil {
		return DefaultInqueueTimeout
	}
	timeoutStr, found := annotations[api.JobInqueueTimeoutAnnotation]
	if !found {
		return DefaultInqueueTimeout
	}
	timeoutSeconds, err := strconv.Atoi(timeoutStr)
	if err != nil || timeoutSeconds <= 0 {
		klog.Warningf("Invalid inqueue-timeout annotation value %q for job <%s/%s>, using default %v",
			timeoutStr, job.Namespace, job.Name, DefaultInqueueTimeout)
		return DefaultInqueueTimeout
	}
	return time.Duration(timeoutSeconds) * time.Second
}

func (enqueue *Action) UnInitialize() {}
