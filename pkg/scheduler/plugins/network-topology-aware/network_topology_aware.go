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
	"net"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
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
	hyperNodeFn := func(job *api.JobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
		hyperNodeScores := make(map[string]float64)

		jobAllocatedHyperNode := job.PodGroup.GetAnnotations()[api.JobAllocatedHyperNode]
		if jobAllocatedHyperNode == "" {
			return hyperNodeScores, nil
		}
		// The job still has remaining tasks to be scheduled, calculate score based on the tier of LCAHyperNode between the hyperNode and jobAllocatedHyperNode.
		var maxScore float64 = -1
		scoreToHyperNodes := map[float64][]string{}
		for hyperNode := range hyperNodes {
			score := nta.networkTopologyAwareScore(hyperNode, jobAllocatedHyperNode, ssn.HyperNodes)
			score *= float64(nta.weight)
			hyperNodeScores[hyperNode] = score
			if score >= maxScore {
				maxScore = score
				scoreToHyperNodes[maxScore] = append(scoreToHyperNodes[maxScore], hyperNode)
			}
		}
		// Calculate score based on the number of tasks scheduled for the job when max score of hyperNode has more than one.
		if len(scoreToHyperNodes[maxScore]) > 1 {
			candidateHyperNodes := scoreToHyperNodes[maxScore]
			for _, hyperNode := range candidateHyperNodes {
				taskNumScore := nta.scoreWithTaskNum(hyperNode, job, ssn.RealNodesList)
				taskNumScore *= float64(nta.weight)
				hyperNodeScores[hyperNode] += taskNumScore
			}
		}

		klog.V(4).Infof("networkTopologyAware hyperNode score is: %v", hyperNodeScores)
		return hyperNodeScores, nil
	}

	nodeFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		nodeScores := make(map[string]float64)

		taskJob := ssn.Jobs[task.Job]
		hardMode, _ := taskJob.IsHardTopologyMode()
		if hardMode {
			return nta.scoreNodeByIP(nodes)
		}

		jobAllocatedHyperNode := task.JobAllocatedHyperNode
		if jobAllocatedHyperNode == "" {
			return nta.scoreNodeByIP(nodes)
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

	ssn.AddHyperNodeOrderFn(nta.Name(), hyperNodeFn)
	ssn.AddBatchNodeOrderFn(nta.Name(), nodeFn)
	ssn.AddTaskOrderFn(nta.Name(), nta.NPUTaskOrderFn)
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

func (nta *networkTopologyAwarePlugin) NPUTaskOrderFn(l interface{}, r interface{}) int {

	a, ok := l.(*api.TaskInfo)
	if !ok {
		klog.Errorf("Object is not a taskinfo")
		return 0
	}
	b, ok := r.(*api.TaskInfo)
	if !ok {
		klog.Errorf("Object is not a taskinfo")
		return 0
	}

	rankA := a.GetRank()
	randB := b.GetRank()
	// 如果没有指定RANK，这里主要是针对MPI任务，因为MPI任务通过ranktable方式指定的，无法优雅获取，可以根据pod名称的序号排序
	if rankA == "" && randB == "" && a.Pod != nil && b.Pod != nil {
		if strings.HasSuffix(a.Pod.Name, "-launcher") || strings.HasSuffix(b.Pod.Name, "-master-0") {
			return -1
		}

		if strings.HasSuffix(b.Pod.Name, "-launcher") || strings.HasSuffix(b.Pod.Name, "-master-0") {
			return 1
		}
		if len(a.Pod.Name) < len(b.Pod.Name) {
			return -1
		}

		if a.Pod.Name < b.Pod.Name {
			return -1
		}
		return 1
	}

	rankAId, Aerr := strconv.Atoi(rankA)
	rankBId, Berr := strconv.Atoi(randB)

	if Aerr == nil && Berr == nil {
		switch {
		case rankAId < rankBId:
			return -1
		case rankAId > rankBId:
			return 1
		default:
			return 0
		}
	}

	if Aerr == nil && Berr != nil {
		return 1
	}

	if Aerr != nil && Berr == nil {
		return -1
	}

	return 0

}

func GetInternalIP(node *corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}

func IPToUint32(ipStr string) (uint32, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return 0, fmt.Errorf("invalid IP address: %s", ipStr)
	}

	// 转换为 IPv4 格式
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("%s is not an IPv4 address", ipStr)
	}

	// 将 4 个字节转换为 uint32 (big-endian)
	return uint32(ipv4[0])<<24 | uint32(ipv4[1])<<16 |
		uint32(ipv4[2])<<8 | uint32(ipv4[3]), nil
}

func (nta *networkTopologyAwarePlugin) scoreNodeByIP(nodes []*api.NodeInfo) (map[string]float64, error) {
	// 找到最小的IP
	var minIP uint32
	minName := ""
	for _, node := range nodes {
		ipStr := GetInternalIP(node.Node)
		if ipStr == "" {
			klog.V(4).Infof("node %s has no internal IP", node.Name)
			continue
		}

		ip, err := IPToUint32(ipStr)
		if err != nil {
			klog.Error(err)
			continue
		}

		if ip < minIP {
			minIP = ip
			minName = node.Name
		}
	}

	score := make(map[string]float64)
	// just set minName with score and others with zero score
	score[minName] = float64(BaseScore * nta.weight)
	return score, nil
}
