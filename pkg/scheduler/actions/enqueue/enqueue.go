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
	for _, node := range ssn.Nodes {
		total.Add(node.Allocatable)
		used.Add(node.Used)
	}
	idle := total.Clone().Multi(enqueue.getOverCommitFactor(ssn)).Sub(used)
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

		inqueue := false
		if job.PodGroup.Spec.MinResources == nil {
			inqueue = true
		} else {
			minReq := api.NewResource(*job.PodGroup.Spec.MinResources)
			if isCapabilitySatisfied(ssn, job) && ssn.JobEnqueueable(job) && minReq.LessEqual(idle) {
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

func isCapabilitySatisfied(ssn *framework.Session, targetJob *api.JobInfo) bool {
	queueID := targetJob.Queue
	queue := ssn.Queues[queueID]

	// If no capability is set, always enqueue the job.
	if len(queue.Queue.Spec.Capability) == 0 {
		klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Enqueue.",
			queue.Name, targetJob.Namespace, targetJob.Name)
		return true
	}

	// If capability is set, calculate allocated resource of queue which the target belongs to,
	// compare the sum of allocated and Job minRequest resource with the capability resource
	allocatedResource := api.EmptyResource()
	klog.V(4).Infof("Count allocated resource of queue of job <%s/%s>", targetJob.Namespace, targetJob.Name)
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if job.Queue == targetJob.Queue {
			// if podgroup of job is Inqueue, the resource to be allocated to job should be considered as allocated in capability.
			// if not so, the following jobs in next loops will incorrectly compare resource allocated with capability
			if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
				for _, tasks := range job.TaskStatusIndex {
					for _, t := range tasks {
						allocatedResource.Add(t.Resreq)
					}
				}
			} else {
				// calculate jobs whose tasks have been partly allocated resource
				for status, tasks := range job.TaskStatusIndex {
					if api.AllocatedStatus(status) {
						for _, t := range tasks {
							allocatedResource.Add(t.Resreq)
						}
					}
				}
			}
		}
	}
	minReq := api.NewResource(*targetJob.PodGroup.Spec.MinResources)
	return minReq.Add(allocatedResource).LessEqual(api.NewResource(queue.Queue.Spec.Capability))
}
