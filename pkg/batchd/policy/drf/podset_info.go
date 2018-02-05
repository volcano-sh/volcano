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
	"k8s.io/api/core/v1"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/batchd/cache"
)

type podSetInfo struct {
	podSet           *cache.PodSet
	dominantResource v1.ResourceName
	allocated        *cache.Resource
	priority         float64
	total            *cache.Resource
	pendingIndex     int
	assignedPending  int
}

func newPodSetInfo(ps *cache.PodSet, t *cache.Resource) *podSetInfo {
	psi := &podSetInfo{
		podSet:           ps,
		allocated:        ps.Allocated.Clone(),
		total:            t,
		dominantResource: v1.ResourceCPU,
	}

	// Calculates pending pod with NodeName
	for _, p := range psi.podSet.Pending {
		if len(p.Pod.Spec.NodeName) != 0 {
			psi.assignedPending++
		}
	}

	// Calculates the dominant resource.
	for _, rn := range cache.ResourceNames() {
		if psi.total.IsZero(rn) {
			continue
		}

		p := psi.allocated.Get(rn) / psi.total.Get(rn)
		if p > psi.priority {
			psi.priority = p
			psi.dominantResource = rn
		}
	}

	glog.V(3).Infof("PodSet <%v/%v>: priority <%f>, dominant resource <%v>",
		psi.podSet.Namespace, psi.podSet.Name, psi.priority, psi.dominantResource)

	return psi
}

func (psi *podSetInfo) assignPendingPod(nodeName string) {
	p := psi.podSet.Pending[psi.pendingIndex]
	psi.allocated.Add(p.Request)
	p.NodeName = nodeName

	// Update related info.
	psi.pendingIndex++
	psi.assignedPending++
	psi.priority = psi.allocated.Get(psi.dominantResource) / psi.total.Get(psi.dominantResource)

	glog.V(3).Infof("PodSet <%v/%v> after assignment: priority <%f>, dominant resource <%v>",
		psi.podSet.Namespace, psi.podSet.Name, psi.priority, psi.dominantResource)
}

func (psi *podSetInfo) nextPendingPod() *cache.PodInfo {
	for i := psi.pendingIndex; i < len(psi.podSet.Pending); i++ {
		if len(psi.podSet.Pending[i].NodeName) == 0 {
			psi.pendingIndex = i
			return psi.podSet.Pending[i]
		}
	}

	return nil
}

func (psi *podSetInfo) meetMinAvailable() bool {
	return len(psi.podSet.Running)+psi.assignedPending >= psi.podSet.MinAvailable
}
