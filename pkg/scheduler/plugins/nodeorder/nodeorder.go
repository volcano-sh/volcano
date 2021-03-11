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

	"k8s.io/apimachinery/pkg/api/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"k8s.io/kubernetes/pkg/scheduler/algorithm/priorities"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
	schedulernodeinfo "k8s.io/kubernetes/pkg/scheduler/nodeinfo"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
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
	// TaintTolerationWeight is the key for providing Most Requested Priority Weight in YAML
	TaintTolerationWeight = "tainttoleration.weight"
)

type nodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

//New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	return &nodeOrderPlugin{pluginArguments: aruguments}
}

func (pp *nodeOrderPlugin) Name() string {
	return PluginName
}

type priorityWeight struct {
	leastReqWeight          int
	nodeAffinityWeight      int
	podAffinityWeight       int
	balancedRescourceWeight int
	taintTolerationWeight   int
}

func calculateWeight(args framework.Arguments) priorityWeight {
	/*
	   User Should give priorityWeight in this format(nodeaffinity.weight, podaffinity.weight, leastrequested.weight, balancedresource.weight).
	   Currently supported only for nodeaffinity, podaffinity, leastrequested, balancedresouce priorities.

	   actions: "reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: priority
	     - name: gang
	     - name: conformance
	   - plugins:
	     - name: drf
	     - name: predicates
	     - name: proportion
	     - name: nodeorder
	       arguments:
	         nodeaffinity.weight: 2
	         podaffinity.weight: 2
	         leastrequested.weight: 2
	         balancedresource.weight: 2
	         tainttoleration.weight: 2
	*/

	// Values are initialized to 1.
	weight := priorityWeight{
		leastReqWeight:          1,
		nodeAffinityWeight:      1,
		podAffinityWeight:       1,
		balancedRescourceWeight: 1,
		taintTolerationWeight:   1,
	}

	// Checks whether nodeaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.nodeAffinityWeight, NodeAffinityWeight)

	// Checks whether podaffinity.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.podAffinityWeight, PodAffinityWeight)

	// Checks whether leastrequested.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.leastReqWeight, LeastRequestedWeight)

	// Checks whether balancedresource.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.balancedRescourceWeight, BalancedResourceWeight)

	// Checks whether tainttoleration.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.taintTolerationWeight, TaintTolerationWeight)

	return weight
}

func (pp *nodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	var nodeMap map[string]*schedulernodeinfo.NodeInfo
	var nodeSlice []*v1.Node

	weight := calculateWeight(pp.pluginArguments)

	pl := util.NewPodLister(ssn)

	cn := &cachedNodeInfo{
		session: ssn,
	}

	nodeMap, nodeSlice = util.GenerateNodeMapAndSlice(ssn.Nodes)

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
				node.RemovePod(pod)
				klog.V(4).Infof("node order, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
			}
		},
	})

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		nodeInfo, found := nodeMap[node.Name]
		if !found {
			nodeInfo = schedulernodeinfo.NewNodeInfo(node.Pods()...)
			nodeInfo.SetNode(node.Node)
			klog.Warningf("node order, generate node info for %s at NodeOrderFn is unexpected", node.Name)
		}
		var score = 0.0

		//TODO: Add ImageLocalityPriority Function once priorityMetadata is published
		//Issue: #74132 in kubernetes ( https://github.com/kubernetes/kubernetes/issues/74132 )

		host, err := priorities.LeastRequestedPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			klog.Warningf("Least Requested Priority Failed because of Error: %v", err)
			return 0, err
		}
		// If leastReqWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		score = score + float64(host.Score*weight.leastReqWeight)

		host, err = priorities.BalancedResourceAllocationMap(task.Pod, nil, nodeInfo)
		if err != nil {
			klog.Warningf("Balanced Resource Allocation Priority Failed because of Error: %v", err)
			return 0, err
		}
		// If balancedRescourceWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		score = score + float64(host.Score*weight.balancedRescourceWeight)

		host, err = priorities.CalculateNodeAffinityPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			klog.Warningf("Calculate Node Affinity Priority Failed because of Error: %v", err)
			return 0, err
		}
		// If nodeAffinityWeight in provided, host.Score is multiplied with weight, if not, host.Score is added to total score.
		score = score + float64(host.Score*weight.nodeAffinityWeight)

		klog.V(4).Infof("Total Score for task %s/%s on node %s is: %f", task.Namespace, task.Name, node.Name, score)
		return score, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)

	batchNodeOrderFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		PodAffinityScore, err := interPodAffinityScore(task.Pod, cn, nodeMap, nodeSlice, weight.podAffinityWeight)
		if err != nil {
			klog.Warningf("inter pod affinity score failed, Error: %v", err)
			return nil, err
		}

		TolerationScores, err := taintTolerationScore(task.Pod, nodes, nodeMap, weight.taintTolerationWeight)
		if err != nil {
			klog.Warningf("taint toleration score failed, Error: %v", err)
			return nil, err
		}

		score := make(map[string]float64, len(nodes))
		for _, node := range nodes {
			score[node.Name] = TolerationScores[node.Name] + PodAffinityScore[node.Name]
		}

		klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, score)
		return score, nil
	}
	ssn.AddBatchNodeOrderFn(pp.Name(), batchNodeOrderFn)
}

func interPodAffinityScore(
	pod *v1.Pod,
	cn *cachedNodeInfo,
	nodeMap map[string]*schedulernodeinfo.NodeInfo,
	nodeSlice []*v1.Node,
	weight int,
) (map[string]float64, error) {
	var interPodAffinityScore schedulerapi.HostPriorityList

	mapFn := priorities.NewInterPodAffinityPriority(cn, v1.DefaultHardPodAffinitySymmetricWeight)
	interPodAffinityScore, err := mapFn(pod, nodeMap, nodeSlice)
	if err != nil {
		klog.Warningf("Calculate Inter Pod Affinity Priority Failed because of Error: %v", err)
		return nil, err
	}

	score := make(map[string]float64, len(interPodAffinityScore))
	for _, host := range interPodAffinityScore {
		score[host.Host] = float64(host.Score) * float64(weight)
	}

	klog.V(4).Infof("inter pod affinity Score for pod %s/%s is: %v", pod.Namespace, pod.Name, score)

	return score, nil
}

func taintTolerationScore(
	pod *v1.Pod,
	candidateNodes []*api.NodeInfo,
	nodeMap map[string]*schedulernodeinfo.NodeInfo,
	weight int,
) (map[string]float64, error) {
	nodes := make([]*schedulernodeinfo.NodeInfo, 0, len(candidateNodes))
	for _, node := range candidateNodes {
		nodes = append(nodes, nodeMap[node.Name])
	}

	result := make(schedulerapi.HostPriorityList, 0, len(nodes))
	errCh := make(chan error, 1)
	workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), func(index int) {
		score, err := priorities.ComputeTaintTolerationPriorityMap(pod, nil, nodes[index])
		if err != nil {
			errCh <- fmt.Errorf("calculate taint toleration priority failed %v", err)
			return
		}

		result = append(result, score)
	})

	select {
	case err := <-errCh:
		return nil, err
	default:
	}

	err := priorities.ComputeTaintTolerationPriorityReduce(pod, nil, nil, result)
	if err != nil {
		return nil, fmt.Errorf("ComputeTaintTolerationPriorityReduce %v", err)
	}

	score := make(map[string]float64, len(result))
	for _, host := range result {
		score[host.Host] = float64(host.Score) * float64(weight)
	}

	klog.V(4).Infof("taint toleration score for pod %s/%s is: %v", pod.Namespace, pod.Name, score)
	return score, nil
}

func (pp *nodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}

type cachedNodeInfo struct {
	session *framework.Session
}

func (c *cachedNodeInfo) GetNodeInfo(name string) (*v1.Node, error) {
	node, found := c.session.Nodes[name]
	if !found {
		for _, cacheNode := range c.session.Nodes {
			pods := cacheNode.Pods()
			for _, pod := range pods {
				if pod.Spec.NodeName == "" {
					return cacheNode.Node, nil
				}
			}
		}
		return nil, errors.NewNotFound(v1.Resource("node"), name)
	}

	return node.Node, nil
}
