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
)

type networkTopologyAwarePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	weight          int
	*hyperNodesTier
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

func (nta *networkTopologyAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter networkTopologyAwarePlugin plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving networkTopologyAware plugin ...")
	}()
	nta.hyperNodesTier.init(ssn.HyperNodesTiers)

	nodeFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64)

		taskJob := ssn.Jobs[task.Job]
		if !taskJob.HasTopologyConstrain() {
			return nodeScores, nil
		}

		jobAllocatedHyperNode := task.JobAllocatedHyperNode
		if jobAllocatedHyperNode == "" {
			return nodeScores, nil
		}
		// Calculate score based on LCAHyperNode tier.
		var maxScore float64 = -1
		scoreToNodes := map[float64][]string{}
		for _, node := range nodes {
			hyperNode := util.FindHyperNodeForNode(node.Name, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
			score := nta.networkTopologyAwareScore(hyperNode, jobAllocatedHyperNode, ssn.HyperNodes)
			score *= float64(nta.weight)
			nodeScores[node.Name] = score
			if score >= maxScore {
				maxScore = score
				scoreToNodes[maxScore] = append(scoreToNodes[maxScore], node.Name)
			}
		}
		// Calculate score based on the number of tasks scheduled for the job when max score of node has more than one.
		if len(scoreToNodes[maxScore]) > 1 {
			candidateNodes := scoreToNodes[maxScore]
			for _, node := range candidateNodes {
				hyperNode := util.FindHyperNodeForNode(node, ssn.RealNodesList, ssn.HyperNodesTiers, ssn.HyperNodesSetByTier)
				taskNumScore := nta.scoreWithTaskNum(hyperNode, taskJob, ssn.RealNodesList)
				taskNumScore *= float64(nta.weight)
				nodeScores[node] += taskNumScore
			}
		}

		klog.V(4).Infof("networkTopologyAware node score is: %v", nodeScores)
		return nodeScores, nil
	}
	ssn.AddBatchNodeOrderFn(nta.Name(), nodeFn)
}

func (bp *networkTopologyAwarePlugin) OnSessionClose(ssn *framework.Session) {
}

// networkTopologyAwareScore use the best fit polices during scheduling.

// Goals:
// - The tier of LCAHyperNode of the hyperNode and the job allocatedHyperNode should be as low as possible.
func (nta *networkTopologyAwarePlugin) networkTopologyAwareScore(hyperNodeName, jobAllocatedHyperNode string, hyperNodeMap api.HyperNodeInfoMap) float64 {
	if hyperNodeName == jobAllocatedHyperNode {
		return BaseScore
	}
	LCAHyperNode := hyperNodeMap.GetLCAHyperNode(hyperNodeName, jobAllocatedHyperNode)
	hyperNodeInfo, ok := hyperNodeMap[LCAHyperNode]
	if !ok {
		return ZeroScore
	}
	// Calculate score: (maxTier - LCAhyperNode.tier)/(maxTier - minTier)
	hyperNodeTierScore := BaseScore * nta.scoreHyperNodeWithTier(hyperNodeInfo.Tier())
	return hyperNodeTierScore
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

func (nta *networkTopologyAwarePlugin) scoreHyperNodeWithTier(tier int) float64 {
	// Use tier to calculate scores and map the original score to the range between 0 and 1.
	if nta.minTier == nta.maxTier {
		return ZeroScore
	}
	return float64(nta.maxTier-tier) / float64(nta.maxTier-nta.minTier)
}

func scoreHyperNodeWithTaskNum(taskNum int, allTaskNum int) float64 {
	// Calculate task distribution rate as score and map the original score to the range between 0 and 1.
	if allTaskNum == 0 {
		return ZeroScore
	}
	return float64(taskNum) / float64(allTaskNum)
}
