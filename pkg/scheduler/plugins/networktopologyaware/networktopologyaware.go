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
	PluginName = "networktopologyaware"
	BaseScore  = 100
)

// HyperNodeTree is the hypernode tree of all hypernodes in the cluster.
// currentJobLCAHyperNode is the hypernode of the job's LCAHyperNode.
var (
	HyperNodeTree []map[string][]string
)

type networkTopologyAwarePlugin struct{}

// New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	return &networkTopologyAwarePlugin{}
}

func (nta *networkTopologyAwarePlugin) Name() string {
	return PluginName
}

func (nta *networkTopologyAwarePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter networkTopologyAwarePlugin plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving networkTopologyAware plugin ...")
	}()
	ntaFn := func(job *api.JobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
		jobHyperNode := job.PodGroup.Annotations[api.TopologyAllocateLCAHyperNode]

		hyperNodeScores := make(map[string]float64)
		for hyperNode := range hyperNodes {
			score := networkTopologyAwareScore(hyperNode, jobHyperNode, HyperNodeTree)
			hyperNodeScores[hyperNode] = score
		}

		klog.V(1).Infof("networkTopologyAware score is: %v", hyperNodeScores)
		return hyperNodeScores, nil
	}
	ssn.AddHyperNodeOrederFn(nta.Name(), ntaFn)
}

func (bp *networkTopologyAwarePlugin) OnSessionClose(ssn *framework.Session) {
}

// networkTopologyAwareScore use the best fit polices during scheduling.

// Explanation:
// The currentJobLCAHyperNode is a property of job and that serves as the smallest root in the hypernode tree of job.
// A job has multiple tasks, each belonging to a hypernode. This LCAHyperNode is the topmost and lowest common ancestor among the hypernodes of all tasks within the job.

// Goals:
// - The tier index to which the LCAHyperNode of a job belongs should be as low as possible.
func networkTopologyAwareScore(hyperNode string, jobHyperNode string, hyperNodeTree []map[string][]string) float64 {
	// job fist first scheduler.
	if jobHyperNode == "" {
		return BaseScore
	}

	if jobHyperNode == hyperNode {
		return BaseScore
	}

	_, index := util.FindLCAHyperNode(hyperNode, jobHyperNode, hyperNodeTree)
	if index <= 0 {
		klog.V(4).Infof("find LCAhyperNode failed wtih %s in hyperNodeTree", hyperNode)
		return 0.0
	}

	// Calculate scores.
	return BaseScore * scoreHyperNodeWithIndex(index, 1, len(hyperNodeTree))
}

func scoreHyperNodeWithIndex(index int, minIndex int, maxIndex int) float64 {
	// Use logarithmic operations to calculate scores and map the original values to the range between 0 and 1.
	// Adjust the formula to ensure that the scores meet the requirements (excluding 0 and 1).
	return (math.Log(float64(maxIndex)) - math.Log(float64(index))) / (math.Log(float64(maxIndex)) - math.Log(float64(minIndex)))
}
