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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/utils/set"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "network-topology-aware"
	FullScore             = 1.0
	ZeroScore             = 0.0
	NetworkTopologyWeight = "weight"
	// HyperNodeBinPackCPU is the key for weight of cpu
	HyperNodeBinPackCPU = "hypernode.binpack.cpu"
	// HyperNodeBinPackMemory is the key for weight of memory
	HyperNodeBinPackMemory = "hypernode.binpack.memory"
	// HyperNodeBinPackResources is the key for additional resource key name
	HyperNodeBinPackResources = "hypernode.binpack.resources"
	// HyperNodeBinPackResourcesPrefix is the key prefix for additional resource key name
	HyperNodeBinPackResourcesPrefix = HyperNodeBinPackResources + "."
	// HyperNodeBinPackNormalPodEnable is the key for whether to enable hypernode-level binpacking for pods without network topology
	HyperNodeBinPackNormalPodEnable = "hypernode.binpack.normal-pod.enable"
	// HyperNodeBinPackNormalPodFading is the key for tier weight fading parameter for pods without network topology
	HyperNodeBinPackNormalPodFading = "hypernode.binpack.normal-pod.fading"
)

type networkTopologyAwarePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	weight          *priorityWeight
	*normalPodConfig
	*hyperNodesTier
	// hyperNodeResourceCache stores the resource status of hypernodes to avoid repeated calculation: hypernode -> resource -> status
	hyperNodeResourceCache map[string]map[corev1.ResourceName]*resourceStatus
}

type priorityWeight struct {
	GlobalWeight                 int
	HyperNodeBinPackingCPU       int
	HyperNodeBinPackingMemory    int
	HyperNodeBinPackingResources map[corev1.ResourceName]int
}

type normalPodConfig struct {
	hyperNodeBinPackingEnable bool
	hyperNodeBinPackingFading float64
}

type hyperNodesTier struct {
	maxTier int
	minTier int
}

type resourceStatus struct {
	allocatable float64
	used        float64
}

func (h *hyperNodesTier) init(hyperNodesSetByTier []int) {
	if len(hyperNodesSetByTier) == 0 {
		return
	}
	h.minTier = hyperNodesSetByTier[0]
	h.maxTier = hyperNodesSetByTier[len(hyperNodesSetByTier)-1]
}

/*
   The arguments of the networktopologyaware plugin can refer to the following configuration:
   tiers:
   - plugins:
     - name: network-topology-aware
       arguments:
         weight: 10
         hypernode.binpack.cpu: 5
         hypernode.binpack.memory: 1
         hypernode.binpack.resources: nvidia.com/gpu, example.com/foo
         hypernode.binpack.resources.nvidia.com/gpu: 2
         hypernode.binpack.resources.example.com/foo: 3
         hypernode.binpack.normal-pod.enable: true
         hypernode.binpack.normal-pod.fading: 0.8
*/

// New function returns prioritizePlugin object
func New(arguments framework.Arguments) framework.Plugin {
	plugin := networkTopologyAwarePlugin{
		pluginArguments:        arguments,
		weight:                 getPriorityWeight(arguments),
		normalPodConfig:        getNormalPodConfig(arguments),
		hyperNodesTier:         &hyperNodesTier{},
		hyperNodeResourceCache: make(map[string]map[corev1.ResourceName]*resourceStatus),
	}
	klog.V(5).InfoS("successfully built plugin", "name", PluginName, "arguments", plugin.String())
	return &plugin
}

func (nta *networkTopologyAwarePlugin) Name() string {
	return PluginName
}

func getPriorityWeight(args framework.Arguments) *priorityWeight {
	// Values are initialized to 1.
	weight := priorityWeight{
		GlobalWeight:                 1,
		HyperNodeBinPackingCPU:       1,
		HyperNodeBinPackingMemory:    1,
		HyperNodeBinPackingResources: make(map[corev1.ResourceName]int),
	}

	// Checks whether binpack.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.GlobalWeight, NetworkTopologyWeight)
	if weight.GlobalWeight < 0 {
		weight.GlobalWeight = 1
	}
	// Checks whether binpack.cpu is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.HyperNodeBinPackingCPU, HyperNodeBinPackCPU)
	if weight.HyperNodeBinPackingCPU < 0 {
		weight.HyperNodeBinPackingCPU = 1
	}
	// Checks whether binpack.memory is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.HyperNodeBinPackingMemory, HyperNodeBinPackMemory)
	if weight.HyperNodeBinPackingMemory < 0 {
		weight.HyperNodeBinPackingMemory = 1
	}

	resourcesStr, ok := args[HyperNodeBinPackResources].(string)
	if !ok {
		resourcesStr = ""
	}

	resources := strings.Split(resourcesStr, ",")
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		// binpack.resources.[ResourceName]
		resourceKey := HyperNodeBinPackResourcesPrefix + resource
		resourceWeight := 1
		args.GetInt(&resourceWeight, resourceKey)
		if resourceWeight < 0 {
			resourceWeight = 1
		}
		weight.HyperNodeBinPackingResources[corev1.ResourceName(resource)] = resourceWeight
	}

	return &weight
}

func getNormalPodConfig(args framework.Arguments) *normalPodConfig {
	// default values are respectively true and 0.8
	config := normalPodConfig{
		hyperNodeBinPackingEnable: true,
		hyperNodeBinPackingFading: 0.8,
	}
	args.GetBool(&config.hyperNodeBinPackingEnable, HyperNodeBinPackNormalPodEnable)
	args.GetFloat64(&config.hyperNodeBinPackingFading, HyperNodeBinPackNormalPodFading)
	// config.hyperNodeBinPackingFading could be 0, which implies only the hypernodes of tier 1 affect the pod binpacking scores
	if config.hyperNodeBinPackingFading < 0 {
		config.hyperNodeBinPackingFading = 0.8
	}
	return &config
}

func (w *priorityWeight) getBinPackWeight(name corev1.ResourceName) (int, bool) {
	switch name {
	case corev1.ResourceCPU:
		return w.HyperNodeBinPackingCPU, true
	case corev1.ResourceMemory:
		return w.HyperNodeBinPackingMemory, true
	default:
		weight, ok := w.HyperNodeBinPackingResources[name]
		return weight, ok
	}
}

func (n *networkTopologyAwarePlugin) String() string {
	length := 5
	if extendLength := len(n.weight.HyperNodeBinPackingResources); extendLength == 0 {
		length++
	} else {
		length += extendLength
	}
	msg := make([]string, 0, length)
	msg = append(msg,
		fmt.Sprintf("%s[%d]", NetworkTopologyWeight, n.weight.GlobalWeight),
		fmt.Sprintf("%s[%d]", HyperNodeBinPackCPU, n.weight.HyperNodeBinPackingCPU),
		fmt.Sprintf("%s[%d]", HyperNodeBinPackMemory, n.weight.HyperNodeBinPackingMemory),
	)

	if len(n.weight.HyperNodeBinPackingResources) == 0 {
		msg = append(msg, "no extend resources")
	} else {
		for name, weight := range n.weight.HyperNodeBinPackingResources {
			msg = append(msg, fmt.Sprintf("%s[%d]", name, weight))
		}
	}
	msg = append(msg, fmt.Sprintf("%s[%t]", HyperNodeBinPackNormalPodEnable, n.normalPodConfig.hyperNodeBinPackingEnable),
		fmt.Sprintf("%s[%f]", HyperNodeBinPackNormalPodFading, n.normalPodConfig.hyperNodeBinPackingFading))

	return strings.Join(msg, ", ")
}

func (nta *networkTopologyAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter networkTopologyAwarePlugin plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving networkTopologyAware plugin ...")
	}()
	nta.hyperNodesTier.init(ssn.HyperNodesTiers)

	ssn.AddHyperNodeOrderFn(nta.Name(), func(subJob *api.SubJobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
		return nta.HyperNodeOrderFn(ssn, subJob, hyperNodes)
	})

	ssn.AddBatchNodeOrderFn(nta.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		return nta.batchNodeOrderFn(ssn, task, nodes)
	})

	ssn.AddHyperNodeGradientForJobFn(nta.Name(), func(job *api.JobInfo, hyperNode *api.HyperNodeInfo) [][]*api.HyperNodeInfo {
		if hardMode, highestAllowedTier := job.IsHardTopologyMode(); hardMode {
			result, err := nta.hyperNodeGradientFn(ssn, hyperNode, highestAllowedTier, job.AllocatedHyperNode)
			if err != nil {
				klog.ErrorS(err, "build hyperNode gradient fail", "job", job.UID, "hyperNode", hyperNode.Name,
					"highestAllowedTier", highestAllowedTier, "allocatedHyperNode", job.AllocatedHyperNode)
				return nil
			}
			return result
		}
		return [][]*api.HyperNodeInfo{{hyperNode}}
	})

	ssn.AddHyperNodeGradientForSubJobFn(nta.Name(), func(subJob *api.SubJobInfo, hyperNode *api.HyperNodeInfo) [][]*api.HyperNodeInfo {
		if job, found := ssn.Jobs[subJob.Job]; found && !job.ContainsSubJobPolicy() {
			return [][]*api.HyperNodeInfo{{hyperNode}} // it is unnecessary to try child hyperNode when there is no actual subJob
		}
		if hardMode, highestAllowedTier := subJob.IsHardTopologyMode(); hardMode {
			result, err := nta.hyperNodeGradientFn(ssn, hyperNode, highestAllowedTier, subJob.AllocatedHyperNode)
			if err != nil {
				klog.ErrorS(err, "build hyperNode gradient fail", "subJob", subJob.UID, "hyperNode", hyperNode.Name,
					"highestAllowedTier", highestAllowedTier, "allocatedHyperNode", subJob.AllocatedHyperNode)
				return nil
			}
			return result
		}
		return [][]*api.HyperNodeInfo{{hyperNode}}
	})

	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			task := event.Task
			node := task.NodeName
			for hyperNode := range ssn.HyperNodes {
				if ssn.RealNodesSet[hyperNode].Has(node) {
					if _, foundHyperNode := nta.hyperNodeResourceCache[hyperNode]; foundHyperNode {
						for _, resource := range task.Resreq.ResourceNames() {
							if _, foundResource := nta.hyperNodeResourceCache[hyperNode][resource]; foundResource {
								nta.hyperNodeResourceCache[hyperNode][resource].used += task.Resreq.Get(resource)
							}
						}
					}
				}
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			task := event.Task
			node := task.NodeName
			for hyperNode := range ssn.HyperNodes {
				if ssn.RealNodesSet[hyperNode].Has(node) {
					if _, foundHyperNode := nta.hyperNodeResourceCache[hyperNode]; foundHyperNode {
						for _, resource := range task.Resreq.ResourceNames() {
							if _, foundResource := nta.hyperNodeResourceCache[hyperNode][resource]; foundResource {
								nta.hyperNodeResourceCache[hyperNode][resource].used -= task.Resreq.Get(resource)
							}
						}
					}
				}
			}
		},
	})
}

func (nta *networkTopologyAwarePlugin) HyperNodeOrderFn(ssn *framework.Session, subJob *api.SubJobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
	hyperNodeScores := nta.getSubJobHyperNodeBinPackingScore(ssn, subJob, hyperNodes)

	scoreToHyperNodes := map[float64][]string{}
	var maxScore float64 = -1
	for hyperNode, score := range hyperNodeScores {
		if score >= maxScore {
			maxScore = score
			scoreToHyperNodes[maxScore] = append(scoreToHyperNodes[maxScore], hyperNode)
		}
	}

	// Calculate score based on the number of tasks scheduled for the job when max score of hyperNode has more than one.
	if len(scoreToHyperNodes[maxScore]) > 1 {
		candidateHyperNodes := scoreToHyperNodes[maxScore]
		for _, hyperNode := range candidateHyperNodes {
			taskNumScore := nta.scoreWithTaskNum(hyperNode, subJob.Tasks, ssn.RealNodesList)
			hyperNodeScores[hyperNode] += taskNumScore
		}
	}

	hyperNodeScores = nta.normalizeFinalScore(hyperNodeScores)
	klog.V(4).Infof("networkTopologyAware hyperNode score is: %v", hyperNodeScores)
	return hyperNodeScores, nil
}

func (nta *networkTopologyAwarePlugin) getSubJobHyperNodeBinPackingScore(ssn *framework.Session, subJob *api.SubJobInfo, hyperNodes map[string][]*api.NodeInfo) map[string]float64 {
	tasksRequest := make(map[corev1.ResourceName]float64)
	// currently, the subJob can only be fully scheduled (minAvailable == taskNum)
	for _, task := range subJob.Tasks {
		for _, resourceName := range task.Resreq.ResourceNames() {
			if _, ok := nta.weight.getBinPackWeight(resourceName); !ok {
				continue
			}
			tasksRequest[resourceName] += task.Resreq.Get(resourceName)
		}
	}

	hyperNodeBinPackingScores := make(map[string]float64)
	for hyperNode := range hyperNodes {
		totalScore := 0.0
		totalWeight := 0
		overused := false

		for resourceName, request := range tasksRequest {
			weight, ok := nta.weight.getBinPackWeight(resourceName)
			if !ok {
				continue
			}
			nta.calculateHyperNodeResourceStatus(ssn, hyperNode, resourceName)

			allocatable := nta.hyperNodeResourceCache[hyperNode][resourceName].allocatable
			used := nta.hyperNodeResourceCache[hyperNode][resourceName].used
			if used+request > allocatable {
				klog.V(4).InfoS("cannot binpack the hyperNode", "subJob", subJob.UID, "hyperNode", hyperNode,
					"resource", resourceName, "allocatable", allocatable, "used", used, "request", request)
				overused = true
				break
			}
			score := (used + request) / allocatable
			klog.V(5).InfoS("hyperNode binpacking score calculation", "subJob", subJob.UID, "hyperNode", hyperNode,
				"resource", resourceName, "allocatable", allocatable, "used", used, "request", request)

			totalScore += float64(weight) * score
			totalWeight += weight
		}

		if overused || totalWeight <= 0 {
			hyperNodeBinPackingScores[hyperNode] = ZeroScore
		} else {
			hyperNodeBinPackingScores[hyperNode] = totalScore / float64(totalWeight)
		}
	}
	return hyperNodeBinPackingScores
}

func (nta *networkTopologyAwarePlugin) batchNodeOrderFn(ssn *framework.Session, task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	nodeScores := make(map[string]float64)
	var err error

	job := ssn.Jobs[task.Job]
	subJob := job.SubJobs[job.TaskToSubJob[task.UID]]
	if subJob.WithNetworkTopology() {
		nodeScores, err = nta.batchNodeOrderFnForNetworkAwarePods(ssn, task, subJob, nodes)
	} else {
		nodeScores, err = nta.batchNodeOrderFnForNormalPods(ssn, task, nodes)
	}

	if err != nil {
		return nil, err
	}
	nodeScores = nta.normalizeFinalScore(nodeScores)
	klog.V(4).Infof("networkTopologyAware node score is: %v", nodeScores)
	return nodeScores, nil
}

func (nta *networkTopologyAwarePlugin) batchNodeOrderFnForNormalPods(ssn *framework.Session, task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	nodeScores := make(map[string]float64)

	if !nta.normalPodConfig.hyperNodeBinPackingEnable {
		return nodeScores, nil
	}

	totalTierWeight := 0.0
	tierWeights := make(map[int]float64)
	for tier := nta.hyperNodesTier.minTier; tier <= nta.hyperNodesTier.maxTier; tier++ {
		// note: math.Pow(0, 0) = 1
		tierWeight := math.Pow(nta.hyperNodeBinPackingFading, float64(tier-1))
		totalTierWeight += tierWeight
		tierWeights[tier] = tierWeight
	}
	if totalTierWeight <= 0 {
		return nodeScores, nil
	}

	for _, node := range nodes {
		totalScore := 0.0
		for tier := nta.hyperNodesTier.minTier; tier <= nta.hyperNodesTier.maxTier; tier++ {
			tierScore := FullScore
			for hyperNodeName := range ssn.HyperNodesSetByTier[tier] {
				if ssn.RealNodesSet[hyperNodeName].Has(node.Name) {
					tierScore = nta.getPodHyperNodeBinPackingScore(ssn, task, hyperNodeName)
					break
				}
			}
			totalScore += tierWeights[tier] * tierScore
		}
		nodeScores[node.Name] = totalScore / totalTierWeight
	}
	return nodeScores, nil
}

func (nta *networkTopologyAwarePlugin) getPodHyperNodeBinPackingScore(ssn *framework.Session, task *api.TaskInfo, hyperNode string) float64 {
	totalScore := 0.0
	totalWeight := 0

	for _, resource := range task.Resreq.ResourceNames() {
		weight, found := nta.weight.getBinPackWeight(resource)
		if !found {
			continue
		}
		request := task.Resreq.Get(resource)

		nta.calculateHyperNodeResourceStatus(ssn, hyperNode, resource)
		allocatable := nta.hyperNodeResourceCache[hyperNode][resource].allocatable
		used := nta.hyperNodeResourceCache[hyperNode][resource].used

		if used+request > allocatable {
			klog.V(4).InfoS("cannot binpack the hyperNode", "task", task.UID, "hyperNode", hyperNode,
				"resource", resource, "allocatable", allocatable, "used", used, "request", request)
			return ZeroScore
		}

		score := (used + request) / allocatable
		klog.V(5).InfoS("hyperNode binpacking score calculation", "task", task.UID, "hyperNode", hyperNode,
			"resource", resource, "allocatable", allocatable, "used", used, "request", request)

		totalScore += float64(weight) * score
		totalWeight += weight
	}

	if totalWeight > 0 {
		return totalScore / float64(totalWeight)
	}
	return ZeroScore
}

func (nta *networkTopologyAwarePlugin) calculateHyperNodeResourceStatus(ssn *framework.Session, hyperNode string, resource corev1.ResourceName) {
	if _, foundHyperNode := nta.hyperNodeResourceCache[hyperNode]; !foundHyperNode {
		nta.hyperNodeResourceCache[hyperNode] = make(map[corev1.ResourceName]*resourceStatus)
	}
	if _, foundResource := nta.hyperNodeResourceCache[hyperNode][resource]; !foundResource {
		allocatable := 0.0
		used := 0.0
		for node := range ssn.RealNodesSet[hyperNode] {
			allocatable += ssn.Nodes[node].Allocatable.Get(resource)
			used += ssn.Nodes[node].Used.Get(resource)
		}
		nta.hyperNodeResourceCache[hyperNode][resource] = &resourceStatus{
			allocatable: allocatable,
			used:        used,
		}
	}
}

func (nta *networkTopologyAwarePlugin) batchNodeOrderFnForNetworkAwarePods(ssn *framework.Session, task *api.TaskInfo, subJob *api.SubJobInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
	nodeScores := make(map[string]float64)

	allocatedHyperNode := task.JobAllocatedHyperNode
	if allocatedHyperNode == "" {
		return nodeScores, nil
	}
	// Calculate score based on LCAHyperNode tier.
	var maxScore float64 = -1
	scoreToNodes := map[float64][]string{}
	for _, node := range nodes {
		hyperNode := util.FindHyperNodeForNode(node.Name, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
		score := nta.networkTopologyAwareScore(hyperNode, allocatedHyperNode, ssn.HyperNodes)
		nodeScores[node.Name] = score
		if score >= maxScore {
			maxScore = score
			scoreToNodes[maxScore] = append(scoreToNodes[maxScore], node.Name)
		}
	}
	// Calculate score based on the number of tasks scheduled for the subjob when max score of node has more than one.
	if len(scoreToNodes[maxScore]) > 1 {
		candidateNodes := scoreToNodes[maxScore]
		for _, node := range candidateNodes {
			hyperNode := util.FindHyperNodeForNode(node, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
			taskNumScore := nta.scoreWithTaskNum(hyperNode, subJob.Tasks, ssn.RealNodesList)
			nodeScores[node] += taskNumScore
		}
	}

	return nodeScores, nil
}

func (nta *networkTopologyAwarePlugin) hyperNodeGradientFn(ssn *framework.Session, hyperNode *api.HyperNodeInfo, highestAllowedTier int, allocatedHyperNode string) ([][]*api.HyperNodeInfo, error) {
	enqueued := set.New[string]()
	var processQueue []*api.HyperNodeInfo

	searchRoot, err := getSearchRoot(ssn.HyperNodes, hyperNode, highestAllowedTier, allocatedHyperNode)
	if err != nil {
		return nil, fmt.Errorf("getSearchRoot failed: %w", err)
	}

	processQueue = append(processQueue, searchRoot)
	enqueued.Insert(searchRoot.Name)

	eligibleHyperNodes := make(map[int][]*api.HyperNodeInfo)
	for len(processQueue) > 0 {
		// pop one hyperNode from queue
		current := processQueue[0]
		processQueue = processQueue[1:]

		if current.Tier() <= highestAllowedTier {
			eligibleHyperNodes[current.Tier()] = append(eligibleHyperNodes[current.Tier()], current)
		}

		// push children hyperNode into queue
		for child := range current.Children {
			if enqueued.Has(child) {
				continue
			}
			processQueue = append(processQueue, ssn.HyperNodes[child])
			enqueued.Insert(child)
		}
	}

	// organize hyperNode gradients by tiers in ascending order
	var tiers []int
	for tier := range eligibleHyperNodes {
		tiers = append(tiers, tier)
	}
	sort.Ints(tiers)

	var result [][]*api.HyperNodeInfo
	for _, tier := range tiers {
		result = append(result, eligibleHyperNodes[tier])
	}

	return result, nil
}

// getSearchRoot first computes the maximum allowable HyperNode subtree for the Job/SubJob based on `allocatedHyperNode`,
// then **intersects** it with the HyperNode subtree constrained by the external caller(`hyperNodeAvailable`),
// ensuring that the returned HyperNode subtree satisfies both the Job's(/SubJob's) network topology constraints
// and the caller's constraints.
func getSearchRoot(hyperNodes api.HyperNodeInfoMap, hyperNodeAvailable *api.HyperNodeInfo, highestAllowedTier int, allocatedHyperNode string) (*api.HyperNodeInfo, error) {
	if allocatedHyperNode == "" {
		return hyperNodeAvailable, nil
	}

	hyperNodeHighestAllowed, err := getHighestAllowedHyperNode(hyperNodes, highestAllowedTier, allocatedHyperNode)
	if err != nil {
		return nil, fmt.Errorf("get highest allowed hyperNode failed: %w", err)
	}

	// returns the intersection of hyperNodeAvailable and hyperNodeHighestAllowed
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

	ancestors := hyperNodes.GetAncestors(allocatedHyperNode)
	for _, ancestor := range ancestors {
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

func (nta *networkTopologyAwarePlugin) OnSessionClose(ssn *framework.Session) {
}

// networkTopologyAwareScore use the best fit polices during scheduling.

// Goals:
// - The tier of LCAHyperNode of the hyperNode and the job allocatedHyperNode should be as low as possible.
func (nta *networkTopologyAwarePlugin) networkTopologyAwareScore(hyperNodeName, jobAllocatedHyperNode string, hyperNodeMap api.HyperNodeInfoMap) float64 {
	if hyperNodeName == "" || jobAllocatedHyperNode == "" {
		return ZeroScore
	}
	if hyperNodeName == jobAllocatedHyperNode {
		return FullScore
	}
	LCAHyperNode := hyperNodeMap.GetLCAHyperNode(hyperNodeName, jobAllocatedHyperNode)
	hyperNodeInfo, ok := hyperNodeMap[LCAHyperNode]
	if !ok {
		return ZeroScore
	}
	// Calculate score: (maxTier - LCAhyperNode.tier)/(maxTier - minTier)
	hyperNodeTierScore := nta.scoreHyperNodeWithTier(hyperNodeInfo.Tier())
	return hyperNodeTierScore
}

// Goals:
// - Tasks under a job should be scheduled to one hyperNode as much as possible.
func (nta *networkTopologyAwarePlugin) scoreWithTaskNum(hyperNodeName string, tasks api.TasksMap, realNodesList map[string][]*api.NodeInfo) float64 {
	taskNum := util.FindJobTaskNumOfHyperNode(hyperNodeName, tasks, realNodesList)
	taskNumScore := ZeroScore
	if len(tasks) > 0 {
		// Calculate score: taskNum/allTaskNum
		taskNumScore = scoreHyperNodeWithTaskNum(taskNum, len(tasks))
	}
	return taskNumScore
}

func (nta *networkTopologyAwarePlugin) scoreHyperNodeWithTier(tier int) float64 {
	// Use tier to calculate scores and map the original score to the range between 0 and 1.
	if nta.minTier == nta.maxTier {
		return FullScore
	}
	if nta.minTier <= tier && tier <= nta.maxTier {
		return float64(nta.maxTier-tier) / float64(nta.maxTier-nta.minTier)
	}
	return ZeroScore
}

func scoreHyperNodeWithTaskNum(taskNum int, allTaskNum int) float64 {
	// Calculate task distribution rate as score and map the original score to the range between 0 and 1.
	if allTaskNum == 0 {
		return FullScore
	}
	return float64(taskNum) / float64(allTaskNum)
}

func (nta *networkTopologyAwarePlugin) normalizeFinalScore(scores map[string]float64) map[string]float64 {
	normalizedScores := make(map[string]float64)
	for name, score := range scores {
		normalizedScores[name] = float64(k8sFramework.MaxNodeScore) * float64(nta.weight.GlobalWeight) * score
	}
	return normalizedScores
}
