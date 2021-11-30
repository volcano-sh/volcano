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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/imagelocality"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
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
	// TaintTolerationWeight is the key for providing Most Requested Priority Weight in YAML
	TaintTolerationWeight = "tainttoleration.weight"
	// ImageLocalityWeight is the key for providing Image Locality Priority Weight in YAML
	ImageLocalityWeight = "imagelocality.weight"
)

type nodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns prioritize plugin object.
func New(arguments framework.Arguments) framework.Plugin {
	return &nodeOrderPlugin{pluginArguments: arguments}
}

func (pp *nodeOrderPlugin) Name() string {
	return PluginName
}

type priorityWeight struct {
	leastReqWeight         int
	mostReqWeight          int
	nodeAffinityWeight     int
	podAffinityWeight      int
	balancedResourceWeight int
	taintTolerationWeight  int
	imageLocalityWeight    int
}

// calculateWeight from the provided arguments.
//
// Currently only supported priorities are nodeaffinity, podaffinity, leastrequested,
// mostrequested, balancedresouce, imagelocality.
//
// User should specify priority weights in the config in this format:
//
//  actions: "reclaim, allocate, backfill, preempt"
//  tiers:
//  - plugins:
//    - name: priority
//    - name: gang
//    - name: conformance
//  - plugins:
//    - name: drf
//    - name: predicates
//    - name: proportion
//    - name: nodeorder
//      arguments:
//        leastrequested.weight: 1
//        mostrequested.weight: 0
//        nodeaffinity.weight: 1
//        podaffinity.weight: 1
//        balancedresource.weight: 1
//        tainttoleration.weight: 1
//        imagelocality.weight: 1
func calculateWeight(args framework.Arguments) priorityWeight {
	// Initial values for weights.
	// By default, for backward compatibility and for reasonable scores,
	// least requested priority is enabled and most requested priority is disabled.
	weight := priorityWeight{
		leastReqWeight:         1,
		mostReqWeight:          0,
		nodeAffinityWeight:     1,
		podAffinityWeight:      1,
		balancedResourceWeight: 1,
		taintTolerationWeight:  1,
		imageLocalityWeight:    1,
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

	return weight
}

func (pp *nodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	weight := calculateWeight(pp.pluginArguments)
	pl := util.NewPodListerFromNode(ssn)
	nodeMap := util.GenerateNodeMapAndSlice(ssn.Nodes)

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			pod := pl.UpdateTask(event.Task, event.Task.NodeName)

			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Warningf("node order, update pod %s/%s allocate to NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
			} else {
				node.AddPod(pod)
				klog.V(4).Infof("node order, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, nodeName)
			}
		},
		DeallocateFunc: func(event *framework.Event) {
			pod := pl.UpdateTask(event.Task, "")

			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Warningf("node order, update pod %s/%s allocate from NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
			} else {
				err := node.RemovePod(pod)
				if err != nil {
					klog.Errorf("Failed to update pod %s/%s and deallocate from node [%s]: %s", pod.Namespace, pod.Name, nodeName, err.Error())
				} else {
					klog.V(4).Infof("node order, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
				}
			}
		},
	})

	// Initialize k8s scheduling plugins
	handle := k8s.NewFrameworkHandle(nodeMap, ssn.KubeClient(), ssn.InformerFactory())
	// 1. NodeResourcesLeastAllocated
	laArgs := &config.NodeResourcesLeastAllocatedArgs{
		Resources: []config.ResourceSpec{
			{
				Name:   "cpu",
				Weight: 50,
			},
			{
				Name:   "memory",
				Weight: 50,
			},
		},
	}
	p, _ := noderesources.NewLeastAllocated(laArgs, handle)
	leastAllocated := p.(*noderesources.LeastAllocated)

	// 2. NodeResourcesMostAllocated
	defaultResourceMostAllocatedSet := []config.ResourceSpec{
		{Name: string(v1.ResourceCPU), Weight: 1},
		{Name: string(v1.ResourceMemory), Weight: 1},
	}
	args := config.NodeResourcesMostAllocatedArgs{Resources: defaultResourceMostAllocatedSet}
	p, _ = noderesources.NewMostAllocated(&args, handle)
	mostAllocated := p.(*noderesources.MostAllocated)

	// 3. NodeResourcesBalancedAllocation
	p, _ = noderesources.NewBalancedAllocation(nil, handle)
	balancedAllocation := p.(*noderesources.BalancedAllocation)

	// 4. NodeAffinity
	p, _ = nodeaffinity.New(nil, handle)
	nodeAffinity := p.(*nodeaffinity.NodeAffinity)

	// 5. ImageLocality
	p, _ = imagelocality.New(nil, handle)
	imageLocality := p.(*imagelocality.ImageLocality)

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		var nodeScore = 0.0

		if weight.imageLocalityWeight != 0 {
			score, status := imageLocality.Score(context.TODO(), nil, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Image Locality Priority Failed because of Error: %v", status.AsError())
				return 0, status.AsError()
			}

			// If imageLocalityWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.imageLocalityWeight)
		}

		// NodeResourcesLeastAllocated
		if weight.leastReqWeight != 0 {
			score, status := leastAllocated.Score(context.TODO(), nil, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Least Allocated Priority Failed because of Error: %v", status.AsError())
				return 0, status.AsError()
			}

			// If leastReqWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.leastReqWeight)
		}

		// NodeResourcesMostAllocated
		if weight.mostReqWeight != 0 {
			score, status := mostAllocated.Score(context.TODO(), nil, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Most Allocated Priority Failed because of Error: %v", status.AsError())
				return 0, status.AsError()
			}

			// If mostRequestedWeight is provided, host.Score is multiplied with weight, it's 0 by default
			nodeScore += float64(score) * float64(weight.mostReqWeight)
		}

		// NodeResourcesBalancedAllocation
		if weight.balancedResourceWeight != 0 {
			score, status := balancedAllocation.Score(context.TODO(), nil, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Balanced Resource Allocation Priority Failed because of Error: %v", status.AsError())
				return 0, status.AsError()
			}

			// If balancedResourceWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.balancedResourceWeight)
		}

		// NodeAffinity
		if weight.nodeAffinityWeight != 0 {
			score, status := nodeAffinity.Score(context.TODO(), nil, task.Pod, node.Name)
			if !status.IsSuccess() {
				klog.Warningf("Calculate Node Affinity Priority Failed because of Error: %v", status.AsError())
				return 0, status.AsError()
			}

			// TODO: should we normalize the score
			// If nodeAffinityWeight is provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
			nodeScore += float64(score) * float64(weight.nodeAffinityWeight)
		}

		klog.V(4).Infof("Total Score for task %s/%s on node %s is: %f", task.Namespace, task.Name, node.Name, nodeScore)
		return nodeScore, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)

	plArgs := &config.InterPodAffinityArgs{}
	p, _ = interpodaffinity.New(plArgs, handle)
	interPodAffinity := p.(*interpodaffinity.InterPodAffinity)

	p, _ = tainttoleration.New(nil, handle)
	taintToleration := p.(*tainttoleration.TaintToleration)

	batchNodeOrderFn := func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		// InterPodAffinity
		state := k8sframework.NewCycleState()
		nodes := make([]*v1.Node, 0, len(nodeInfo))
		for _, node := range nodeInfo {
			nodes = append(nodes, node.Node)
		}
		nodeScores := make(map[string]float64, len(nodes))

		podAffinityScores, podErr := interPodAffinityScore(interPodAffinity, state, task.Pod, nodes, weight.podAffinityWeight)
		if podErr != nil {
			return nil, podErr
		}

		nodeTolerationScores, err := taintTolerationScore(taintToleration, state, task.Pod, nodes, weight.taintTolerationWeight)
		if err != nil {
			return nil, err
		}

		for _, node := range nodes {
			nodeScores[node.Name] = podAffinityScores[node.Name] + nodeTolerationScores[node.Name]
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
	nodes []*v1.Node,
	podAffinityWeight int,
) (map[string]float64, error) {
	preScoreStatus := interPodAffinity.PreScore(context.TODO(), state, pod, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodescoreList := make(k8sframework.NodeScoreList, len(nodes))
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
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodes), func(index int) {
		nodeName := nodes[index].Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := interPodAffinity.Score(ctx, state, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate inter pod affinity priority failed %v", status.Message())
			return
		}
		nodescoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	interPodAffinity.NormalizeScore(context.TODO(), state, pod, nodescoreList)

	nodeScores := make(map[string]float64, len(nodes))
	for i, nodeScore := range nodescoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("inter pod affinity returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(podAffinityWeight)
		nodescoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("inter pod affinity Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}

func taintTolerationScore(
	taintToleration *tainttoleration.TaintToleration,
	cycleState *k8sframework.CycleState,
	pod *v1.Pod,
	nodes []*v1.Node,
	taintTolerationWeight int,
) (map[string]float64, error) {
	preScoreStatus := taintToleration.PreScore(context.TODO(), cycleState, pod, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	nodescoreList := make(k8sframework.NodeScoreList, len(nodes))
	// size of errCh should be no less than parallelization number, see interPodAffinityScore.
	workerNum := 16
	errCh := make(chan error, workerNum)
	parallelizeContext, parallelizeCancel := context.WithCancel(context.TODO())
	workqueue.ParallelizeUntil(parallelizeContext, workerNum, len(nodes), func(index int) {
		nodeName := nodes[index].Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s, status := taintToleration.Score(ctx, cycleState, pod, nodeName)
		if !status.IsSuccess() {
			parallelizeCancel()
			errCh <- fmt.Errorf("calculate taint toleration priority failed %v", status.Message())
			return
		}
		nodescoreList[index] = k8sframework.NodeScore{
			Name:  nodeName,
			Score: s,
		}
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	taintToleration.NormalizeScore(context.TODO(), cycleState, pod, nodescoreList)

	nodeScores := make(map[string]float64, len(nodes))
	for i, nodeScore := range nodescoreList {
		// return error if score plugin returns invalid score.
		if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
			return nil, fmt.Errorf("taint toleration returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
		}
		nodeScore.Score *= int64(taintTolerationWeight)
		nodescoreList[i] = nodeScore
		nodeScores[nodeScore.Name] = float64(nodeScore.Score)
	}

	klog.V(4).Infof("taint toleration Score for task %s/%s is: %v", pod.Namespace, pod.Name, nodeScores)
	return nodeScores, nil
}

func (pp *nodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}
