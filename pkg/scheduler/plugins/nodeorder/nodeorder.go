/*
Copyright 2019 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Refactored to use Kubernetes native scheduling plugins for improved compatibility

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

package nodeorder

import (
	"context"

	v1 "k8s.io/api/core/v1"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/imagelocality"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodescore"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "nodeorder"

	// NodeAffinityWeight is the key for providing Node Affinity Priority Weight in YAML
	NodeAffinityWeight = "nodeaffinity.weight"
	// PodAffinityWeight is the key for providing Pod Affinity Priority Weight in YAML
	PodAffinityWeight = "podaffinity.weight"
	// LeastRequestedWeight is the key for providing Least Requested Priority Weight in YAML
	LeastRequestedWeight = "leastrequested.weight"
	// BalancedResourceWeight is the key for providing Balanced Resource Priority Weight in YAML
	BalancedResourceWeight = "balancedresource.weight"
	// MostRequestedWeight is the key for providing Most Requested Priority Weight in YAML
	MostRequestedWeight = "mostrequested.weight"
	// TaintTolerationWeight is the key for providing Taint Toleration Priority Weight in YAML
	TaintTolerationWeight = "tainttoleration.weight"
	// ImageLocalityWeight is the key for providing Image Locality Priority Weight in YAML
	ImageLocalityWeight = "imagelocality.weight"
	// PodTopologySpreadWeight is the key for providing Pod Topology Spread Priority Weight in YAML
	PodTopologySpreadWeight = "podtopologyspread.weight"
)

type NodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments       framework.Arguments
	weight                priorityWeight
	Handle                k8sframework.Handle
	ScorePlugins          map[string]nodescore.BaseScorePlugin
	NodeOrderScorePlugins map[string]ScorePluginWithWeight
}

type ScorePluginWithWeight struct {
	plugin k8sframework.ScorePlugin
	weight int
}

// New function returns nodeorder plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(arguments)
	return &NodeOrderPlugin{pluginArguments: arguments, weight: weight}
}

func (pp *NodeOrderPlugin) Name() string {
	return PluginName
}

type priorityWeight struct {
	leastReqWeight          int
	mostReqWeight           int
	nodeAffinityWeight      int
	podAffinityWeight       int
	balancedResourceWeight  int
	taintTolerationWeight   int
	imageLocalityWeight     int
	podTopologySpreadWeight int
}

// calculateWeight from the provided arguments.
//
// Currently only supported priorities are nodeaffinity, podaffinity, leastrequested,
// mostrequested, balancedresouce, imagelocality, tainttoleration.
//
// User should specify priority weights in the config in this format:
//
//	actions: "reclaim, allocate, backfill, preempt"
//	tiers:
//	- plugins:
//	  - name: priority
//	  - name: gang
//	  - name: conformance
//	- plugins:
//	  - name: drf
//	  - name: predicates
//	  - name: proportion
//	  - name: nodeorder
//	    arguments:
//	      leastrequested.weight: 1
//	      mostrequested.weight: 0
//	      nodeaffinity.weight: 2
//	      podaffinity.weight: 2
//	      balancedresource.weight: 1
//	      tainttoleration.weight: 3
//	      imagelocality.weight: 1
//	      podtopologyspread.weight: 2
func calculateWeight(args framework.Arguments) priorityWeight {
	// Initial values for weights.
	// By default, for backward compatibility and for reasonable scores,
	// least requested priority is enabled and most requested priority is disabled.
	weight := priorityWeight{
		leastReqWeight:          1,
		mostReqWeight:           0,
		nodeAffinityWeight:      2,
		podAffinityWeight:       2,
		balancedResourceWeight:  1,
		taintTolerationWeight:   3,
		imageLocalityWeight:     1,
		podTopologySpreadWeight: 2, // be consistent with kubernetes default setting.
	}

	// Checks whether nodeaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.nodeAffinityWeight, NodeAffinityWeight)

	// Checks whether podaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.podAffinityWeight, PodAffinityWeight)

	// Checks whether leastrequested.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.leastReqWeight, LeastRequestedWeight)

	// Checks whether mostrequested.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.mostReqWeight, MostRequestedWeight)

	// Checks whether balancedresource.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.balancedResourceWeight, BalancedResourceWeight)

	// Checks whether tainttoleration.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.taintTolerationWeight, TaintTolerationWeight)

	// Checks whether imagelocality.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.imageLocalityWeight, ImageLocalityWeight)

	// Checks whether podtopologyspread.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.podTopologySpreadWeight, PodTopologySpreadWeight)

	return weight
}

func (pp *NodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	nodeMap := ssn.NodeMap
	// Initialize k8s scheduling plugins
	handle := k8s.NewFramework(nodeMap,
		k8s.WithClientSet(ssn.KubeClient()),
		k8s.WithInformerFactory(ssn.InformerFactory()),
	)
	pp.Handle = handle
	pp.InitPlugin()

	ssn.AddNodeOrderFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		nodeInfo := nodeMap[node.Name]
		state := k8sframework.NewCycleState()
		return pp.NodeOrderFn(task, node, nodeInfo, state)
	})

	ssn.AddBatchNodeOrderFn(pp.Name(), func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		state := k8sframework.NewCycleState()
		return pp.BatchNodeOrderFn(task, nodeInfo, state)
	})
}

func (pp *NodeOrderPlugin) InitPlugin() {
	scorePlugins := map[string]nodescore.BaseScorePlugin{}
	nodeOrderScorePlugins := map[string]ScorePluginWithWeight{}

	fts := feature.Features{
		EnableNodeInclusionPolicyInPodTopologySpread: utilFeature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      utilFeature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
	}

	if pp.weight.leastReqWeight != 0 {
		// 1. NodeResourcesLeastAllocated
		leastAllocatedArgs := &config.NodeResourcesFitArgs{
			ScoringStrategy: &config.ScoringStrategy{
				Type:      config.LeastAllocated,
				Resources: []config.ResourceSpec{{Name: "cpu", Weight: 50}, {Name: "memory", Weight: 50}},
			},
		}
		p, _ := noderesources.NewFit(context.TODO(), leastAllocatedArgs, pp.Handle, fts)
		leastAllocated := p.(*noderesources.Fit)
		nodeOrderScorePlugins["Least Allocated"] = ScorePluginWithWeight{leastAllocated, pp.weight.leastReqWeight}
	}

	if pp.weight.mostReqWeight != 0 {
		// 2. NodeResourcesMostAllocated
		mostAllocatedArgs := &config.NodeResourcesFitArgs{
			ScoringStrategy: &config.ScoringStrategy{
				Type:      config.MostAllocated,
				Resources: []config.ResourceSpec{{Name: "cpu", Weight: 1}, {Name: "memory", Weight: 1}},
			},
		}
		p, _ := noderesources.NewFit(context.TODO(), mostAllocatedArgs, pp.Handle, fts)
		mostAllocation := p.(*noderesources.Fit)
		nodeOrderScorePlugins["Most Allocated"] = ScorePluginWithWeight{mostAllocation, pp.weight.mostReqWeight}
	}

	if pp.weight.balancedResourceWeight != 0 {
		// 3. NodeResourcesBalancedAllocation
		blArgs := &config.NodeResourcesBalancedAllocationArgs{
			Resources: []config.ResourceSpec{
				{Name: string(v1.ResourceCPU), Weight: 1},
				{Name: string(v1.ResourceMemory), Weight: 1},
				{Name: "nvidia.com/gpu", Weight: 1},
			},
		}
		p, _ := noderesources.NewBalancedAllocation(context.TODO(), blArgs, pp.Handle, fts)
		balancedAllocation := p.(*noderesources.BalancedAllocation)
		nodeOrderScorePlugins["Balanced Resource Allocation"] = ScorePluginWithWeight{balancedAllocation, pp.weight.balancedResourceWeight}
	}

	if pp.weight.nodeAffinityWeight != 0 {
		// 4. NodeAffinity
		naArgs := &config.NodeAffinityArgs{
			AddedAffinity: &v1.NodeAffinity{},
		}
		p, _ := nodeaffinity.New(context.TODO(), naArgs, pp.Handle, fts)
		nodeAffinity := p.(*nodeaffinity.NodeAffinity)
		nodeOrderScorePlugins["Node Affinity"] = ScorePluginWithWeight{nodeAffinity, pp.weight.nodeAffinityWeight}
	}

	// 5. ImageLocality
	if pp.weight.imageLocalityWeight != 0 {
		p, _ := imagelocality.New(context.TODO(), nil, pp.Handle)
		imageLocality := p.(*imagelocality.ImageLocality)
		nodeOrderScorePlugins["Image Locality"] = ScorePluginWithWeight{imageLocality, pp.weight.imageLocalityWeight}
	}

	plArgs := &config.InterPodAffinityArgs{}
	p, _ := interpodaffinity.New(context.TODO(), plArgs, pp.Handle, fts)
	interPodAffinity := p.(*interpodaffinity.InterPodAffinity)
	scorePlugins[interpodaffinity.Name] = interPodAffinity

	p, _ = tainttoleration.New(context.TODO(), nil, pp.Handle, fts)
	taintToleration := p.(*tainttoleration.TaintToleration)
	scorePlugins[tainttoleration.Name] = taintToleration

	ptsArgs := &config.PodTopologySpreadArgs{
		DefaultingType: config.SystemDefaulting,
	}
	p, _ = podtopologyspread.New(context.TODO(), ptsArgs, pp.Handle, fts)
	podTopologySpread := p.(*podtopologyspread.PodTopologySpread)
	scorePlugins[podtopologyspread.Name] = podTopologySpread

	pp.NodeOrderScorePlugins = nodeOrderScorePlugins
	pp.ScorePlugins = scorePlugins
}

func (pp *NodeOrderPlugin) NodeOrderFn(task *api.TaskInfo, node *api.NodeInfo, k8sNodeInfo fwk.NodeInfo, state *k8sframework.CycleState) (float64, error) {
	var nodeScore = 0.0
	for name, p := range pp.NodeOrderScorePlugins {
		score, status := p.plugin.Score(context.TODO(), state, task.Pod, k8sNodeInfo)
		if !status.IsSuccess() {
			klog.Warningf("Node: %s, <%s> Priority Failed because of Error: %v", node.Name, name, status.AsError())
			return 0, status.AsError()
		}

		// If imageLocalityWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		nodeScore += float64(score) * float64(p.weight)
		klog.V(5).Infof("Node: %s, task<%s/%s> %s weight %d, score: %f", node.Name, task.Namespace, task.Name, name, pp.weight.imageLocalityWeight, float64(score)*float64(p.weight))
	}
	klog.V(4).Infof("Nodeorder Total Score for task<%s/%s> on node %s is: %f", task.Namespace, task.Name, node.Name, nodeScore)
	return nodeScore, nil
}

func (pp *NodeOrderPlugin) BatchNodeOrderFn(task *api.TaskInfo, nodeInfo []*api.NodeInfo, state *k8sframework.CycleState) (map[string]float64, error) {
	nodeInfos := make([]fwk.NodeInfo, 0, len(nodeInfo))
	nodes := make([]*v1.Node, 0, len(nodeInfo))
	for _, node := range nodeInfo {
		newNodeInfo := &k8sframework.NodeInfo{}
		newNodeInfo.SetNode(node.Node)
		nodeInfos = append(nodeInfos, newNodeInfo)
		nodes = append(nodes, node.Node)
	}
	nodeScores := make(map[string]float64, len(nodes))

	podAffinityScores, podErr := interPodAffinityScore(pp.ScorePlugins[interpodaffinity.Name].(*interpodaffinity.InterPodAffinity), state, task.Pod, nodeInfos, pp.weight.podAffinityWeight)
	if podErr != nil {
		return nil, podErr
	}

	nodeTolerationScores, err := taintTolerationScore(pp.ScorePlugins[tainttoleration.Name].(*tainttoleration.TaintToleration), state, task.Pod, nodeInfos, pp.weight.taintTolerationWeight)
	if err != nil {
		return nil, err
	}

	podTopologySpreadScores, err := podTopologySpreadScore(pp.ScorePlugins[podtopologyspread.Name].(*podtopologyspread.PodTopologySpread), state, task.Pod, nodeInfos, pp.weight.podTopologySpreadWeight)
	if err != nil {
		return nil, err
	}

	for _, node := range nodes {
		nodeScores[node.Name] = podAffinityScores[node.Name] + nodeTolerationScores[node.Name] + podTopologySpreadScores[node.Name]
	}

	klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, nodeScores)
	return nodeScores, nil
}

func interPodAffinityScore(
	interPodAffinity *interpodaffinity.InterPodAffinity,
	state fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
	podAffinityWeight int,
) (map[string]float64, error) {
	return nodescore.CalculatePluginScore(interPodAffinity.Name(), interPodAffinity, interPodAffinity,
		state, pod, nodeInfos, podAffinityWeight)
}

func taintTolerationScore(
	taintToleration *tainttoleration.TaintToleration,
	cycleState fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
	taintTolerationWeight int,
) (map[string]float64, error) {
	return nodescore.CalculatePluginScore(taintToleration.Name(), taintToleration, taintToleration,
		cycleState, pod, nodeInfos, taintTolerationWeight)
}

func podTopologySpreadScore(
	podTopologySpread *podtopologyspread.PodTopologySpread,
	cycleState fwk.CycleState,
	pod *v1.Pod,
	nodeInfos []fwk.NodeInfo,
	podTopologySpreadWeight int,
) (map[string]float64, error) {
	return nodescore.CalculatePluginScore(podTopologySpread.Name(), podTopologySpread, podTopologySpread,
		cycleState, pod, nodeInfos, podTopologySpreadWeight)
}

func (pp *NodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}
