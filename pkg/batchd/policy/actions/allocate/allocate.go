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

package allocate

import (
	"sort"

	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/policy/framework"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/policy/util"
)

type allocateAction struct {
}

func New() *allocateAction {
	return &allocateAction{}
}

func (alloc *allocateAction) Name() string {
	return "allocate"
}

func (alloc *allocateAction) Initialize() {}

func (alloc *allocateAction) Execute(ssn *framework.Session) []*cache.QueueInfo {
	glog.V(4).Infof("Enter Allocate ...")
	defer glog.V(4).Infof("Leaving Allocate ...")

	queues := ssn.Queues
	nodes := ssn.Nodes

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

	// assign MinAvailable of each podSet first by chronologically
	sort.Sort(dq)
	pq := util.NewPriorityQueue()
	matchNodesForPodSet := make(map[string][]*cache.NodeInfo)
	for _, q := range dq {
		psi := q.Value.(*podSetInfo)

		// fetch the nodes that match PodSet NodeSelector and NodeAffinity
		// and store it for following DRF assignment
		matchNodes := fetchMatchNodeForPodSet(psi, nodes)
		matchNodesForPodSet[psi.podSet.Name] = matchNodes

		assigned := alloc.assignMinimalPods(psi.insufficientMinAvailable(), psi, matchNodes)
		if assigned {
			// only push PodSet with MinAvailable to priority queue
			// to avoid PodSet get resources less than MinAvailable by following DRF assignment
			pq.Push(psi, psi.share)

			glog.V(3).Infof("assign MinAvailable for podset %s/%s successfully",
				psi.podSet.Namespace, psi.podSet.Name)
		} else {
			glog.V(3).Infof("assign MinAvailable for podset %s/%s failed, there is no enough resources",
				psi.podSet.Namespace, psi.podSet.Name)
		}
	}

	for !pq.Empty() {
		psi := pq.Pop().(*podSetInfo)

		glog.V(3).Infof("try to allocate resources to PodSet <%v/%v>",
			psi.podSet.Namespace, psi.podSet.Name)

		// assign one pod of PodSet by DRF
		assigned := alloc.assignMinimalPods(1, psi, matchNodesForPodSet[psi.podSet.Name])

		if assigned {
			// push PosSet back for next assignment
			pq.Push(psi, psi.share)
		}
	}

	return queues
}

func (alloc *allocateAction) UnInitialize() {}

// Assign node for min Pods of psi
// If min Pods can not be satisfy, then don't assign any pods
func (alloc *allocateAction) assignMinimalPods(min int, psi *podSetInfo, nodes []*cache.NodeInfo) bool {
	glog.V(4).Infof("Enter assignMinimalPods ...")
	defer glog.V(4).Infof("Leaving assignMinimalPods ...")

	if min == 0 {
		// PodSet need to be assigned 0 Pod this time
		// the assignment is successful directly
		return true
	}

	unacceptedAssignedNodes := make(map[string]*cache.NodeInfo)
	for min > 0 {
		p := psi.popPendingPod()
		if p == nil {
			glog.V(3).Infof("no pending Pod in PodSet <%v/%v>",
				psi.podSet.Namespace, psi.podSet.Name)
			break
		}

		assigned := false
		for _, node := range nodes {
			currentIdle := node.CurrentIdle()
			if p.Resreq.LessEqual(currentIdle) {

				// record the assignment temporarily in PodSet and Node
				// this assignment will be accepted (min could be met in this time)
				// or discarded (min could not be met in this time)
				psi.assignPendingPod(p, node.Name)
				node.AddUnAcceptedAllocated(p.Resreq)

				assigned = true

				unacceptedAssignedNodes[node.Name] = node

				glog.V(3).Infof("assign <%v/%v> to <%s>: available <%v>, request <%v>",
					p.Namespace, p.Name, p.NodeName, node.Idle, p.Resreq)
				break
			}
		}

		// the left resources can not meet any pod in this PodSet
		// (assume that all pods in same PodSet share same resource request)
		if !assigned {
			// push pending pod back for consistent
			psi.pushPendingPod(p)
			break
		}

		min--
	}

	if len(unacceptedAssignedNodes) == 0 {
		// there is no nodes assigned pods this time
		// the assignment is failed(no pod is assigned in this time)
		return false
	}

	if min == 0 {
		// min is met, accept all assignment this time
		psi.acceptAssignedPods()
		for _, node := range unacceptedAssignedNodes {
			node.AcceptAllocated()
		}
		return true
	} else {
		// min could not be met, discard all assignment this time
		// to avoid PodSet get resources less than min
		psi.discardAssignedPods()
		for _, node := range unacceptedAssignedNodes {
			node.DiscardAllocated()
		}
		return false
	}
}
