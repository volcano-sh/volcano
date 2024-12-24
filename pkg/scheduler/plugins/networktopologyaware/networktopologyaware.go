/*
Copyright 2019 The Volcano Authors.

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
	PluginName            = "networktopologyaware"
	BaseScore             = 100.0
	TaskBaseScore         = 10.0
	ZeroScore             = 0.0
	NetworkTopologyWeight = "weight"
)

type networkTopologyAwarePlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns prioritizePlugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &networkTopologyAwarePlugin{
		pluginArguments: arguments,
	}
}

func (nta *networkTopologyAwarePlugin) Name() string {
	return PluginName
}

func calculateWeight(args framework.Arguments) int {
	weight := 1
	args.GetInt(&weight, NetworkTopologyWeight)
	return weight
}

func (nta *networkTopologyAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter networkTopologyAwarePlugin plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving networkTopologyAware plugin ...")
	}()

	weight := calculateWeight(nta.pluginArguments)
	hyperNodeFn := func(job *api.JobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
		hyperNodeScores := make(map[string]float64)
		jobHyperNode := job.PodGroup.GetAnnotations()[api.TopologyAllocateLCAHyperNode]
		// job is scheduled for the first time, All hyperNodes have the same score..
		if jobHyperNode == "" {
			for hyperNode := range hyperNodes {
				hyperNodeScores[hyperNode] = ZeroScore
			}
			return hyperNodeScores, nil
		}
		// job is not scheduled for the first time, calculate score based on hypernode tree.
		maxScore := ZeroScore
		scoreHyperNode := map[float64][]string{}
		for hyperNode := range hyperNodes {
			score := networkTopologyAwareScore(hyperNode, job, ssn.HyperNodeTree)
			score *= float64(weight)
			hyperNodeScores[hyperNode] = score
			if score >= maxScore {
				maxScore = score
				scoreHyperNode[score] = append(scoreHyperNode[score], hyperNode)
			}
		}
		// calculate score based on task num if max score hyperNode has more than one.
		if len(scoreHyperNode[maxScore]) > 1 {
			for hyperNode, score := range hyperNodeScores {
				if score == maxScore {
					taskNumScore := networkTopologyAwareScoreWithTaskNum(hyperNode, job, ssn.HyperNodes)
					taskNumScore *= float64(weight)
					hyperNodeScores[hyperNode] += taskNumScore
				}
			}
		}

		klog.V(1).Infof("networkTopologyAware score is: %v", hyperNodeScores)
		return hyperNodeScores, nil
	}

	nodeFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64)

		taskJob := ssn.Jobs[task.Job]
		jobHyperNode := taskJob.PodGroup.GetAnnotations()[api.TopologyAllocateLCAHyperNode]
		// job fist first scheduler, All node have the same score.
		if jobHyperNode == "" {
			for _, node := range nodes {
				nodeScores[node.Name] = ZeroScore
			}
			return nodeScores, nil
		}
		// job not first scheduler, calculate score based on hypernode tree.
		for _, node := range nodes {
			hyperNode := util.FindHyperNodeOfNode(node.Name, ssn.HyperNodes)
			score := networkTopologyAwareScore(hyperNode, taskJob, ssn.HyperNodeTree)
			score *= float64(weight)
			nodeScores[node.Name] = score
		}

		maxScore := ZeroScore
		scoreNodes := map[float64][]string{}
		for _, node := range nodes {
			hyperNode := util.FindHyperNodeOfNode(node.Name, ssn.HyperNodes)
			score := networkTopologyAwareScore(hyperNode, taskJob, ssn.HyperNodeTree)
			score *= float64(weight)
			nodeScores[node.Name] = score
			if score >= maxScore {
				maxScore = score
				scoreNodes[score] = append(scoreNodes[score], node.Name)
			}
		}
		// calculate score based on task num if max score hyperNode has more than one.
		if len(scoreNodes[maxScore]) > 1 {
			for node, score := range nodeScores {
				if score == maxScore {
					hyperNode := util.FindHyperNodeOfNode(node, ssn.HyperNodes)
					taskNumScore := networkTopologyAwareScoreWithTaskNum(hyperNode, taskJob, ssn.HyperNodes)
					taskNumScore *= float64(weight)
					nodeScores[node] += taskNumScore
				}
			}
		}

		klog.V(1).Infof("networkTopologyAware score is: %v", nodeScores)
		return nodeScores, nil
	}

	ssn.AddHyperNodeOrederFn(nta.Name(), hyperNodeFn)
	ssn.AddBatchNodeOrderFn(nta.Name(), nodeFn)
}

func (bp *networkTopologyAwarePlugin) OnSessionClose(ssn *framework.Session) {
}

// networkTopologyAwareScore use the best fit polices during scheduling.

// Goals:
// - The tier index to which the LCAHyperNode of a job belongs should be as low as possible.
func networkTopologyAwareScore(hyperNodeName string, job *api.JobInfo, hyperNodeTree []map[string][]string) float64 {
	jobHyperNode := job.PodGroup.GetAnnotations()[api.TopologyAllocateLCAHyperNode]

	if jobHyperNode == hyperNodeName {
		return BaseScore
	}
	// Calculate hyperNode tier index score.
	_, index := util.FindLCAHyperNode(hyperNodeName, jobHyperNode, hyperNodeTree)
	if index <= 0 {
		klog.V(4).Infof("find LCAhyperNode failed wtih %s in hyperNodeTree", hyperNodeName)
		return 0.0
	}
	tierIndexScore := BaseScore * scoreHyperNodeWithIndex(index, 1, len(hyperNodeTree))

	return tierIndexScore
}

// Goals:
// - Tasks under a job should be scheduled to one hyperNode as much as possible.
func networkTopologyAwareScoreWithTaskNum(hyperNodeName string, job *api.JobInfo, hyperNodes map[string][]*api.NodeInfo) float64 {
	// Calculate tasks num score.
	taskNum := util.FindJobTaskNumOfHyperNode(hyperNodeName, job, hyperNodes)
	taskNumScore := ZeroScore
	if len(job.Tasks) > 0 {
		taskNumScore = TaskBaseScore * scoreHyperNodeWithTaskNum(taskNum, len(job.Tasks))
	}
	return taskNumScore
}

func scoreHyperNodeWithIndex(index int, minIndex int, maxIndex int) float64 {
	// Use tier index to calculate scores and map the original score to the range between 0 and 1.
	return float64(maxIndex-index) / float64(maxIndex-minIndex)
}

func scoreHyperNodeWithTaskNum(taskNum int, maxTaskNum int) float64 {
	// Calculate task distribution rate as score and make sure the original score to the range between 0 and 1.
	return float64(taskNum) / float64(maxTaskNum)
}
