/*
Copyright 2021 The Volcano Authors.

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

package policy

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	nodeinfov1alpha1 "volcano.sh/apis/pkg/apis/nodeinfo/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// TopologyHint is a struct containing the NUMANodeAffinity for a Container
type TopologyHint struct {
	NUMANodeAffinity bitmask.BitMask
	// Preferred is set to true when the NUMANodeAffinity encodes a preferred
	// allocation for the Container. It is set to false otherwise.
	Preferred bool
}

// Policy is an interface for topology manager policy
type Policy interface {
	// Predicate Get the best hit.
	Predicate(providersHints []map[string][]TopologyHint) (TopologyHint, bool)
}

// HintProvider is an interface for components that want to collaborate to
// achieve globally optimal concrete resource alignment with respect to
// NUMA locality.
type HintProvider interface {
	// Name returns provider name used for register and logging.
	Name() string
	// GetTopologyHints returns hints if this hint provider has a preference,
	GetTopologyHints(container *v1.Container, topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string][]TopologyHint
	Allocate(container *v1.Container, bestHit *TopologyHint, topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets) map[string]cpuset.CPUSet
}

// GetPolicy return the interface matched the input task topology config
func GetPolicy(node *api.NodeInfo, numaNodes []int) Policy {
	switch batch.NumaPolicy(node.NumaSchedulerInfo.Policies[nodeinfov1alpha1.TopologyManagerPolicy]) {
	case batch.None:
		return NewPolicyNone(numaNodes)
	case batch.BestEffort:
		return NewPolicyBestEffort(numaNodes)
	case batch.Restricted:
		return NewPolicyRestricted(numaNodes)
	case batch.SingleNumaNode:
		return NewPolicySingleNumaNode(numaNodes)
	}

	return &policyNone{}
}

// AccumulateProvidersHints return all TopologyHint collection from different providers
func AccumulateProvidersHints(container *v1.Container,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets,
	hintProviders []HintProvider) (providersHints []map[string][]TopologyHint) {
	for _, provider := range hintProviders {
		hints := provider.GetTopologyHints(container, topoInfo, resNumaSets)
		providersHints = append(providersHints, hints)
	}

	return providersHints
}

// Allocate return all resource assignment collection from different providers
func Allocate(container *v1.Container, bestHit *TopologyHint,
	topoInfo *api.NumatopoInfo, resNumaSets api.ResNumaSets, hintProviders []HintProvider) map[string]cpuset.CPUSet {
	allResAlloc := make(map[string]cpuset.CPUSet)
	for _, provider := range hintProviders {
		resAlloc := provider.Allocate(container, bestHit, topoInfo, resNumaSets)
		for resName, assign := range resAlloc {
			allResAlloc[resName] = assign
		}
	}

	return allResAlloc
}
