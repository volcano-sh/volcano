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

package cache

import (
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type ConsumerInfo struct {
	Consumer *arbv1.Consumer

	Name      string
	Namespace string
	Weight    int

	// The total resources that a Consumer should get
	Deserved *Resource

	// The total resources of running Pods belong to this Consumer
	//   * UnderUsed: Used < Allocated
	//   * Meet: Used == Allocated
	//   * OverUsed: Used > Allocated
	Used *Resource

	// The total resources that a Consumer can get currently, it's expected to
	// be equal or less than `Deserved` (when Preemption try to reclaim resource
	// for this Consumer)
	Allocated *Resource

	// All jobs belong to this Consumer
	PodSets []*PodSet

	// The pod that without `Owners`
	Pods []*PodInfo

	// The node candidates for Consumer
	Nodes []*NodeInfo
}

func NewConsumerInfo(queue *arbv1.Consumer) *ConsumerInfo {
	return &ConsumerInfo{
		Name:      queue.Name,
		Namespace: queue.Namespace,
		Consumer:  queue,
		Weight:    queue.Spec.Weight,
		//Deserved:  NewResource(queue.Status.Deserved),
		//Used:      NewResource(queue.Status.Used),
		//Allocated: NewResource(queue.Status.Allocated),

		PodSets: make([]*PodSet, 0),
		Pods:    make([]*PodInfo, 0),
		Nodes:   make([]*NodeInfo, 0),
	}
}

func (qi *ConsumerInfo) UnderUsed() bool {
	return qi.Used.Less(qi.Allocated)
}

func (qi *ConsumerInfo) OverUsed() bool {
	return qi.Allocated.Less(qi.Used)
}

func (qi *ConsumerInfo) Clone() *ConsumerInfo {
	return &ConsumerInfo{
		Name:      qi.Name,
		Namespace: qi.Namespace,
		Consumer:  qi.Consumer,
		//Deserved:  qi.Deserved.Clone(),
		//Used:      qi.Used.Clone(),
		//Allocated: qi.Allocated.Clone(),

		// Did not clone jobs, pods, nodes which are update by QueueController.
	}
}
