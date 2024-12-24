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
	"math"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "networktopologyaware"
	BaseScore             = 100
	TaskBaseScore         = 10
	NetworkTopologyWeight = "weight"
)

// HyperNodeTree is the hypernode tree of all hypernodes in the cluster.
// currentJobLCAHyperNode is the hypernode of the job's LCAHyperNode.
var (
	HyperNodeTree []map[string][]string
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
		for hyperNode := range hyperNodes {
			score := networkTopologyAwareScore(hyperNode, job, HyperNodeTree)
			score *= float64(weight)
			hyperNodeScores[hyperNode] = score
		}

		klog.V(1).Infof("networkTopologyAware score is: %v", hyperNodeScores)
		return hyperNodeScores, nil
	}

	nodeFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		taskJob := ssn.Jobs[task.Job]
		nodeScores := make(map[string]float64)
		for _, node := range nodes {
			hyperNode := util.FindHyperNodeOfNode(node.Name, HyperNodeTree)
			score := networkTopologyAwareScore(hyperNode, taskJob, HyperNodeTree)
			score *= float64(weight)
			nodeScores[node.Name] = score
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

// Explanation:
// The currentJobLCAHyperNode is a property of job and that serves as the smallest root in the hypernode tree of job.
// A job has multiple tasks, each belonging to a hypernode. This LCAHyperNode is the topmost and lowest common ancestor among the hypernodes of all tasks within the job.

// Goals:
// - The tier index to which the LCAHyperNode of a job belongs should be as low as possible.
// - Tasks under a job should be scheduled to one hyperNode as much as possible.
func networkTopologyAwareScore(hyperNodeName string, job *api.JobInfo, hyperNodeTree []map[string][]string) float64 {
	jobHyperNode := job.PodGroup.GetAnnotations()[api.TopologyAllocateLCAHyperNode]
	// job fist first scheduler.
	if jobHyperNode == "" {
		return BaseScore
	}

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

	// Calculate tasks num score.
	taskNum := util.FindJobTaskNumOfHyperNode(hyperNodeName, job, hyperNodeTree)
	taskNumScore := 0.0
	if len(job.Tasks) > 0 {
		taskNumScore = TaskBaseScore * scoreHyperNodeWithTaskNum(taskNum, len(job.Tasks))
	}
	// aggregate scores
	return tierIndexScore + taskNumScore
}

func scoreHyperNodeWithIndex(index int, minIndex int, maxIndex int) float64 {
	// Use logarithmic operations to calculate scores and map the original values to the range between 0 and 1.
	// Adjust the formula to ensure that the scores meet the requirements (excluding 0 and 1).
	return (math.Log(float64(maxIndex)) - math.Log(float64(index))) / (math.Log(float64(maxIndex)) - math.Log(float64(minIndex)))
}

func scoreHyperNodeWithTaskNum(taskNum int, maxTaskNum int) float64 {
	// Calculate task distribution rate as score and make sure the score to the range between 0 and 1.
	return float64(taskNum) / float64(maxTaskNum)
}
