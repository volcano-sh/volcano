/*
Copyright 2026 The Volcano Authors.

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

package framework

import "volcano.sh/volcano/pkg/scheduler/api"

// AddHyperNodeCandidateFn registers an optional, allocation-only domain filter.
func (ssn *Session) AddHyperNodeCandidateFn(name string, fn api.HyperNodeCandidateFn) {
	if ssn.hyperNodeCandidateFns == nil {
		ssn.hyperNodeCandidateFns = make(map[string]api.HyperNodeCandidateFn)
	}
	ssn.hyperNodeCandidateFns[name] = fn
}

// HyperNodeContainsNomination checks whether all enabled gradient plugins allow
// a common domain covering pinned, without exploring unrelated subtrees.
// This also handles a child with exactly the same Nodes as its parent.
func (ssn *Session) HyperNodeContainsNomination(job *api.JobInfo, subJob *api.SubJobInfo, root *api.HyperNodeInfo, pinned string) bool {
	nodes := ssn.RealNodesSet[pinned]
	if root == nil || len(nodes) == 0 || ssn.HyperNodes[pinned] == nil {
		return false
	}
	candidates := ssn.HyperNodeIndex().DomainsContaining(nodes)
	if len(candidates) == 0 {
		return false
	}

	var results []api.HyperNodePluginGradient
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if !isEnabled(plugin.EnabledHyperNodeGradient) {
				continue
			}
			jobFn, jobFound := ssn.hyperNodeGradientForJobFns[plugin.Name]
			subJobFn, subJobFound := ssn.hyperNodeGradientForSubJobFns[plugin.Name]
			if (subJob == nil && !jobFound) || (subJob != nil && !subJobFound) {
				continue
			}
			var result api.HyperNodeGradientResult
			if fn := ssn.hyperNodeCandidateFns[plugin.Name]; fn != nil {
				result = fn(job, subJob, root, candidates)
			} else if subJob == nil {
				result = jobFn(job, root, api.PurposeAllocate)
			} else {
				result = subJobFn(subJob, root, api.PurposeAllocate)
			}
			if result.Unconstrained && len(result.Gradients) == 0 {
				continue
			}
			if result.Unconstrained {
				return false
			}
			results = append(results, api.HyperNodePluginGradient{PluginName: plugin.Name, Gradients: result.Gradients})
		}
	}
	// With no constrained plugins the input subtree is allowed, including the
	// historical root-only fallback when no gradient callback is registered.
	if len(results) == 0 {
		return ssn.RealNodesSet[root.Name].IsSuperset(nodes)
	}
	allowed := api.HyperNodeNamesInGradients(results[0].Gradients)
	for _, result := range results[1:] {
		allowed = allowed.Intersection(api.HyperNodeNamesInGradients(result.Gradients))
	}
	for _, candidate := range candidates {
		if allowed.Has(candidate.Name) {
			return true
		}
	}
	return false
}
