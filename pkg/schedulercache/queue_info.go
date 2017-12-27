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

package schedulercache

import (
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type QueueInfo struct {
	Name   string
	Queue  *arbv1.Queue
	Weight int

	// The total resources that a Queue should get
	Deserved *Resource

	// The total resources of running Pods belong to this QueueJob
	//   * UnderUsed: Used < Allocated
	//   * Meet: Used == Allocated
	//   * OverUsed: Used > Allocated
	Used *Resource

	// The total resources that a Queue can get currently, it's expected to
	// be equal or less than `Deserved` (when Preemption try to reclaim resource
	// for this Queue)
	Allocated *Resource

	// All jobs belong to this Queue
	Jobs []*QueueJobInfo

	// The pod that without `Owners`
	Pods []*PodInfo

	// The node candidates for Queue and QueueJobs
	Nodes []*NodeInfo
}

func NewQueueInfo(queue *arbv1.Queue) *QueueInfo {
	return &QueueInfo{
		Name:      queue.Name,
		Queue:     queue,
		Weight:    queue.Spec.Weight,
		Deserved:  NewResource(queue.Status.Deserved),
		Used:      NewResource(queue.Status.Used),
		Allocated: NewResource(queue.Status.Allocated),

		Jobs:  make([]*QueueJobInfo, 10),
		Pods:  make([]*PodInfo, 10),
		Nodes: make([]*NodeInfo, 10),
	}
}

func (qi *QueueInfo) UnderUsed() bool {
	return qi.Used.Less(qi.Allocated)
}

func (qi *QueueInfo) OverUsed() bool {
	return qi.Allocated.Less(qi.Used)
}

func (qi *QueueInfo) Clone() *QueueInfo {
	return &QueueInfo{
		Name:      qi.Name,
		Queue:     qi.Queue,
		Deserved:  qi.Deserved.Clone(),
		Used:      qi.Used.Clone(),
		Allocated: qi.Allocated.Clone(),

		// Did not clone jobs, pods, nodes which are update by QueueController.
	}
}
