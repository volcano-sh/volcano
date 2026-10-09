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

package grouptopologyaffinity

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// compiledTerm retains only Session-stable policy and peer selection. The peer
// pointers deliberately refer to live Session Jobs so dry-runs remain visible.
type compiledTerm struct {
	tier   int
	weight int32
	peers  []*api.JobInfo
}

type compiledTerms struct {
	terms []compiledTerm
	err   error
}

type constraintKey struct {
	job       *api.JobInfo
	preferred bool
}

type antiAffinityConstraint struct {
	tier     int
	weight   int32
	occupied sets.Set[string]
}

// enablePlacementTracking compiles active rules before any Job can be scheduled.
// Peers need tracking even if they run before the Job selecting them. Compilation
// is shared with callbacks; mutable occupancy is still read at evaluation time.
func (gta *groupTopologyAffinityPlugin) enablePlacementTracking(ssn *framework.Session) {
	if !ssn.HyperNodesReadyToSchedule {
		return
	}
	var gradientEnabled, orderEnabled bool
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name == gta.Name() {
				gradientEnabled = gradientEnabled || ptr.Deref(plugin.EnabledHyperNodeGradient, false)
				orderEnabled = orderEnabled || ptr.Deref(plugin.EnabledHyperNodeOrder, false)
			}
		}
	}
	if !gradientEnabled && !orderEnabled {
		return
	}
	for _, job := range ssn.Jobs {
		if job == nil {
			continue
		}
		for _, preferred := range []bool{false, true} {
			if (!preferred && (!gradientEnabled || !job.ContainsHardPodGroupAntiAffinity())) ||
				(preferred && (!orderEnabled || !job.HasPreferredPodGroupAntiAffinity())) {
				continue
			}
			ssn.EnablePodGroupPlacement(job)
			compiled := gta.compiledTermsFor(ssn, job, preferred)
			// Callback evaluation reports compilation errors and rejects required
			// rules. An invalid rule must not enable tracking for unrelated Jobs.
			if compiled.err != nil {
				continue
			}
			for _, term := range compiled.terms {
				for _, peer := range term.peers {
					ssn.EnablePodGroupPlacement(peer)
				}
			}
		}
	}
}

func (gta *groupTopologyAffinityPlugin) compiledTermsFor(ssn *framework.Session, job *api.JobInfo, preferred bool) compiledTerms {
	if gta.constraintSession != ssn {
		gta.constraintSession = ssn
		gta.compiled = make(map[constraintKey]compiledTerms)
	}
	key := constraintKey{job: job, preferred: preferred}
	compiled, found := gta.compiled[key]
	if !found {
		terms := job.RequiredPodGroupAntiAffinityTerms()
		if preferred {
			terms = job.PreferredPodGroupAntiAffinityTerms()
		}
		compiled.terms, compiled.err = compileTerms(ssn, job, terms)
		gta.compiled[key] = compiled
	}
	return compiled
}

func (gta *groupTopologyAffinityPlugin) constraintsFor(ssn *framework.Session, job *api.JobInfo, preferred bool) ([]antiAffinityConstraint, error) {
	compiled := gta.compiledTermsFor(ssn, job, preferred)
	if compiled.err != nil {
		return nil, compiled.err
	}
	constraints := make([]antiAffinityConstraint, 0, len(compiled.terms))
	// Occupancy is reused across terms at the same tier during this evaluation,
	// but never retained across allocation, pipelining, eviction or rollback.
	occupancy := make(map[int]map[api.JobID]sets.Set[string])
	for i, term := range compiled.terms {
		constraint := antiAffinityConstraint{tier: term.tier, weight: term.weight, occupied: sets.New[string]()}
		if occupancy[term.tier] == nil {
			occupancy[term.tier] = make(map[api.JobID]sets.Set[string])
		}
		for _, peer := range term.peers {
			domains, found := occupancy[term.tier][peer.UID]
			if !found {
				domains = ssn.HyperNodeIndex().OccupiedHyperNodes(peer, term.tier)
				occupancy[term.tier][peer.UID] = domains
			}
			for domain := range domains {
				constraint.occupied.Insert(domain)
			}
			if klog.V(5).Enabled() {
				klog.InfoS("Matched PodGroup topology occupancy", "job", job.UID, "peer", peer.UID,
					"termIndex", i, "comparisonTier", term.tier, "occupiedHyperNodes", sets.List(domains))
			}
		}
		constraints = append(constraints, constraint)
	}
	return constraints, nil
}

func compileTerms(ssn *framework.Session, job *api.JobInfo, terms []scheduling.PodGroupAffinityTerm) ([]compiledTerm, error) {
	compiled := make([]compiledTerm, 0, len(terms))
	for i, term := range terms {
		tier, err := api.ResolvePodGroupTermTier(term, ssn.HyperNodeTierNameMap)
		if err != nil {
			return nil, fmt.Errorf("term %d: %w", i, err)
		}
		selector, err := metav1.LabelSelectorAsSelector(term.PodGroupSelector)
		if err != nil {
			return nil, fmt.Errorf("term %d PodGroup selector: %w", i, err)
		}
		namespaceSelector, err := metav1.LabelSelectorAsSelector(term.NamespaceSelector)
		if err != nil {
			return nil, fmt.Errorf("term %d Namespace selector: %w", i, err)
		}
		resolved := compiledTerm{tier: tier, weight: term.Weight}
		lister := namespaceListerForSession(ssn)
		namespaces := make(map[string]bool)
		for _, peer := range ssn.Jobs {
			if peer == nil || peer.PodGroup == nil || peer.UID == job.UID {
				continue
			}
			matchesNamespace := peer.Namespace == job.Namespace
			if term.NamespaceSelector != nil && lister != nil {
				var found bool
				matchesNamespace, found = namespaces[peer.Namespace]
				if !found {
					namespace, err := lister.Get(peer.Namespace)
					if err != nil {
						return nil, fmt.Errorf("term %d Namespace lookup: %w", i, err)
					}
					matchesNamespace = namespaceSelector.Matches(labels.Set(namespace.Labels))
					namespaces[peer.Namespace] = matchesNamespace
				}
			}
			if matchesNamespace && selector.Matches(labels.Set(peer.PodGroup.Labels)) {
				resolved.peers = append(resolved.peers, peer)
			}
		}
		compiled = append(compiled, resolved)
	}
	return compiled, nil
}
