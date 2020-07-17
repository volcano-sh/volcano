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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
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
)

type nodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New function returns prioritize plugin object.
func New(aruguments framework.Arguments) framework.Plugin {
	return &nodeOrderPlugin{pluginArguments: aruguments}
}

func (pp *nodeOrderPlugin) Name() string {
	return PluginName
}

type priorityWeight struct {
	leastReqWeight          int
	mostReqWeight           int
	nodeAffinityWeight      int
	podAffinityWeight       int
	balancedRescourceWeight int
}

// calculateWeight from the provided arguments.
//
// Currently only supported priorities are nodeaffinity, podaffinity, leastrequested,
// mostrequested, balancedresouce.
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
//        leastrequested.weight: 2
//        mostrequested.weight: 0
//        nodeaffinity.weight: 2
//        podaffinity.weight: 2
//        balancedresource.weight: 2
func calculateWeight(args framework.Arguments) priorityWeight {

	// Initial values for weights.
	// By default, for backward compatibility and for reasonable scores,
	// least requested priority is enabled and most requested priority is disabled.
	weight := priorityWeight{
		leastReqWeight:          1,
		mostReqWeight:           0,
		nodeAffinityWeight:      1,
		podAffinityWeight:       1,
		balancedRescourceWeight: 1,
	}

	// Checks whether mostrequested.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.mostReqWeight, MostRequestedWeight)

	// Checks whether nodeaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.nodeAffinityWeight, NodeAffinityWeight)

	// Checks whether podaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.podAffinityWeight, PodAffinityWeight)

	// Checks whether leastrequested.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.leastReqWeight, LeastRequestedWeight)

	// Checks whether balancedresource.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.balancedRescourceWeight, BalancedResourceWeight)

	return weight
}

func (pp *nodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	weight := calculateWeight(pp.pluginArguments)
	pl := util.NewPodLister(ssn)
	pods, _ := pl.List(labels.NewSelector())
	nodeMap, nodeSlice := util.GenerateNodeMapAndSlice(ssn.Nodes)

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
	handle := k8s.NewFrameworkHandle(pods, nodeSlice)
	// 1. NodeResourcesLeastAllocated
	p, _ := noderesources.NewLeastAllocated(nil, handle)
	leastAllocated := p.(*noderesources.LeastAllocated)

	// 2. NodeResourcesBalancedAllocation
	p, _ = noderesources.NewBalancedAllocation(nil, handle)
	balancedAllocation := p.(*noderesources.BalancedAllocation)

	// 3. NodeAffinity
	p, _ = nodeaffinity.New(nil, handle)
	affinity := p.(*nodeaffinity.NodeAffinity)

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		var nodeScore = 0.0

		//TODO: Add ImageLocalityPriority Function once priorityMetadata is published
		//Issue: #74132 in kubernetes ( https://github.com/kubernetes/kubernetes/issues/74132 )

		// NodeResourcesLeastAllocated
		score, status := leastAllocated.Score(context.TODO(), nil, task.Pod, node.Name)
		if !status.IsSuccess() {
			klog.Warningf("Least Allocated Priority Failed because of Error: %v", status.AsError())
			return 0, status.AsError()
		}

		// If leastReqWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		nodeScore += float64(score) * float64(weight.leastReqWeight)

		// NodeResourcesBalancedAllocation
		score, status = balancedAllocation.Score(context.TODO(), nil, task.Pod, node.Name)
		if !status.IsSuccess() {
			klog.Warningf("Balanced Resource Allocation Priority Failed because of Error: %v", status.AsError())
			return 0, status.AsError()
		}
		// If balancedRescourceWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		nodeScore += float64(score) * float64(weight.balancedRescourceWeight)

		score, status = affinity.Score(context.TODO(), nil, task.Pod, node.Name)
		if !status.IsSuccess() {
			klog.Warningf("Calculate Node Affinity Priority Failed because of Error: %v", status.AsError())
			return 0, status.AsError()
		}
		// TODO: should we normalize the score
		// If nodeAffinityWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		nodeScore += float64(score) * float64(weight.nodeAffinityWeight)

		klog.V(4).Infof("Total Score for task %s/%s on node %s is: %f", task.Namespace, task.Name, node.Name, nodeScore)
		return nodeScore, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)

	p, _ = interpodaffinity.New(nil, handle)
	interPodAffinity := p.(*interpodaffinity.InterPodAffinity)

	batchNodeOrderFn := func(task *api.TaskInfo, nodeInfo []*api.NodeInfo) (map[string]float64, error) {
		// InterPodAffinity
		state := k8sframework.NewCycleState()
		nodes := make([]*v1.Node, 0, len(nodeInfo))
		for _, node := range nodeInfo {
			nodes = append(nodes, node.Node)
		}
		preScoreStatus := interPodAffinity.PreScore(context.TODO(), state, task.Pod, nodes)
		if !preScoreStatus.IsSuccess() {
			return nil, preScoreStatus.AsError()
		}

		nodescoreList := make(k8sframework.NodeScoreList, len(nodes))
		errCh := make(chan error, 1)
		workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), func(index int) {
			nodeName := nodes[index].Name
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, status := interPodAffinity.Score(ctx, state, task.Pod, nodeName)
			if !status.IsSuccess() {
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

		interPodAffinity.NormalizeScore(context.TODO(), state, task.Pod, nodescoreList)

		nodeScores := make(map[string]float64, len(nodes))
		for i, nodeScore := range nodescoreList {
			// return error if score plugin returns invalid score.
			if nodeScore.Score > k8sframework.MaxNodeScore || nodeScore.Score < k8sframework.MinNodeScore {
				return nil, fmt.Errorf("inter pod affinity returns an invalid score %v for node %s", nodeScore.Score, nodeScore.Name)
			}
			nodeScore.Score *= int64(weight.podAffinityWeight)
			nodescoreList[i] = nodeScore
			nodeScores[nodeScore.Name] = float64(nodeScore.Score)
		}

		klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, nodeScores)
		return nodeScores, nil
	}
	ssn.AddBatchNodeOrderFn(pp.Name(), batchNodeOrderFn)
}

func (pp *nodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}
