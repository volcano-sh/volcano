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

	total := cache.EmptyResource()
	for _, n := range nodes {
		total.Add(n.Allocatable)
	}

	dq := util.NewDictionaryQueue()
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
		if psi.meetMinAvailable() {
			glog.V(3).Infof("MinAvailable of podset %s/%s is met, assign next podset",
				psi.podSet.Namespace, psi.podSet.Name)
			continue
		}

		assigned := drf.assignMinimalPods(psi.insufficientMinAvailable(), psi, nodes)
		if assigned {
			glog.V(3).Infof("assign MinAvailable for podset %s/%s successfully",
				psi.podSet.Namespace, psi.podSet.Name)
		} else {
			glog.V(3).Infof("assign MinAvailable for podset %s/%s failed, there is no enough resources",
				psi.podSet.Namespace, psi.podSet.Name)
		}
	}

	// build priority queue after assign minAvailable
	pq := util.NewPriorityQueue()
	for _, q := range dq {
		psi := q.Value.(*podSetInfo)
		pq.Push(psi, psi.share)
	}

	for {
		if pq.Empty() {
			break
		}

		psi := pq.Pop().(*podSetInfo)

		glog.V(3).Infof("try to allocate resources to PodSet <%v/%v>",
			psi.podSet.Namespace, psi.podSet.Name)

		assigned := drf.assignMinimalPods(1, psi, nodes)

		if assigned {
			// push PosSet back for next assignment
			pq.Push(psi, psi.share)
		}
	}

	return queues
}

func (drf *drfScheduler) UnInitialize() {}

// Assign node for min Pods of psi as much as possible
func (drf *drfScheduler) assignMinimalPods(min int, psi *podSetInfo, nodes []*cache.NodeInfo) bool {
	glog.V(4).Infof("Enter assignMinimalPods ...")
	defer glog.V(4).Infof("Leaving assignMinimalPods ...")

	for min > 0 {
		p := psi.popPendingPod()
		if p == nil {
			glog.V(3).Infof("no pending Pod in PodSet <%v/%v>",
				psi.podSet.Namespace, psi.podSet.Name)
			break
		}

		assigned := false
		for _, node := range nodes {
			if p.Request.LessEqual(node.Idle) {
				psi.assignPendingPod(p, node.Name)
				node.Idle.Sub(p.Request)

				assigned = true

				glog.V(3).Infof("assign <%v/%v> to <%s>: available <%v>, request <%v>",
					p.Namespace, p.Name, p.NodeName, node.Idle, p.Request)
				break
			}
		}

		// the left resources can not meet any pod in this podset
		if !assigned {
			// push pending pod back for consistent
			psi.pushPendingPod(p)
			break
		}

		min--
	}

	if min == 0 {
		return true
	} else {
		return false
	}
}
