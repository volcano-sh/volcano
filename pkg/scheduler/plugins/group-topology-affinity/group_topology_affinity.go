/*
Copyright 2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License and the specific language governing permissions and
limitations under the License.
*/

package grouptopologyaffinity

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/utils/set"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	PluginName    = "group-topology-affinity"
	PluginWeight  = "weight"
	DefaultWeight = 1
	FullScore     = 1.0
	ZeroScore     = 0.0
)

type groupTopologyAffinityPlugin struct {
	pluginArguments   framework.Arguments
	weight            int
	constraintSession *framework.Session
	compiled          map[constraintKey]compiledTerms
}

func New(arguments framework.Arguments) framework.Plugin {
	weight := DefaultWeight
	arguments.GetInt(&weight, PluginWeight)
	if weight < 0 {
		weight = DefaultWeight
	}
	return &groupTopologyAffinityPlugin{
		pluginArguments: arguments,
		weight:          weight,
	}
}

func (gta *groupTopologyAffinityPlugin) Name() string {
	return PluginName
}

func (gta *groupTopologyAffinityPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.EnablePodGroupPlacement(gta.Name())
	ssn.AddHyperNodeCandidateFn(gta.Name(), func(job *api.JobInfo, subJob *api.SubJobInfo, root *api.HyperNodeInfo, candidates []*api.HyperNodeInfo) api.HyperNodeGradientResult {
		placement := job.AllocatedHyperNode
		if subJob != nil {
			placement = subJob.AllocatedHyperNode
		}
		searchRoot, err := getSearchRootForGradient(ssn.HyperNodes, root, maxHyperNodeTier(ssn.HyperNodesSetByTier), placement)
		if err != nil {
			return api.HyperNodeGradientResult{}
		}
		constraints, err := gta.constraintsFor(ssn, job, false)
		if err != nil {
			return api.HyperNodeGradientResult{}
		}
		if len(constraints) == 0 && searchRoot.Name == root.Name {
			return api.HyperNodeGradientResult{Unconstrained: true}
		}
		var eligible []*api.HyperNodeInfo
		for _, candidate := range candidates {
			if ssn.HyperNodes.GetLCAHyperNode(searchRoot.Name, candidate.Name) == searchRoot.Name &&
				satisfiesAntiAffinity(ssn.HyperNodeIndex(), candidate, constraints) {
				eligible = append(eligible, candidate)
			}
		}
		return api.HyperNodeGradientResult{Gradients: [][]*api.HyperNodeInfo{eligible}}
	})
	ssn.AddHyperNodeGradientForJobFn(gta.Name(), func(job *api.JobInfo, hyperNode *api.HyperNodeInfo, _ api.SearchPurpose) api.HyperNodeGradientResult {
		return gta.hyperNodeGradientForJob(ssn, job, hyperNode)
	})

	ssn.AddHyperNodeGradientForSubJobFn(gta.Name(), func(subJob *api.SubJobInfo, hyperNode *api.HyperNodeInfo, _ api.SearchPurpose) api.HyperNodeGradientResult {
		job, ok := ssn.Jobs[subJob.Job]
		if !ok {
			return api.HyperNodeGradientResult{}
		}
		return gta.hyperNodeGradientForSubJob(ssn, job, subJob, hyperNode)
	})

	ssn.AddHyperNodeOrderFn(gta.Name(), func(subJob *api.SubJobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
		job, ok := ssn.Jobs[subJob.Job]
		if !ok {
			return nil, nil
		}
		return gta.hyperNodeOrderFn(ssn, job, hyperNodes)
	})
}

func (gta *groupTopologyAffinityPlugin) OnSessionClose(ssn *framework.Session) {
	gta.constraintSession = nil
	gta.compiled = nil
}

// hyperNodeGradientForJob returns HyperNode candidates for podGroupAntiAffinity.
// Hard required terms filter candidates; jobs without hard rules return an
// unconstrained result unless their existing placement restricts the search root.
func (gta *groupTopologyAffinityPlugin) hyperNodeGradientForJob(
	ssn *framework.Session,
	job *api.JobInfo,
	root *api.HyperNodeInfo,
) api.HyperNodeGradientResult {
	return gta.hyperNodeGradient(ssn, job, root, job.AllocatedHyperNode)
}

func (gta *groupTopologyAffinityPlugin) hyperNodeGradientForSubJob(
	ssn *framework.Session,
	job *api.JobInfo,
	subJob *api.SubJobInfo,
	root *api.HyperNodeInfo,
) api.HyperNodeGradientResult {
	return gta.hyperNodeGradient(ssn, job, root, subJob.AllocatedHyperNode)
}

func (gta *groupTopologyAffinityPlugin) hyperNodeGradient(
	ssn *framework.Session,
	job *api.JobInfo,
	root *api.HyperNodeInfo,
	allocatedHyperNode string,
) api.HyperNodeGradientResult {
	maxTier := maxHyperNodeTier(ssn.HyperNodesSetByTier)
	hardTerms := job.RequiredPodGroupAntiAffinityTerms()
	if len(hardTerms) > 0 {
		klog.V(5).Infof("podGroup anti-affinity: evaluate gradient, job=%s, rootHyperNode=%s, allocatedHyperNode=%s",
			klog.KRef(job.Namespace, job.Name), root.Name, allocatedHyperNode)
		result, err := gta.buildPodGroupAntiAffinityGradient(
			ssn, job, root, maxTier, allocatedHyperNode,
		)
		if err != nil {
			klog.Errorf("build podGroup anti-affinity gradient failed, job=%s, err=%v", job.UID, err)
			return api.HyperNodeGradientResult{}
		}
		return api.HyperNodeGradientResult{Gradients: result}
	}

	searchRoot, err := getSearchRootForGradient(
		ssn.HyperNodes, root, maxTier, allocatedHyperNode,
	)
	if err != nil {
		klog.ErrorS(err, "Resolve podGroup anti-affinity search root failed", "job", job.UID)
		return api.HyperNodeGradientResult{}
	}
	if searchRoot.Name == root.Name {
		return api.HyperNodeGradientResult{Unconstrained: true}
	}
	eligibleHyperNodes := gta.bfsEligibleHyperNodesUnderRoot(ssn, searchRoot, maxTier)
	return api.HyperNodeGradientResult{Gradients: groupHyperNodesByTierAsc(eligibleHyperNodes)}
}

func (gta *groupTopologyAffinityPlugin) bfsEligibleHyperNodesUnderRoot(
	ssn *framework.Session,
	searchRoot *api.HyperNodeInfo,
	highestAllowedTier int,
) map[int][]*api.HyperNodeInfo {
	enqueued := set.New[string]()
	processQueue := []*api.HyperNodeInfo{searchRoot}
	enqueued.Insert(searchRoot.Name)

	eligibleByTier := make(map[int][]*api.HyperNodeInfo)
	for len(processQueue) > 0 {
		current := processQueue[0]
		processQueue = processQueue[1:]

		if current.Tier() <= highestAllowedTier {
			eligibleByTier[current.Tier()] = append(eligibleByTier[current.Tier()], current)
		}

		for child := range current.Children {
			if enqueued.Has(child) {
				continue
			}
			childHN, ok := ssn.HyperNodes[child]
			if !ok {
				continue
			}
			processQueue = append(processQueue, childHN)
			enqueued.Insert(child)
		}
	}
	return eligibleByTier
}

// buildPodGroupAntiAffinityGradient builds topology-only HyperNode gradients for hard
// podGroupAntiAffinity: BFS from the search root, drop candidates whose ancestor HyperNode
// at any required term tier overlaps a matching PodGroup's allocation.
func (gta *groupTopologyAffinityPlugin) buildPodGroupAntiAffinityGradient(
	ssn *framework.Session,
	job *api.JobInfo,
	root *api.HyperNodeInfo,
	highestAllowedTier int,
	allocatedHyperNode string,
) ([][]*api.HyperNodeInfo, error) {
	constraints, err := gta.constraintsFor(ssn, job, false)
	if err != nil {
		return nil, err
	}
	searchRoot, err := getSearchRootForGradient(ssn.HyperNodes, root, highestAllowedTier, allocatedHyperNode)
	if err != nil {
		return nil, err
	}
	eligible := gta.bfsEligibleHyperNodesUnderRoot(ssn, searchRoot, highestAllowedTier)
	for tier, candidates := range eligible {
		filtered := candidates[:0]
		for _, candidate := range candidates {
			if satisfiesAntiAffinity(ssn.HyperNodeIndex(), candidate, constraints) {
				filtered = append(filtered, candidate)
			}
		}
		if len(filtered) == 0 {
			delete(eligible, tier)
		} else {
			eligible[tier] = filtered
		}
	}
	if klog.V(5).Enabled() {
		klog.InfoS("PodGroup anti-affinity gradient result", "job", job.UID,
			"searchRoot", searchRoot.Name, "eligibleHyperNodes", hyperNodeNamesByTier(eligible))
	}
	return groupHyperNodesByTierAsc(eligible), nil
}

// groupHyperNodesByTierAsc groups HyperNodes by tier and returns tiers in ascending order.
func groupHyperNodesByTierAsc(eligibleHyperNodes map[int][]*api.HyperNodeInfo) [][]*api.HyperNodeInfo {
	var tiers []int
	for tier := range eligibleHyperNodes {
		tiers = append(tiers, tier)
	}
	sort.Ints(tiers)

	result := make([][]*api.HyperNodeInfo, 0, len(tiers))
	for _, tier := range tiers {
		result = append(result, eligibleHyperNodes[tier])
	}
	return result
}

// satisfiesAntiAffinity compares a candidate with live peer occupancy at each
// compiled term tier. A missing ancestor cannot satisfy a required term.
func satisfiesAntiAffinity(index *api.HyperNodeIndex, candidate *api.HyperNodeInfo, constraints []antiAffinityConstraint) bool {
	for termIndex, constraint := range constraints {
		ancestor := index.AncestorAtTier(candidate.Name, constraint.tier)
		if ancestor == "" || constraint.occupied.Has(ancestor) {
			klog.V(5).InfoS("PodGroup anti-affinity rejected HyperNode", "hyperNode", candidate.Name,
				"termIndex", termIndex, "comparisonTier", constraint.tier, "conflictHyperNode", ancestor)
			return false
		}
	}
	return true
}

func (gta *groupTopologyAffinityPlugin) hyperNodeOrderFn(
	ssn *framework.Session,
	job *api.JobInfo,
	hyperNodes map[string][]*api.NodeInfo,
) (map[string]float64, error) {
	if !job.HasPreferredPodGroupAntiAffinity() {
		return nil, nil
	}
	constraints, err := gta.constraintsFor(ssn, job, true)
	if err != nil {
		return nil, err
	}
	scores := make(map[string]float64, len(hyperNodes))
	for name := range hyperNodes {
		score := FullScore
		for _, constraint := range constraints {
			if constraint.weight < 1 || constraint.weight > 100 {
				continue
			}
			ancestor := ssn.HyperNodeIndex().AncestorAtTier(name, constraint.tier)
			if ancestor != "" && constraint.occupied.Has(ancestor) {
				score -= float64(constraint.weight) / 100.0
			}
		}
		scores[name] = float64(gta.weight) * max(ZeroScore, score) * float64(fwk.MaxNodeScore)
	}
	if klog.V(5).Enabled() {
		klog.InfoS("PodGroup anti-affinity preferred scores", "job", job.UID, "pluginWeight", gta.weight, "scores", scores)
	}
	return scores, nil
}

func maxHyperNodeTier(hyperNodesSetByTier map[int]sets.Set[string]) int {
	maxTier := 0
	for tier := range hyperNodesSetByTier {
		if tier > maxTier {
			maxTier = tier
		}
	}
	return maxTier
}

func getSearchRootForGradient(
	hyperNodes api.HyperNodeInfoMap,
	hyperNodeAvailable *api.HyperNodeInfo,
	highestAllowedTier int,
	allocatedHyperNode string,
) (*api.HyperNodeInfo, error) {
	if allocatedHyperNode == "" {
		return hyperNodeAvailable, nil
	}

	hyperNodeHighestAllowed, err := getHighestAllowedHyperNode(hyperNodes, highestAllowedTier, allocatedHyperNode)
	if err != nil {
		return nil, fmt.Errorf("get highest allowed hyperNode failed: %w", err)
	}

	lca := hyperNodes.GetLCAHyperNode(hyperNodeAvailable.Name, hyperNodeHighestAllowed)
	if lca == hyperNodeHighestAllowed {
		return hyperNodeAvailable, nil
	}
	if lca == hyperNodeAvailable.Name {
		hni, ok := hyperNodes[hyperNodeHighestAllowed]
		if !ok {
			return nil, fmt.Errorf("failed to get highest allowed HyperNode info for %s", hyperNodeHighestAllowed)
		}
		return hni, nil
	}

	return nil, fmt.Errorf("there is no intersection between hyperNodeAvailable %s and hyperNodeHighestAllowed %s",
		hyperNodeAvailable.Name, hyperNodeHighestAllowed)
}

func getHighestAllowedHyperNode(hyperNodes api.HyperNodeInfoMap, highestAllowedTier int, allocatedHyperNode string) (string, error) {
	var highestAllowedHyperNode string

	for _, ancestor := range hyperNodes.GetAncestors(allocatedHyperNode) {
		hni, ok := hyperNodes[ancestor]
		if !ok {
			return "", fmt.Errorf("allocated hyperNode %s ancestor %s not found", allocatedHyperNode, ancestor)
		}
		if hni.Tier() > highestAllowedTier {
			break
		}
		highestAllowedHyperNode = ancestor
	}

	if highestAllowedHyperNode == "" {
		return "", fmt.Errorf("allocated hyperNode %s tier is greater than highest allowed tier %d", allocatedHyperNode, highestAllowedTier)
	}

	return highestAllowedHyperNode, nil
}

func namespaceListerForSession(ssn *framework.Session) corelisters.NamespaceLister {
	if ssn == nil || ssn.InformerFactory() == nil {
		return nil
	}
	return ssn.InformerFactory().Core().V1().Namespaces().Lister()
}

func hyperNodeNamesByTier(hyperNodesByTier map[int][]*api.HyperNodeInfo) string {
	if len(hyperNodesByTier) == 0 {
		return "{}"
	}
	tiers := make([]int, 0, len(hyperNodesByTier))
	for tier := range hyperNodesByTier {
		tiers = append(tiers, tier)
	}
	sort.Ints(tiers)

	parts := make([]string, 0, len(tiers))
	for _, tier := range tiers {
		names := make([]string, 0, len(hyperNodesByTier[tier]))
		for _, hn := range hyperNodesByTier[tier] {
			names = append(names, hn.Name)
		}
		sort.Strings(names)
		parts = append(parts, fmt.Sprintf("tier-%d:[%s]", tier, strings.Join(names, ",")))
	}
	return "{" + strings.Join(parts, " ") + "}"
}
