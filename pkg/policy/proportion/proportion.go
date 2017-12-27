/*
Copyright 2017 The Kubernetes Authors.

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

package proportion

import (
	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

// PolicyName is the name of proportion policy; it'll be use for any case
// that need a name, e.g. default policy, register proportion policy.
var PolicyName = "proportion"

type proportionScheduler struct {
}

func New() *proportionScheduler {
	return &proportionScheduler{}
}

// collectSchedulingInfo collects total resources of the cluster
func (ps *proportionScheduler) collectSchedulingInfo(queues map[string]*schedulercache.QueueInfo, nodes []*schedulercache.NodeInfo) (*schedulercache.Resource, *schedulercache.Resource, int64) {
	total := schedulercache.EmptyResource()
	available := schedulercache.EmptyResource()
	totalWeight := int64(0)

	for _, node := range nodes {
		total.Add(node.Allocatable)
		available.Add(node.Idle)
	}

	for _, queue := range queues {
		totalWeight += int64(queue.Weight)
	}

	return total, available, totalWeight
}

func (ps *proportionScheduler) Name() string {
	return PolicyName
}

func (ps *proportionScheduler) Initialize() {
	// TODO
}

func (ps *proportionScheduler) Group(queuejobs []*schedulercache.QueueJobInfo) map[string][]*schedulercache.QueueJobInfo {
	glog.V(4).Infof("Enter Group ...")
	defer glog.V(4).Infof("Leaving Group ...")

	// calculate total queuejob resource request under queue
	queues := make(map[string][]*schedulercache.QueueJobInfo, 0)

	for _, job := range queuejobs {
		// NOTES: Queue name is just an suggestion here; it'll be customized.
		if _, found := queues[job.QueueName]; !found {
			queues[job.QueueName] = make([]*schedulercache.QueueJobInfo, 1)
		}
		queues[job.QueueName] = append(queues[job.QueueName], job)
	}

	return queues
}

func (ps *proportionScheduler) Allocate(queues map[string]*schedulercache.QueueInfo, nodes []*schedulercache.NodeInfo) map[string]*schedulercache.QueueInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	total, available, totalWeight := ps.collectSchedulingInfo(queues, nodes)
	if total.IsEmpty() || totalWeight == 0 {
		glog.V(4).Infof("There is no resources or queues in cluster")
		return nil
	}

	glog.V(4).Infof("total resource <%v>, available resource <%v>, total weight <%v>",
		total, available, totalWeight)

	for _, queue := range queues {
		deserved := total.Multiply(float64(queue.Weight) / float64(totalWeight))
		queue.Deserved = deserved.Clone()

		// allocated resources is init as min(deserved, used)
		if deserved.Less(queue.Used) {
			queue.Allocated = deserved.Clone()
		} else {
			queue.Allocated = queue.Used.Clone()
		}
	}

	// Allocated resources to queue
	for _, n := range nodes {
		assigned := false
		for _, queue := range queues {
			alloc := queue.Allocated.Clone()

			if alloc.Add(n.Idle).LessEqual(queue.Deserved) {
				queue.Allocated.Add(n.Idle)
				queue.Nodes = append(queue.Nodes, n)
				assigned = true
			}
			break
		}

		if !assigned {
			glog.Infof("Failed to assign node <%s> to any queue :(.", n.Name)
		}
	}

	if glog.V(4) {
		for _, queue := range queues {
			glog.V(4).Infof("queue <%s>, deserved <%v>, used <%v>, allocated <%v>",
				queue.Name, queue.Deserved, queue.Used, queue.Allocated)
		}
	}

	// Allocated resource to QueueJob
	for _, queue := range queues {
		for _, n := range queue.Nodes {
			idle := n.Idle.Clone()

			for {
				if idle.IsEmpty() {
					break
				}

				assigned := false
				for _, job := range queue.Jobs {
					if job.UnderUsed() {
						if job.TaskRequest.LessEqual(idle) {
							assigned = true
							idle.Sub(job.TaskRequest)
							job.Allocated.Add(job.TaskRequest)
						}
					}
				}

				if !assigned {
					glog.V(4).Infof("No job in queue <%s> can consume idle resource <%v> on node <%s>, try next node.",
						queue.Name, idle, n.Name)
					break
				}
			}
		}
	}

	return queues
}

func (ps *proportionScheduler) UnInitialize() {
	// TODO
}
