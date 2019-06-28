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
	"github.com/golang/glog"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/util"
)

type enqueueAction struct {
	ssn *framework.Session
}

func New() *enqueueAction {
	return &enqueueAction{}
}

func (enqueue *enqueueAction) Name() string {
	return "enqueue"
}

func (enqueue *enqueueAction) Initialize() {}

func (enqueue *enqueueAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Enqueue ...")
	defer glog.V(3).Infof("Leaving Enqueue ...")

	queues := util.NewPriorityQueue(ssn.QueueOrderFn)
	queueMap := map[api.QueueID]*api.QueueInfo{}

	jobsMap := map[api.QueueID]*util.PriorityQueue{}

	for _, job := range ssn.Jobs {
		if queue, found := ssn.Queues[job.Queue]; !found {
			glog.Errorf("Failed to find Queue <%s> for Job <%s/%s>",
				job.Queue, job.Namespace, job.Name)
			continue
		} else {
			if _, existed := queueMap[queue.UID]; !existed {
				glog.V(3).Infof("Added Queue <%s> for Job <%s/%s>",
					queue.Name, job.Namespace, job.Name)

				queueMap[queue.UID] = queue
				queues.Push(queue)
			}
		}

		if job.PodGroup.Status.Phase == v1alpha1.PodGroupPending {
			if _, found := jobsMap[job.Queue]; !found {
				jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
			}
			glog.V(3).Infof("Added Job <%s/%s> into Queue <%s>", job.Namespace, job.Name, job.Queue)
			jobsMap[job.Queue].Push(job)
		}
	}

	glog.V(3).Infof("Try to enqueue PodGroup to %d Queues", len(jobsMap))

	emptyRes := api.EmptyResource()
	nodesIdleRes := api.EmptyResource()
	for _, node := range ssn.Nodes {
		nodesIdleRes.Add(node.Allocatable.Clone().Multi(1.2).Sub(node.Used))
	}

	for {
		if queues.Empty() {
			break
		}

		if nodesIdleRes.Less(emptyRes) {
			glog.V(3).Infof("Node idle resource <%s> is overused, ignore it.")
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
			pgResource := api.NewResource(*job.PodGroup.Spec.MinResources)
			if ssn.JobEnqueueable(job) && pgResource.LessEqual(nodesIdleRes) {
				nodesIdleRes.Sub(pgResource)
				inqueue = true
			}
		}

		if inqueue {
			job.PodGroup.Status.Phase = v1alpha1.PodGroupInqueue
			ssn.Jobs[job.UID] = job
		}

		// Added Queue back until no job in Queue.
		queues.Push(queue)
	}
}

func (enqueue *enqueueAction) UnInitialize() {}
