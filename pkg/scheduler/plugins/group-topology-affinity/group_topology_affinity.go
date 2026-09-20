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

	scheduling "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	PluginName    = "group-topology-affinity"
	PluginWeight  = "weight"
	DefaultWeight = 1
	FullScore     = 1.0
	ZeroScore     = 0.0

	// noAffinityTermIndex marks reject reasons not tied to a specific affinity term.
	noAffinityTermIndex = -1
)

var emptyHyperNodeGradients = [][]*api.HyperNodeInfo{}

type groupTopologyAffinityPlugin struct {
	pluginArguments framework.Arguments
	weight          int
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
	ssn.AddHyperNodeGradientForJobFn(gta.Name(), func(job *api.JobInfo, hyperNode *api.HyperNodeInfo, _ api.SearchPurpose) [][]*api.HyperNodeInfo {
		return gta.hyperNodeGradientForJob(ssn, job, hyperNode)
	})

	ssn.AddHyperNodeGradientForSubJobFn(gta.Name(), func(subJob *api.SubJobInfo, hyperNode *api.HyperNodeInfo, _ api.SearchPurpose) [][]*api.HyperNodeInfo {
		job, ok := ssn.Jobs[subJob.Job]
		if !ok {
			return emptyHyperNodeGradients
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

func (gta *groupTopologyAffinityPlugin) OnSessionClose(ssn *framework.Session) {}

// hyperNodeGradientForJob returns HyperNode candidates for podGroupAntiAffinity.
// Hard required terms filter candidates; jobs without hard rules return the full subtree
// so framework intersection and HyperNodeOrderFn can evaluate preferred terms.
func (gta *groupTopologyAffinityPlugin) hyperNodeGradientForJob(
	ssn *framework.Session,
	job *api.JobInfo,
	root *api.HyperNodeInfo,
) [][]*api.HyperNodeInfo {
	return gta.hyperNodeGradient(ssn, job, root, job.AllocatedHyperNode)
}

func (gta *groupTopologyAffinityPlugin) hyperNodeGradientForSubJob(
	ssn *framework.Session,
	job *api.JobInfo,
	subJob *api.SubJobInfo,
	root *api.HyperNodeInfo,
) [][]*api.HyperNodeInfo {
	return gta.hyperNodeGradient(ssn, job, root, subJob.AllocatedHyperNode)
}

func (gta *groupTopologyAffinityPlugin) hyperNodeGradient(
	ssn *framework.Session,
	job *api.JobInfo,
	root *api.HyperNodeInfo,
	allocatedHyperNode string,
) [][]*api.HyperNodeInfo {
	maxTier := maxHyperNodeTier(ssn.HyperNodesSetByTier)
	hardTerms := job.RequiredPodGroupAntiAffinityTerms()
	if len(hardTerms) > 0 {
		klog.V(3).Infof("podGroup anti-affinity: evaluate gradient, job=%s, rootHyperNode=%s, allocatedHyperNode=%s",
			klog.KRef(job.Namespace, job.Name), root.Name, allocatedHyperNode)
		result, err := gta.buildPodGroupAntiAffinityGradient(
			ssn, job, root, hardTerms, maxTier, allocatedHyperNode,
		)
		if err != nil {
			klog.Errorf("build podGroup anti-affinity gradient failed, job=%s, err=%v", job.UID, err)
			return emptyHyperNodeGradients
		}
		return result
	}

	klog.V(3).Infof("podGroup anti-affinity: gradient full-subtree, job=%s, rootHyperNode=%s, allocatedHyperNode=%s",
		klog.KRef(job.Namespace, job.Name), root.Name, allocatedHyperNode)
	result, err := gta.buildFullHyperNodeGradient(ssn, root, maxTier, allocatedHyperNode)
	if err != nil {
		klog.Errorf("build podGroup anti-affinity full gradient failed, job=%s, err=%v", job.UID, err)
		return emptyHyperNodeGradients
	}
	return result
}

// buildFullHyperNodeGradient returns every HyperNode under the search root up to highestAllowedTier.
// Used when hard podGroupAntiAffinity does not filter candidates; preferred terms are scored in HyperNodeOrderFn.
func (gta *groupTopologyAffinityPlugin) buildFullHyperNodeGradient(
	ssn *framework.Session,
	root *api.HyperNodeInfo,
	highestAllowedTier int,
	allocatedHyperNode string,
) ([][]*api.HyperNodeInfo, error) {
	searchRoot, err := getSearchRootForGradient(
		ssn.HyperNodes, root, highestAllowedTier, allocatedHyperNode,
	)
	if err != nil {
		return nil, err
	}
	eligibleHyperNodes := gta.bfsEligibleHyperNodesUnderRoot(ssn, searchRoot, highestAllowedTier)
	return groupHyperNodesByTierAsc(eligibleHyperNodes), nil
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
	terms []scheduling.PodGroupAffinityTerm,
	highestAllowedTier int,
	allocatedHyperNode string,
) ([][]*api.HyperNodeInfo, error) {
	matchingHyperNodesByTerm, err := collectMatchingHyperNodesByTerm(ssn, job, terms)
	if err != nil {
		return nil, err
	}

	searchRoot, err := getSearchRootForGradient(
		ssn.HyperNodes, root, highestAllowedTier, allocatedHyperNode,
	)
	if err != nil {
		return nil, err
	}

	for index, term := range terms {
		tier, err := api.ResolvePodGroupTermTier(term, ssn.HyperNodeTierNameMap)
		if err != nil {
			klog.V(3).Infof("podGroup anti-affinity: resolve term tier failed, job=%s, termIndex=%d, err=%v",
				klog.KRef(job.Namespace, job.Name), index, err)
			continue
		}
		occupiedHyperNodes := matchingHyperNodesByTerm[index].UnsortedList()
		sort.Strings(occupiedHyperNodes)
		klog.V(3).Infof("podGroup anti-affinity: matching occupancy, job=%s, termIndex=%d, tier=%d, occupiedHyperNodes=%s, matchingPodGroups=%s",
			klog.KRef(job.Namespace, job.Name), index, tier,
			strings.Join(occupiedHyperNodes, ","),
			strings.Join(matchingPodGroupPlacementsForTerm(ssn, job, term), "; "))
	}

	eligibleHyperNodes := gta.bfsAntiAffinityEligibleHyperNodes(
		ssn, job, searchRoot, terms, matchingHyperNodesByTerm, highestAllowedTier,
	)
	klog.V(3).Infof("podGroup anti-affinity: gradient result, job=%s, searchRoot=%s, eligibleHyperNodes=%s",
		klog.KRef(job.Namespace, job.Name), searchRoot.Name, hyperNodeNamesByTier(eligibleHyperNodes))
	return groupHyperNodesByTierAsc(eligibleHyperNodes), nil
}

// collectMatchingHyperNodesByTerm resolves, for each required term, the ancestor HyperNodes
// at the term tier where matching PodGroups are already allocated.
func collectMatchingHyperNodesByTerm(
	ssn *framework.Session,
	job *api.JobInfo,
	terms []scheduling.PodGroupAffinityTerm,
) ([]sets.Set[string], error) {
	matchingHyperNodesByTerm := make([]sets.Set[string], len(terms))
	for index, term := range terms {
		matchingHyperNodes, err := api.MatchingPodGroupsAllocatedHyperNodesForTermWithNamespaceLister(
			ssn.Jobs, ssn.HyperNodes, ssn.HyperNodeTierNameMap, job, term, ssn.RealNodesSet,
			namespaceListerForSession(ssn),
		)
		if err != nil {
			return nil, fmt.Errorf("term %d: %w", index, err)
		}
		matchingHyperNodesByTerm[index] = matchingHyperNodes
	}
	return matchingHyperNodesByTerm, nil
}

func (gta *groupTopologyAffinityPlugin) bfsAntiAffinityEligibleHyperNodes(
	ssn *framework.Session,
	job *api.JobInfo,
	searchRoot *api.HyperNodeInfo,
	terms []scheduling.PodGroupAffinityTerm,
	matchingHyperNodesByTerm []sets.Set[string],
	highestAllowedTier int,
) map[int][]*api.HyperNodeInfo {
	enqueued := set.New[string]()
	processQueue := []*api.HyperNodeInfo{searchRoot}
	enqueued.Insert(searchRoot.Name)

	eligibleByTier := make(map[int][]*api.HyperNodeInfo)
	for len(processQueue) > 0 {
		current := processQueue[0]
		processQueue = processQueue[1:]

		if gta.isEligibleForPodGroupAntiAffinity(
			ssn, job, current, terms, matchingHyperNodesByTerm, highestAllowedTier,
		) {
			eligibleByTier[current.Tier()] = append(eligibleByTier[current.Tier()], current)
		}

		for child := range current.Children {
			if enqueued.Has(child) {
				continue
			}
			processQueue = append(processQueue, ssn.HyperNodes[child])
			enqueued.Insert(child)
		}
	}
	return eligibleByTier
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

// isEligibleForPodGroupAntiAffinity checks whether hn may host the job without violating
// hard podGroupAntiAffinity at each required term tier.
func (gta *groupTopologyAffinityPlugin) isEligibleForPodGroupAntiAffinity(
	ssn *framework.Session,
	job *api.JobInfo,
	hn *api.HyperNodeInfo,
	terms []scheduling.PodGroupAffinityTerm,
	matchingHyperNodesByTerm []sets.Set[string],
	highestAllowedTier int,
) bool {
	if hn.Tier() > highestAllowedTier {
		klog.V(3).Infof("podGroup anti-affinity: reject hyperNode, job=%s, hyperNode=%s, reason=tierAboveHighestAllowed, termIndex=%d, tier=%d, conflictHyperNode=",
			klog.KRef(job.Namespace, job.Name), hn.Name, noAffinityTermIndex, hn.Tier())
		return false
	}

	for index, term := range terms {
		tier, err := api.ResolvePodGroupTermTier(term, ssn.HyperNodeTierNameMap)
		if err != nil {
			klog.V(3).Infof("podGroup anti-affinity: reject hyperNode, job=%s, hyperNode=%s, reason=resolveTermTierFailed, termIndex=%d, tier=0, conflictHyperNode=",
				klog.KRef(job.Namespace, job.Name), hn.Name, index)
			return false
		}
		// Compare at the term tier: reject if this candidate shares an ancestor HyperNode
		// with any matching PodGroup that is already placed there.
		ancestorHyperNode := ssn.HyperNodes.GetAncestorHyperNode(hn.Name, tier)
		if ancestorHyperNode == "" {
			klog.V(3).Infof("podGroup anti-affinity: reject hyperNode, job=%s, hyperNode=%s, reason=emptyAncestorHyperNode, termIndex=%d, tier=%d, conflictHyperNode=",
				klog.KRef(job.Namespace, job.Name), hn.Name, index, tier)
			return false
		}
		if matchingHyperNodesByTerm[index].Has(ancestorHyperNode) {
			klog.V(3).Infof("podGroup anti-affinity: reject hyperNode, job=%s, hyperNode=%s, reason=conflictWithMatchingPodGroup, termIndex=%d, tier=%d, conflictHyperNode=%s",
				klog.KRef(job.Namespace, job.Name), hn.Name, index, tier, ancestorHyperNode)
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
	terms := job.PreferredPodGroupAntiAffinityTerms()
	if len(terms) == 0 {
		return nil, nil
	}

	hyperNodeCandidates := make([]string, 0, len(hyperNodes))
	for hyperNode := range hyperNodes {
		hyperNodeCandidates = append(hyperNodeCandidates, hyperNode)
	}
	sort.Strings(hyperNodeCandidates)
	klog.V(3).Infof("podGroup anti-affinity: evaluate preferred, job=%s, hyperNodeCandidates=%s",
		klog.KRef(job.Namespace, job.Name), strings.Join(hyperNodeCandidates, ","))

	scores := make(map[string]float64, len(hyperNodes))
	for hyperNode := range hyperNodes {
		scores[hyperNode] = FullScore
	}

	matchingHyperNodesByTerm := make([]sets.Set[string], len(terms))
	for termIndex, term := range terms {
		matchingHyperNodes, err := api.MatchingPodGroupsAllocatedHyperNodesForTermWithNamespaceLister(
			ssn.Jobs, ssn.HyperNodes, ssn.HyperNodeTierNameMap, job, term, ssn.RealNodesSet,
			namespaceListerForSession(ssn),
		)
		if err != nil {
			return nil, err
		}
		matchingHyperNodesByTerm[termIndex] = matchingHyperNodes
	}
	for index, term := range terms {
		tier, err := api.ResolvePodGroupTermTier(term, ssn.HyperNodeTierNameMap)
		if err != nil {
			klog.V(3).Infof("podGroup anti-affinity: resolve term tier failed, job=%s, termIndex=%d, err=%v",
				klog.KRef(job.Namespace, job.Name), index, err)
			continue
		}
		occupiedHyperNodes := matchingHyperNodesByTerm[index].UnsortedList()
		sort.Strings(occupiedHyperNodes)
		klog.V(3).Infof("podGroup anti-affinity: matching occupancy, job=%s, termIndex=%d, tier=%d, occupiedHyperNodes=%s, matchingPodGroups=%s",
			klog.KRef(job.Namespace, job.Name), index, tier,
			strings.Join(occupiedHyperNodes, ","),
			strings.Join(matchingPodGroupPlacementsForTerm(ssn, job, term), "; "))
	}

	for termIndex, term := range terms {
		matchingHyperNodes := matchingHyperNodesByTerm[termIndex]
		tier, err := api.ResolvePodGroupTermTier(term, ssn.HyperNodeTierNameMap)
		if err != nil {
			return nil, err
		}

		if term.Weight < 1 || term.Weight > 100 {
			continue
		}
		weightFactor := float64(term.Weight) / 100.0
		for hyperNode := range hyperNodes {
			ancestorHyperNode := ssn.HyperNodes.GetAncestorHyperNode(hyperNode, tier)
			if ancestorHyperNode != "" && matchingHyperNodes.Has(ancestorHyperNode) {
				scoreBefore := scores[hyperNode]
				klog.V(3).Infof("podGroup anti-affinity: preferred penalty, job=%s, hyperNode=%s, termIndex=%d, tier=%d, conflictHyperNode=%s",
					klog.KRef(job.Namespace, job.Name), hyperNode, termIndex, tier, ancestorHyperNode)
				scores[hyperNode] -= weightFactor * FullScore
				if scores[hyperNode] < ZeroScore {
					scores[hyperNode] = ZeroScore
				}
				klog.V(4).Infof("podGroup anti-affinity: preferred score detail, job=%s, hyperNode=%s, termIndex=%d, weight=%d, weightFactor=%.2f, scoreBefore=%.2f, scoreAfter=%.2f, penalty=%.2f",
					klog.KRef(job.Namespace, job.Name), hyperNode, termIndex, term.Weight, weightFactor,
					scoreBefore, scores[hyperNode], scoreBefore-scores[hyperNode])
			}
		}
	}

	for hyperNode, score := range scores {
		scores[hyperNode] = float64(gta.weight) * score * float64(fwk.MaxNodeScore)
	}
	if len(scores) > 0 {
		scoredHyperNodes := make([]string, 0, len(scores))
		for hyperNode := range scores {
			scoredHyperNodes = append(scoredHyperNodes, hyperNode)
		}
		sort.Strings(scoredHyperNodes)

		details := make([]string, 0, len(scoredHyperNodes))
		for _, hyperNode := range scoredHyperNodes {
			details = append(details, fmt.Sprintf("%s:%.2f", hyperNode, scores[hyperNode]))
		}
		klog.V(3).Infof("podGroup anti-affinity: preferred final scores, job=%s, pluginWeight=%d, scores=%s",
			klog.KRef(job.Namespace, job.Name), gta.weight, strings.Join(details, ","))
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

func describeMatchingPodGroupPlacement(
	job *api.JobInfo,
	hyperNodes api.HyperNodeInfoMap,
	nodesByHyperNode map[string]sets.Set[string],
) string {
	if job == nil {
		return ""
	}
	allocatedHyperNode := job.AllocatedHyperNode
	if allocatedHyperNode == "" {
		allocatedHyperNode = api.ComputeJobAllocatedHyperNode(job, hyperNodes, nodesByHyperNode)
	}
	if allocatedHyperNode == "" {
		return ""
	}
	pgName := job.Name
	if job.PodGroup != nil && job.PodGroup.Name != "" {
		pgName = job.PodGroup.Name
	}
	return fmt.Sprintf("%s/%s(hyperNode=%s)", job.Namespace, pgName, allocatedHyperNode)
}

func matchingPodGroupPlacementsForTerm(
	ssn *framework.Session,
	selfJob *api.JobInfo,
	term scheduling.PodGroupAffinityTerm,
) []string {
	placements := make([]string, 0)
	for _, matchingJob := range ssn.Jobs {
		matched, err := api.PodGroupMatchesTermWithNamespaceLister(
			term, selfJob, matchingJob, namespaceListerForSession(ssn),
		)
		if err != nil || !matched {
			continue
		}
		placement := describeMatchingPodGroupPlacement(matchingJob, ssn.HyperNodes, ssn.RealNodesSet)
		if placement == "" {
			continue
		}
		placements = append(placements, placement)
	}
	sort.Strings(placements)
	return placements
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
