/*
Copyright 2019 The Kubernetes Authors.

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
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

type nodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns nodeorder plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &nodeOrderPlugin{pluginArguments: arguments}
}

func (pp *nodeOrderPlugin) Name() string {
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

func (pp *nodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	weight := calculateWeight(pp.pluginArguments)
	nodeMap := ssn.NodeMap

	fts := feature.Features{
		EnableVolumeCapacityPriority:                 utilFeature.DefaultFeatureGate.Enabled(features.VolumeCapacityPriority),
		EnableNodeInclusionPolicyInPodTopologySpread: utilFeature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      utilFeature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
	}

	// Initialize k8s scheduling plugins
	handle := k8s.NewFrameworkHandle(nodeMap, ssn.KubeClient(), ssn.InformerFactory())

	// 1. NodeResourcesLeastAllocated
	leastAllocatedArgs := &config.NodeResourcesFitArgs{
		ScoringStrategy: &config.ScoringStrategy{
			Type:      config.LeastAllocated,
			Resources: []config.ResourceSpec{{Name: "cpu", Weight: 50}, {Name: "memory", Weight: 50}},
		},
	}
	p, _ := noderesources.NewFit(context.TODO(), leastAllocatedArgs, handle, fts)
	leastAllocated := p.(*noderesources.Fit)

	// 2. NodeResourcesMostAllocated
	mostAllocatedArgs := &config.NodeResourcesFitArgs{
		ScoringStrategy: &config.ScoringStrategy{
			Type:      config.MostAllocated,
			Resources: []config.ResourceSpec{{Name: "cpu", Weight: 1}, {Name: "memory", Weight: 1}},
		},
	}
	p, _ = noderesources.NewFit(context.TODO(), mostAllocatedArgs, handle, fts)
	mostAllocation := p.(*noderesources.Fit)

	// 3. NodeResourcesBalancedAllocation
	blArgs := &config.NodeResourcesBalancedAllocationArgs{
		Resources: []config.ResourceSpec{
			{Name: string(v1.ResourceCPU), Weight: 1},
			{Name: string(v1.ResourceMemory), Weight: 1},
			{Name: "nvidia.com/gpu", Weight: 1},
		},
	}
	p, _ = noderesources.NewBalancedAllocation(context.TODO(), blArgs, handle, fts)
	balancedAllocation := p.(*noderesources.BalancedAllocation)

	// 4. NodeAffinity
	naArgs := &config.NodeAffinityArgs{
		AddedAffinity: &v1.NodeAffinity{},
	}
	p, _ = nodeaffinity.New(context.TODO(), naArgs, handle, fts)
	nodeAffinity := p.(*nodeaffinity.NodeAffinity)

	// 5. ImageLocality
	p, _ = imagelocality.New(context.TODO(), nil, handle)
	imageLocality := p.(*imagelocality.ImageLocality)

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		var nodeScore = 0.0

		state := k8sframework.NewCycleState()
		if weight.imageLocalityWeight != 0 {
			score, status := imageLocality.Score(context.TODO(), state, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Image Locality Priority Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// If imageLocalityWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.imageLocalityWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Image Locality weight %d, score: %f", node.Name, task.Namespace, task.Name, weight.imageLocalityWeight, float64(score)*float64(weight.imageLocalityWeight))
		}

		// NodeResourcesLeastAllocated
		if weight.leastReqWeight != 0 {
			score, status := leastAllocated.Score(context.TODO(), state, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Least Allocated Priority Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// If leastReqWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.leastReqWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Least Request weight %d, score: %f", node.Name, task.Namespace, task.Name, weight.leastReqWeight, float64(score)*float64(weight.leastReqWeight))
		}

		// NodeResourcesMostAllocated
		if weight.mostReqWeight != 0 {
			score, status := mostAllocation.Score(context.TODO(), state, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Most Allocated Priority Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// If mostRequestedWeight is provided, host.Score is multiplied with weight, it's 0 by default
			nodeScore += float64(score) * float64(weight.mostReqWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Most Request weight %d, score: %f", node.Name, task.Namespace, task.Name, weight.mostReqWeight, float64(score)*float64(weight.mostReqWeight))
		}

		// NodeResourcesBalancedAllocation
		if weight.balancedResourceWeight != 0 {
			score, status := balancedAllocation.Score(context.TODO(), state, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Balanced Resource Allocation Priority Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// If balancedResourceWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.balancedResourceWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Balanced Request weight %d, score: %f", node.Name, task.Namespace, task.Name, weight.balancedResourceWeight, float64(score)*float64(weight.balancedResourceWeight))
		}

		// NodeAffinity
		if weight.nodeAffinityWeight != 0 {
			score, status := nodeAffinity.Score(context.TODO(), state, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Node: %s, Calculate Node Affinity Priority Failed because of Error: %v", node.Name, status.AsError())
				return 0, status.AsError()
			}

			// TODO: should we normalize the score
			// If nodeAffinityWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.nodeAffinityWeight)
			klog.V(5).Infof("Node: %s, task<%s/%s> Node Affinity weight %d, score: %f", node.Name, task.Namespace, task.Name, weight.nodeAffinityWeight, float64(score)*float64(weight.nodeAffinityWeight))
		}

		klog.V(4).Infof("Nodeorder Total Score for task<%s/%s> on node %s is: %f", task.Namespace, task.Name, node.Name, nodeScore)
		return nodeScore, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)

	plArgs := &config.InterPodAffinityArgs{}
	p, _ = interpodaffinity.New(context.TODO(), plArgs, handle, fts)
	interPodAffinity := p.(*interpodaffinity.InterPodAffinity)

	p, _ = tainttoleration.New(context.TODO(), nil, handle, fts)
	taintToleration := p.(*tainttoleration.TaintToleration)

	ptsArgs := &config.PodTopologySpreadArgs{
		DefaultingType: config.SystemDefaulting,
	}
	p, _ = podtopologyspread.New(context.TODO(), ptsArgs, handle, fts)
	podTopologySpread := p.(*podtopologyspread.PodTopologySpread)

	batchNodeOrderFn := func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		// InterPodAffinity
		state := k8sframework.NewCycleState()
		nodeInfos := make([]*k8sframework.NodeInfo, 0, len(nodeInfo))
		nodes := make([]*v1.Node, 0, len(nodeInfo))
		for _, node := range nodeInfo {
			newNodeInfo := &k8sframework.NodeInfo{}
			newNodeInfo.SetNode(node.Node)
			nodeInfos = append(nodeInfos, newNodeInfo)
			nodes = append(nodes, node.Node)
		}
		nodeScores := make(map[string]float64, len(nodes))

		podAffinityScores, podErr := interPodAffinityScore(interPodAffinity, state, task.Pod, nodeInfos, weight.podAffinityWeight)
		if podErr != nil {
			return nil, podErr
		}

		nodeTolerationScores, err := taintTolerationScore(taintToleration, state, task.Pod, nodeInfos, weight.taintTolerationWeight)
		if err != nil {
			return nil, err
		}

		podTopologySpreadScores, err := podTopologySpreadScore(podTopologySpread, state, task.Pod, nodeInfos, weight.podTopologySpreadWeight)
		if err != nil {
			return nil, err
		}

		for _, node := range nodes {
			nodeScores[node.Name] = podAffinityScores[node.Name] + nodeTolerationScores[node.Name] + podTopologySpreadScores[node.Name]
		}

		klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, nodeScores)
		return nodeScores, nil
	}
	ssn.AddBatchNodeOrderFn(pp.Name(), batchNodeOrderFn)
}

func interPodAffinityScore(
	interPodAffinity *interpodaffinity.InterPodAffinity,
	state *k8sframework.CycleState,
	pod *v1.Pod,
	nodeInfos []*k8sframework.NodeInfo,
	podAffinityWeight int,
) (map[string]float64, error) {
	preScoreStatus := interPodAffinity.PreScore(context.TODO(), state, pod, nodeInfos)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodeScoreList := make(k8sframework.NodeScoreList, len(nodeInfos))
	// the default parallelization worker number is 16.
	// the whole scoring will fail if one of the processes failed.
	// so just create a parallelizeContext to control the whole ParallelizeUntil process.
	// if the parallelizeCancel is invoked, the whole "ParallelizeUntil" goes to the end.
	// this could avoid extra computation, especially in huge cluster.
	// and the ParallelizeUntil guarantees only "workerNum" goroutines will be working simultaneously.
	// so it's enough to allocate workerNum size for errCh.
	// note that, in such case, size of errCh should be no less than parallelization number
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	defer parallelizeCancel()

	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		nodeName := nodeInfos[index].Node().Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := interPodAffinity.Score(ctx, state, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate inter pod affinity priority failed %v", status.Message())
			return
		}
		nodeScoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	interPodAffinity.NormalizeScore(context.TODO(), state, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodeInfos))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("inter pod affinity returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(podAffinityWeight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("inter pod affinity Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}

func taintTolerationScore(
	taintToleration *tainttoleration.TaintToleration,
	cycleState *k8sframework.CycleState,
	pod *v1.Pod,
	nodeInfos []*k8sframework.NodeInfo,
	taintTolerationWeight int,
) (map[string]float64, error) {
	preScoreStatus := taintToleration.PreScore(context.TODO(), cycleState, pod, nodeInfos)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodeScoreList := make(k8sframework.NodeScoreList, len(nodeInfos))
	// size of errCh should be no less than parallelization number, see interPodAffinityScore.
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	defer parallelizeCancel()

	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		nodeName := nodeInfos[index].Node().Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := taintToleration.Score(ctx, cycleState, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate taint toleration priority failed %v", status.Message())
			return
		}
		nodeScoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	taintToleration.NormalizeScore(context.TODO(), cycleState, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodeInfos))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("taint toleration returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(taintTolerationWeight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("taint toleration Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}

func podTopologySpreadScore(
	podTopologySpread *podtopologyspread.PodTopologySpread,
	cycleState *k8sframework.CycleState,
	pod *v1.Pod,
	nodeInfos []*k8sframework.NodeInfo,
	podTopologySpreadWeight int,
) (map[string]float64, error) {
	preScoreStatus := podTopologySpread.PreScore(context.TODO(), cycleState, pod, nodeInfos)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodeScoreList := make(k8sframework.NodeScoreList, len(nodeInfos))
	// size of errCh should be no less than parallelization number, see interPodAffinityScore.
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodeInfos), func(index int) {
		nodeName := nodeInfos[index].Node().Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := podTopologySpread.Score(ctx, cycleState, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate pod topology spread priority failed %v", status.Message())
			return
		}
		nodeScoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	podTopologySpread.NormalizeScore(context.TODO(), cycleState, pod, nodeScoreList)

	nodeScores := make(map[string]float64, len(nodeInfos))
	for i, nodeScore := range nodeScoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("pod topology spread returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(podTopologySpreadWeight)
		nodeScoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("pod topology spread Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}

func (pp *nodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}
