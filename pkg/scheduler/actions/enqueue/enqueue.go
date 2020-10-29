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

package enqueue

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// overCommitFactor is resource overCommit factor for enqueue action
	// It determines the number of `pending` pods that the scheduler will tolerate
	// when the resources of the cluster is insufficient
	overCommitFactor = "overcommit-factor"
)

var (
	// defaultOverCommitFactor defines the default overCommit resource factor for enqueue action
	defaultOverCommitFactor = 1.2
	targetJob               = util.Reservation.TargetJob
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
	klog.V(3).Infof("Enter Enqueue ...")
	defer klog.V(3).Infof("Leaving Enqueue ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}
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
		} else if _, existed := queueMap[queue.UID]; !existed {
			klog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
				queue.Name, job.Namespace, job.Name)

			queueMap[queue.UID] = queue
			queues.Push(queue)
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupPending {
			if _, found := jobsMap[job.Queue]; !found {
				jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			klog.V(3).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
			jobsMap[job.Queue].Push(job)
		}
	}

	klog.V(3).Infof("Try to enqueue PodGroup to %d Queues", len(jobsMap))

	total := api.EmptyResource()
	used := api.EmptyResource()
	lockedNodesIdle := api.EmptyResource()
	if targetJob != nil && len(util.Reservation.LockedNodes) != 0 {
		for _, node := range util.Reservation.LockedNodes {
			lockedNodesIdle.Add(node.Idle)
			klog.V(4).Infof("locked node: %s", node.Name)
		}
	}
	for _, node := range ssn.Nodes {
		total.Add(node.Allocatable)
		used.Add(node.Used)
	}
	idle := total.Clone().Multi(enqueue.getOverCommitFactor(ssn)).Sub(used).Sub(lockedNodesIdle)

	for {
		if queues.Empty() {
			break
		}

		if idle.IsEmpty() {
			klog.V(3).Infof("Node idle resource is overused, ignore it.")
			break
		}

		queue := queues.Pop().(*api.QueueInfo)

		// Found "high" priority job
		jobs, found := jobsMap[queue.UID]
		if !found || jobs.Empty() {
			continue
		}
		job := jobs.Pop().(*api.JobInfo)
		if targetJob != nil && job.UID == targetJob.UID {
			klog.V(3).Infof("Target Job name: %s", targetJob.Name)
			continue
		}

		inqueue := false

		if job.PodGroup.Spec.MinResources == nil {
			inqueue = true
		} else {
			minReq := api.NewResource(*job.PodGroup.Spec.MinResources)
			if ssn.JobEnqueueable(job) && minReq.LessEqual(idle) {
				idle.Sub(minReq)
				inqueue = true
			}
		}

		if inqueue {
			job.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			ssn.Jobs[job.UID] = job
		}

		// Added Queue back until no job in Queue.
		queues.Push(queue)
	}
	// if target job exists, judge whether it can be inqueue or not
	if targetJob != nil && targetJob.PodGroup.Status.Phase == scheduling.PodGroupPending && len(util.Reservation.LockedNodes) != 0 {
		klog.V(4).Infof("Start to deal with Target Job")
		minReq := api.NewResource(*targetJob.PodGroup.Spec.MinResources)
		idle = idle.Add(lockedNodesIdle)
		if ssn.JobEnqueueable(targetJob) && minReq.LessEqual(idle) {
			klog.V(3).Infof("Turn Target Job phase to Inqueue")
			targetJob.PodGroup.Status.Phase = scheduling.PodGroupInqueue
			ssn.Jobs[targetJob.UID] = targetJob
		}
	}
}

func (enqueue *Action) UnInitialize() {}

func (enqueue *Action) getOverCommitFactor(ssn *framework.Session) float64 {
	factor := defaultOverCommitFactor
	arg := framework.GetArgOfActionFromConf(ssn.Configurations, enqueue.Name())
	if arg != nil {
		arg.GetFloat64(&factor, overCommitFactor)
	}

	return factor
}
