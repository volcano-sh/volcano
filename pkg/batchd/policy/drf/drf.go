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

package drf

import (
	"sort"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/policy/util"
)

// PolicyName is the name of drf policy; it'll be use for any case
// that need a name, e.g. default policy, register drf policy.
var PolicyName = "drf"

type drfScheduler struct {
}

func New() *drfScheduler {
	return &drfScheduler{}
}

func (drf *drfScheduler) Name() string {
	return PolicyName
}

func (drf *drfScheduler) Initialize() {}

func (drf *drfScheduler) Allocate(queues []*cache.QueueInfo, nodes []*cache.NodeInfo) []*cache.QueueInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	dq := util.NewDictionaryQueue()

	total := cache.EmptyResource()

	for _, n := range nodes {
		total.Add(n.Allocatable)
	}

	for _, c := range queues {
		for _, ps := range c.PodSets {
			psi := newPodSetInfo(ps, total)
			dq.Push(util.NewDictionaryItem(psi, psi.podSet.Name))
		}
	}

	sort.Sort(dq)
	// assign MinAvailable of each podSet first by chronologically
	for _, q := range dq {
		psi := q.Value.(*podSetInfo)
		for {
			if psi.meetMinAvailable() {
				glog.V(3).Infof("MinAvailable of podset %s/%s is met, assign next podset",
					psi.podSet.Namespace, psi.podSet.Name)
				break
			}

			p := psi.nextPendingPod()
			if p == nil {
				glog.V(3).Infof("no pending Pod in PodSet <%v/%v>",
					psi.podSet.Namespace, psi.podSet.Name)
				break
			}

			assigned := false
			for _, node := range nodes {
				if p.Request.LessEqual(node.Idle) {
					psi.assignPendingPod(node.Name)
					node.Idle.Sub(p.Request)

					assigned = true

					glog.V(3).Infof("assign <%v/%v> to <%s>: available <%v>, request <%v>",
						p.Namespace, p.Name, p.NodeName, node.Idle, p.Request)
					break
				}
			}

			// the left resources can not meet any pod in this podset
			if !assigned {
				break
			}
		}
	}

	// build priority queue after assign minAvailable
	pq := util.NewPriorityQueue()
	for _, q := range dq {
		psi := q.Value.(*podSetInfo)
		pq.Push(psi, psi.priority)
	}

	for {
		if pq.Empty() {
			break
		}

		psi := pq.Pop().(*podSetInfo)

		glog.V(3).Infof("try to allocate resources to PodSet <%v/%v>",
			psi.podSet.Namespace, psi.podSet.Name)

		p := psi.nextPendingPod()

		// If no pending pods, skip this PodSet without push it back.
		if p == nil {
			glog.V(3).Infof("no pending Pod in PodSet <%v/%v>",
				psi.podSet.Namespace, psi.podSet.Name)
			continue
		}

		assigned := false
		for _, node := range nodes {
			if p.Request.LessEqual(node.Idle) {
				psi.assignPendingPod(node.Name)
				node.Idle.Sub(p.Request)

				assigned = true

				glog.V(3).Infof("assign <%v/%v> to <%s>: available <%v>, request <%v>",
					p.Namespace, p.Name, p.NodeName, node.Idle, p.Request)
				break
			}
		}

		if assigned {
			pq.Push(psi, psi.priority)
		} else {
			// If no assignment, did not push PodSet back as no node can be used.
			glog.V(3).Infof("no node was assigned to <%v/%v> with request <%v>",
				p.Namespace, p.Name, p.Request)
		}
	}

	return queues
}

func (drf *drfScheduler) UnInitialize() {}
