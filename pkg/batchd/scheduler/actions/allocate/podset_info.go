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
	"github.com/golang/glog"

	"k8s.io/api/core/v1"

	arbapi "github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/scheduler/util"
)

type podSetInfo struct {
	podSet *arbapi.JobInfo

	dominantResource v1.ResourceName  // The dominant resource name of PodSet
	allocated        *arbapi.Resource // Allocated resource of PodSet
	share            float64          // The DRF share of PodSet
	total            *arbapi.Resource // The total resource of cluster, used to update DRF share

	unacceptedAllocated    *arbapi.Resource
	unacceptedAssignedPods []*arbapi.TaskInfo

	pendingSorted *util.PriorityQueue
}

func compareTaskPriority(l, r interface{}) bool {
	lv := l.(*arbapi.TaskInfo)
	rv := r.(*arbapi.TaskInfo)

	return lv.Priority > rv.Priority
}

func newPodSetInfo(ps *arbapi.JobInfo, t *arbapi.Resource) *podSetInfo {
	psi := &podSetInfo{
		podSet:                 ps,
		allocated:              ps.Allocated.Clone(),
		total:                  t,
		dominantResource:       v1.ResourceCPU,
		unacceptedAllocated:    arbapi.EmptyResource(),
		unacceptedAssignedPods: make([]*arbapi.TaskInfo, 0),
		pendingSorted:          util.NewPriorityQueue(compareTaskPriority),
	}

	// Calculates the dominant resource.
	for _, rn := range arbapi.ResourceNames() {
		if psi.total.IsZero(rn) {
			continue
		}

		p := psi.calculateShare(rn)
		if p > psi.share {
			psi.share = p
			psi.dominantResource = rn
		}
	}

	// TODO(jinzhejz): it is better to move sorted pods to PodSet
	for _, ps := range psi.podSet.Tasks[arbapi.Pending] {
		psi.pendingSorted.Push(ps)
	}

	glog.V(3).Infof("PodSet <%v/%v>: priority <%f>, dominant resource <%v>",
		psi.podSet.UID, psi.podSet.Name, psi.share, psi.dominantResource)

	return psi
}

func (psi *podSetInfo) popPendingPod() *arbapi.TaskInfo {
	if psi.pendingSorted.Empty() {
		return nil
	}

	pi := psi.pendingSorted.Pop().(*arbapi.TaskInfo)

	return pi
}

func (psi *podSetInfo) pushPendingPod(p *arbapi.TaskInfo) {
	psi.pendingSorted.Push(p)
}

func (psi *podSetInfo) insufficientMinAvailable() int {
	insufficient := 0
	occupied := 0
	for status, tasks := range psi.podSet.Tasks {
		if arbapi.OccupiedResources(status) {
			occupied = occupied + len(tasks)
		}
	}

	if occupied < psi.podSet.MinAvailable {
		insufficient = psi.podSet.MinAvailable - occupied
	}
	return insufficient
}

func (psi *podSetInfo) calculateShare(rn v1.ResourceName) float64 {
	return psi.allocated.Get(rn) / psi.total.Get(rn)
}
