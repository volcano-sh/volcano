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
	podSet *cache.PodSet

	dominantResource v1.ResourceName // The dominant resource name of PodSet
	allocated        *cache.Resource // Allocated resource of PodSet
	share            float64         // The DRF share of PodSet
	total            *cache.Resource // The total resource of cluster, used to update DRF share

	pendingIndex    int
	assignedPending int
}

func newPodSetInfo(ps *cache.PodSet, t *cache.Resource) *podSetInfo {
	psi := &podSetInfo{
		podSet:           ps,
		allocated:        ps.Allocated.Clone(),
		total:            t,
		dominantResource: v1.ResourceCPU,
	}

	// Calculates the dominant resource.
	for _, rn := range cache.ResourceNames() {
		if psi.total.IsZero(rn) {
			continue
		}

		p := psi.allocated.Get(rn) / psi.total.Get(rn)
		if p > psi.share {
			psi.share = p
			psi.dominantResource = rn
		}
	}

	glog.V(3).Infof("PodSet <%v/%v>: priority <%f>, dominant resource <%v>",
		psi.podSet.Namespace, psi.podSet.Name, psi.share, psi.dominantResource)

	return psi
}

func (psi *podSetInfo) assignPendingPod(p *cache.PodInfo, nodeName string) {
	psi.allocated.Add(p.Request)
	p.NodeName = nodeName
	psi.podSet.Assigned = append(psi.podSet.Assigned, p)

	psi.share = psi.allocated.Get(psi.dominantResource) / psi.total.Get(psi.dominantResource)

	glog.V(3).Infof("PodSet <%v/%v> after assignment: priority <%f>, dominant resource <%v>",
		psi.podSet.Namespace, psi.podSet.Name, psi.share, psi.dominantResource)
}

func (psi *podSetInfo) popPendingPod() *cache.PodInfo {
	if len(psi.podSet.Pending) == 0 {
		return nil
	}

	pi := psi.podSet.Pending[0]
	psi.podSet.Pending = psi.podSet.Pending[1:]

	return pi
}

func (psi *podSetInfo) pushPendingPod(p *cache.PodInfo) {
	psi.podSet.Pending = append(psi.podSet.Pending, p)
}

func (psi *podSetInfo) meetMinAvailable() bool {
	return len(psi.podSet.Running)+len(psi.podSet.Assigned) >= psi.podSet.MinAvailable
}
