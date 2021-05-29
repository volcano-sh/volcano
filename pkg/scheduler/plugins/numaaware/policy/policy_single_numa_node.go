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

type policySingleNumaNode struct {
	numaNodes []int
}

// NewPolicySingleNumaNode return a new policy interface
func NewPolicySingleNumaNode(numaNodes []int) Policy {
	return &policySingleNumaNode{numaNodes: numaNodes}
}

func (policy *policySingleNumaNode) canAdmitPodResult(hint *TopologyHint) bool {
	return hint.Preferred
}

// Return hints that have valid bitmasks with exactly one bit set.
func filterSingleNumaHints(allResourcesHints [][]TopologyHint) [][]TopologyHint {
	var filteredResourcesHints [][]TopologyHint
	for _, oneResourceHints := range allResourcesHints {
		var filtered []TopologyHint
		for _, hint := range oneResourceHints {
			if hint.NUMANodeAffinity == nil && hint.Preferred {
				filtered = append(filtered, hint)
			}
			if hint.NUMANodeAffinity != nil && hint.NUMANodeAffinity.Count() == 1 && hint.Preferred {
				filtered = append(filtered, hint)
			}
		}
		filteredResourcesHints = append(filteredResourcesHints, filtered)
	}
	return filteredResourcesHints
}

func (policy *policySingleNumaNode) Predicate(providersHints []map[string][]TopologyHint) (TopologyHint, bool) {
	filteredHints := filterProvidersHints(providersHints)
	singleNumaHints := filterSingleNumaHints(filteredHints)
	bestHint := mergeFilteredHints(policy.numaNodes, singleNumaHints)
	klog.V(4).Infof("bestHint: %v\n", bestHint)
	admit := policy.canAdmitPodResult(&bestHint)
	return bestHint, admit
}
