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

import "k8s.io/klog"

type policyRestricted struct {
	numaNodes []int
}

// NewPolicyRestricted return a new policy interface
func NewPolicyRestricted(numaNodes []int) Policy {
	return &policyRestricted{numaNodes: numaNodes}
}

func (p *policyRestricted) canAdmitPodResult(hint *TopologyHint) bool {
	return hint.Preferred
}

func (p *policyRestricted) Predicate(providersHints []map[string][]TopologyHint) (TopologyHint, bool) {
	filteredHints := filterProvidersHints(providersHints)
	bestHint := mergeFilteredHints(p.numaNodes, filteredHints)
	admit := p.canAdmitPodResult(&bestHint)

	klog.V(4).Infof("bestHint: %v admit %v\n", bestHint, admit)
	return bestHint, admit
}
