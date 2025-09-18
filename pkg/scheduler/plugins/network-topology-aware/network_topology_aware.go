/*
Copyright 2025 The Volcano Authors.

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

package networktopologyaware

import (
	"fmt"
	"math"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "network-topology-aware"
	BaseScore             = 100.0
	ZeroScore             = 0.0
	NetworkTopologyWeight = "weight"

	// Default binpack configuration values
	DefaultResourceWeight = 1
	MinValidWeight        = 0

	// Binpack scoring weights
	// ConsolidationWeight prioritizes resource consolidation and task affinity
	ConsolidationWeight = 0.5
	// CapacityWeight prioritizes layers that can accommodate remaining tasks
	CapacityWeight = 0.5

	// BinpackCPU is the key for weight of cpu
	BinpackCPU = "binpack.cpu"
	// BinpackMemory is the key for weight of memory
	BinpackMemory = "binpack.memory"

	// BinpackResources is the key for additional resource key name
	BinpackResources = "binpack.resources"
	// BinpackResourcesPrefix is the key prefix for additional resource key name
	BinpackResourcesPrefix = BinpackResources + "."

	resourceFmt = "%s[%d]"
)

type networkTopologyAwarePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	weight          int
	binpack         binpackConfig

	// HyperNode
	*hyperNodesTier
	hyperNodeCapacities map[string]*api.Resource
	leafAncestors       map[string][]string
	tierWeights         map[int]float64
}

type binpackConfig struct {
	BinPackingCPU       int
	BinPackingMemory    int
	BinPackingResources map[v1.ResourceName]int
}

func (w *binpackConfig) String() string {
	length := 3
	if extendLength := len(w.BinPackingResources); extendLength == 0 {
		length++
	} else {
		length += extendLength
	}
	msg := make([]string, 0, length)

	if len(w.BinPackingResources) == 0 {
		msg = append(msg, "no extend resources.")
	} else {
		for name, weight := range w.BinPackingResources {
			msg = append(msg, fmt.Sprintf(resourceFmt, name, weight))
		}
	}
	sort.Strings(msg)
	return strings.Join(msg, ", ")
}

type hyperNodesTier struct {
	maxTier int
	minTier int
}

func (h *hyperNodesTier) init(hyperNodesSetByTier []int) {
	if len(hyperNodesSetByTier) == 0 {
		return
	}
	h.minTier = hyperNodesSetByTier[0]
	h.maxTier = hyperNodesSetByTier[len(hyperNodesSetByTier)-1]
}

// New function returns prioritizePlugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &networkTopologyAwarePlugin{
		pluginArguments: arguments,
		hyperNodesTier:  &hyperNodesTier{},
		weight:          calculateWeight(arguments),
		binpack:         calculateBinpackWeight(arguments),
	}
}

func (nta *networkTopologyAwarePlugin) Name() string {
	return PluginName
}

func calculateWeight(args framework.Arguments) int {
	/*
	   The arguments of the networktopologyaware plugin can refer to the following configuration:
	   tiers:
	   - plugins:
	     - name: network-topology-aware
	       arguments:
	         weight: 10
	*/
	weight := 1
	args.GetInt(&weight, NetworkTopologyWeight)
	return weight
}

func calculateBinpackWeight(args framework.Arguments) binpackConfig {
	/*
		User Should give priorityWeight in this format(binpack.config, binpack.cpu, binpack.memory).
		Support change the config about cpu, memory and additional resource by arguments:

		tiers:
		- plugins:
			- name: network-topology-aware
				arguments:
				weight: 20
				binpack.cpu: 1
				binpack.memory: 2
				binpack.resources: nvidia.com/gpu
				binpack.resources.nvidia.com/gpu: 10

		Also, you should set `--percentage-nodes-to-find=100` to make binpack work effectively.
	*/
	config := binpackConfig{
		BinPackingCPU:       DefaultResourceWeight,
		BinPackingMemory:    DefaultResourceWeight,
		BinPackingResources: map[v1.ResourceName]int{},
	}

	// Checks whether binpack.cpu is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&config.BinPackingCPU, BinpackCPU)
	if config.BinPackingCPU < 0 {
		config.BinPackingCPU = MinValidWeight
	}
	// Checks whether binpack.memory is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&config.BinPackingMemory, BinpackMemory)
	if config.BinPackingMemory < 0 {
		config.BinPackingMemory = MinValidWeight
	}

	resourcesStr, ok := args[BinpackResources].(string)
	if !ok {
		resourcesStr = ""
	}

	resources := strings.SplitSeq(resourcesStr, ",")
	for resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		// binpack.resources.[ResourceName]
		resourceKey := BinpackResourcesPrefix + resource
		resourceWeight := 1
		args.GetInt(&resourceWeight, resourceKey)
		if resourceWeight < 0 {
			resourceWeight = MinValidWeight
		}
		config.BinPackingResources[v1.ResourceName(resource)] = resourceWeight
	}

	config.BinPackingResources[v1.ResourceCPU] = config.BinPackingCPU
	config.BinPackingResources[v1.ResourceMemory] = config.BinPackingMemory

	return config
}

func (nta *networkTopologyAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter networkTopologyAwarePlugin plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving networkTopologyAware plugin ...")
	}()
	nta.hyperNodesTier.init(ssn.HyperNodesTiers)

	// Validate tier information before expensive initialization
	if nta.minTier == nta.maxTier && nta.minTier == 0 {
		klog.V(4).Infof("networkTopologyAware: no valid tier information, disabling binpack")
	} else {
		nta.initializeHierarchyCapacity(ssn)
		nta.tierWeights = calculateTierWeights(nta.minTier, nta.maxTier)
		klog.V(4).Infof("networkTopologyAware: binpack initialized with %d tier levels", nta.maxTier-nta.minTier+1)
	}

	// Register scoring functions
	ssn.AddBatchNodeOrderFn(nta.Name(), nta.batchNodeScoringFn(ssn))
}

// batchNodeScoringFn returns a function that scores nodes for task placement.
// The scoring considers both network topology awareness and binpack optimization.
func (nta *networkTopologyAwarePlugin) batchNodeScoringFn(ssn *framework.Session) api.BatchNodeOrderFn {
	return func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64)

		taskJob := ssn.Jobs[task.Job]
		if !taskJob.HasTopologyConstrain() {
			return nodeScores, nil
		}

		jobAllocatedHyperNode := task.JobAllocatedHyperNode

		minHyperNodeOfNodeMap := ssn.MinHyperNodeOfNodeMap
		var resourceRequest *api.Resource
		var binpackScores map[string]float64

		resourceRequest = calculateUnallocatedResources(taskJob)
		allocatedLeafMap := collectAllocatedLeafMap(taskJob.Tasks, minHyperNodeOfNodeMap)
		binpackScores = nta.calculateLeafBinpackScores(nodes, resourceRequest, minHyperNodeOfNodeMap, allocatedLeafMap)

		// Handle subsequent allocation scenarios (combining topology and Binpack scores)
		var maxScore, minScore float64 = -1, math.MaxFloat64
		normalizeScore := func(scores map[string]float64) {
			// Normalize nodeScores to the range [0, BaseScore]
			for nodeName, score := range scores {
				if maxScore-minScore < 1e-6 {
					scores[nodeName] = BaseScore
				} else {
					scores[nodeName] = (score - minScore) / (maxScore - minScore) * BaseScore
				}
				scores[nodeName] *= float64(nta.weight)
			}
		}

		// Handle initial allocation scenario
		if jobAllocatedHyperNode == "" {
			for _, node := range nodes {
				leafNode := minHyperNodeOfNodeMap[node.Name]
				nodeScores[node.Name] = binpackScores[leafNode]
				minScore = min(minScore, nodeScores[node.Name])
				maxScore = max(maxScore, nodeScores[node.Name])
			}

			normalizeScore(nodeScores)
			klog.V(4).Infof("networkTopologyAware initial hyperNode selection scores: %v", nodeScores)
			return nodeScores, nil
		}

		scoreToNodes := map[float64][]string{}
		for _, node := range nodes {
			leafNode := minHyperNodeOfNodeMap[node.Name]

			// Calculate topology score
			score := binpackScores[leafNode]
			nodeScores[node.Name] = score
			if score >= maxScore {
				maxScore = score
				scoreToNodes[maxScore] = append(scoreToNodes[maxScore], node.Name)
			}
			minScore = min(minScore, score)
		}
		// Calculate score based on the number of tasks scheduled for the job when max score of node has more than one.
		if len(scoreToNodes[maxScore]) > 1 {
			candidateNodes := scoreToNodes[maxScore]

			for _, node := range candidateNodes {
				hyperNode := minHyperNodeOfNodeMap[node]
				taskNumScore := nta.scoreWithTaskNum(hyperNode, taskJob, ssn.RealNodesList)
				nodeScores[node] += taskNumScore
				maxScore = max(maxScore, nodeScores[node])
			}
		}

		// Normalize nodeScores to the range [0, BaseScore] * weight
		normalizeScore(nodeScores)
		klog.V(4).Infof("networkTopologyAware node score is: %v", nodeScores)
		return nodeScores, nil
	}
}

func (bp *networkTopologyAwarePlugin) OnSessionClose(ssn *framework.Session) {
}

// Goals:
// - Tasks under a job should be scheduled to one hyperNode as much as possible.
func (nta *networkTopologyAwarePlugin) scoreWithTaskNum(hyperNodeName string, job *api.JobInfo, realNodesList map[string][]*api.NodeInfo) float64 {
	taskNum := util.FindJobTaskNumOfHyperNode(hyperNodeName, job, realNodesList)
	taskNumScore := ZeroScore
	if len(job.Tasks) > 0 {
		// Calculate score: taskNum/allTaskNum
		taskNumScore = BaseScore * scoreHyperNodeWithTaskNum(taskNum, len(job.Tasks))
	}
	return taskNumScore
}

func scoreHyperNodeWithTaskNum(taskNum int, allTaskNum int) float64 {
	// Calculate task distribution rate as score and map the original score to the range between 0 and 1.
	if allTaskNum == 0 {
		return ZeroScore
	}
	return float64(taskNum) / float64(allTaskNum)
}

// calculateLeafBinpackScores computes binpack scores for leaf hypernodes based on resource utilization.
// This function implements a hierarchical scoring approach that considers:
// 1. Resource availability at each tier level
// 2. Existing job allocation to promote task consolidation
// 3. Tier-based weighting to prefer lower tiers (better locality)
//
// The scoring mechanism combines two key metrics:
// - Consolidation Score: Measures resource consolidation efficiency
//   - Prioritizes hypernodes that already contain tasks from the same job (promotes task affinity)
//   - For empty hypernodes, favors higher resource utilization to minimize resource fragmentation
//
// - Capacity Score: Measures the hypernode's ability to accommodate remaining tasks
//   - Gives maximum score to hypernodes that can fit all remaining unscheduled tasks
//   - Partially rewards hypernodes based on how much of the remaining workload they can handle
//
// Parameters:
// - nodes: candidate nodes for scheduling
// - required: total unallocated resources needed by the job
// - leafOfNodeMap: mapping from node names to their leaf hypernode names
// - allocatedLeafMap: set of leaf hypernodes that already contain tasks from this job
//
// Returns a map of hypernode names to their binpack scores.
func (nta *networkTopologyAwarePlugin) calculateLeafBinpackScores(
	nodes []*api.NodeInfo,
	required *api.Resource,
	leafOfNodeMap map[string]string,
	allocatedLeafMap sets.Set[string],
) map[string]float64 {
	scores := make(map[string]float64)
	hyperNodeIdles := make(map[string]*api.Resource)
	leafMap := sets.New[string]()

	for _, node := range nodes {
		leaf := leafOfNodeMap[node.Name]
		leafMap.Insert(leaf)
		for _, hn := range nta.leafAncestors[leaf] {
			if hyperNodeIdles[hn] == nil {
				hyperNodeIdles[hn] = api.EmptyResource()
			}
			if node.Idle != nil {
				hyperNodeIdles[hn].Add(node.Idle)
			}
		}
	}

	hyperNodeAllocated := sets.New[string]()
	for leaf := range allocatedLeafMap {
		for _, hn := range nta.leafAncestors[leaf] {
			hyperNodeAllocated.Insert(hn)
		}
	}

	for leaf := range leafMap {
		totalScore := 0.0

		// Traverse hierarchy from min to max tier
		for tierOffset, hn := range nta.leafAncestors[leaf] {
			consolidationScore, capacityScore := 0.0, 0.0
			tier := nta.minTier + tierOffset
			idle, okIdle := hyperNodeIdles[hn]
			capacity, okCap := nta.hyperNodeCapacities[hn]

			if !okIdle || !okCap {
				continue
			}

			isEnough := required.LessEqual(idle, api.Zero)

			if isEnough {
				// Current layer can accommodate all remaining tasks, capacity score should be 1 (optimal fit)
				capacityScore = 1.0
			} else {
				// Calculate partial capacity satisfaction based on resource gap
				notEnough, _ := required.Diff(idle, api.Zero)
				capacityScore = 1.0 - nta.calculateResourceDiffScore(notEnough, required, required)
			}

			// If current layer already contains tasks of the job, consolidation score should be 1 (maximum affinity)
			if hyperNodeAllocated.Has(hn) {
				consolidationScore = 1.0
			} else if isEnough {
				// For empty hypernodes with sufficient capacity, favor higher resource utilization
				consolidationScore = 1.0 - nta.calculateResourceDiffScore(idle, capacity, required)
			}

			klog.V(5).Infof("networkTopologyAware tier %s (%d) has consolidation: %f, capacity: %f", hn, tier, consolidationScore, capacityScore)
			totalScore += (ConsolidationWeight*consolidationScore + CapacityWeight*capacityScore) * nta.tierWeights[tier]
		}

		scores[leaf] = totalScore * BaseScore
		klog.V(4).Infof("networkTopologyAware leaf %s has binpack score: %f", leaf, scores[leaf])
	}

	return scores
}

// initializeHierarchyCapacity builds capacity hierarchy for hypernodes.
// This function calculates the total capacity for each hypernode by aggregating
// the capacity of all real nodes within its subtree.
//
// The function performs the following steps:
// 1. Iterate through all leaf hypernodes (lowest tier)
// 2. Calculate total capacity for each leaf by summing real node capacities
// 3. Propagate capacity up the hierarchy by adding leaf capacity to ancestors
//
// Error handling: Logs warnings for missing hypernode information but continues processing.
func (nta *networkTopologyAwarePlugin) initializeHierarchyCapacity(ssn *framework.Session) {
	nta.hyperNodeCapacities = make(map[string]*api.Resource)
	nta.leafAncestors = make(map[string][]string)

	hnim := ssn.HyperNodes
	// Check if we have valid tier information
	hyperNodesSet, exists := ssn.HyperNodesSetByTier[nta.minTier]
	if !exists || len(hyperNodesSet) == 0 {
		klog.V(4).Infof("networkTopologyAware: no hypernodes found at min tier %d", nta.minTier)
		return
	}

	for leaf := range hyperNodesSet {
		capacity := api.EmptyResource()

		// Get real nodes for this leaf hypernode
		realNodes, exists := ssn.RealNodesList[leaf]
		if !exists || len(realNodes) == 0 {
			klog.V(4).Infof("networkTopologyAware: no real nodes found for leaf hypernode %s", leaf)
			continue
		}

		for _, rn := range realNodes {
			if rn == nil {
				klog.Warningf("networkTopologyAware: found nil real node in leaf %s", leaf)
				continue
			}
			capacity.Add(rn.Capacity)
		}

		hnAncestors := hnim.GetAncestors(leaf)
		if len(hnAncestors) == 0 {
			klog.V(4).Infof("networkTopologyAware: no ancestors found for leaf %s", leaf)
			continue
		}

		nta.leafAncestors[leaf] = hnAncestors

		for _, hyperNode := range hnAncestors {
			if _, ok := nta.hyperNodeCapacities[hyperNode]; !ok {
				nta.hyperNodeCapacities[hyperNode] = api.EmptyResource()
			}
			nta.hyperNodeCapacities[hyperNode].Add(capacity)
		}
	}

	klog.V(4).Infof("networkTopologyAware: initialized capacity for %d hypernodes", len(nta.hyperNodeCapacities))
}

// collectAllocatedLeafMap gathers all hypernodes in the lowest tier which contains tasks
func collectAllocatedLeafMap(tasks map[api.TaskID]*api.TaskInfo, minHyperNodeOfNodeMap map[string]string) sets.Set[string] {
	leafMap := sets.New[string]()
	for _, task := range tasks {
		if task.NodeName == "" {
			continue
		}
		currentHN := minHyperNodeOfNodeMap[task.NodeName]
		leafMap.Insert(currentHN)
	}
	return leafMap
}

// calculateUnallocatedResources get unallocated resource totals for a job
func calculateUnallocatedResources(job *api.JobInfo) *api.Resource {
	unallocated := api.EmptyResource()
	for _, t := range job.TaskStatusIndex[api.Pending] {
		if t.NodeName != "" {
			continue
		}
		unallocated.Add(t.Resreq)
	}
	return unallocated
}

// calculateResourceDiffScore calculates the weighted resource utilization score based on configured weights.
// It considers all resources specified in binpackWeight configuration.
// Returns a normalized value between 0 (no utilization) and 1 (full utilization).
func (nta *networkTopologyAwarePlugin) calculateResourceDiffScore(target, total, required *api.Resource) float64 {
	score := 0.0
	weightSum := 0

	// Process all resources that have weights configured
	for resource, weight := range nta.binpack.BinPackingResources {
		if weight <= 0 {
			continue
		}

		if required.IsZero(resource) {
			continue
		}

		score += calculateResourceUtilization(target.Get(resource), total.Get(resource)) * float64(weight)
		weightSum += weight
	}

	// Normalize the score if we processed any resources
	if weightSum > 0 {
		return score / float64(weightSum)
	}
	return 0
}

// calculateResourceUtilization calculates utilization for resources
func calculateResourceUtilization(target, capacity float64) float64 {
	if capacity <= 0 || target <= 0 {
		return 0
	}
	return math.Min(1.0, target/capacity)
}

func calculateTierWeights(minTier, maxTier int) map[int]float64 {
	weights := make(map[int]float64)
	total := 0.0

	// Exponential decay weights (higher priority for lower tiers)
	for tier := minTier; tier <= maxTier; tier++ {
		weight := math.Pow(0.5, float64(tier-minTier))
		weights[tier] = weight
		total += weight
	}

	// Normalize weights
	for tier := range weights {
		weights[tier] /= total
	}

	return weights
}
