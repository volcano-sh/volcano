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
	"fmt"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/pkg/scheduler/algorithm/priorities"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"
	"k8s.io/kubernetes/pkg/scheduler/cache"

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type nodeOrderPlugin struct {
	// Arguments given for the plugin
	pluginArguments map[string]string
}

func getInterPodAffinityScore(name string, interPodAffinityScore schedulerapi.HostPriorityList) int {
	for _, hostPriority := range interPodAffinityScore {
		if hostPriority.Host == name {
			return hostPriority.Score
		}
	}
	return 0
}

func generateNodeMapAndSlice(nodes map[string]*api.NodeInfo) (map[string]*cache.NodeInfo, []*v1.Node) {
	var nodeMap map[string]*cache.NodeInfo
	var nodeSlice []*v1.Node
	nodeMap = make(map[string]*cache.NodeInfo)
	for _, node := range nodes {
		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)
		nodeMap[node.Name] = nodeInfo
		nodeSlice = append(nodeSlice, node.Node)
	}
	return nodeMap, nodeSlice
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
		return nil, fmt.Errorf("failed to find node <%s>", name)
	}

	return node.Node, nil
}

type podLister struct {
	session *framework.Session
}

func (pl *podLister) List(selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if selector.Matches(labels.Set(task.Pod.Labels)) {
					if task.NodeName != task.Pod.Spec.NodeName {
						pod := task.Pod.DeepCopy()
						pod.Spec.NodeName = task.NodeName
						pods = append(pods, pod)
					} else {
						pods = append(pods, task.Pod)
					}
				}
			}
		}
	}

	return pods, nil
}

func (pl *podLister) FilteredList(podFilter algorithm.PodFilter, selector labels.Selector) ([]*v1.Pod, error) {
	var pods []*v1.Pod
	for _, job := range pl.session.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if !api.AllocatedStatus(status) {
				continue
			}

			for _, task := range tasks {
				if podFilter(task.Pod) && selector.Matches(labels.Set(task.Pod.Labels)) {
					if task.NodeName != task.Pod.Spec.NodeName {
						pod := task.Pod.DeepCopy()
						pod.Spec.NodeName = task.NodeName
						pods = append(pods, pod)
					} else {
						pods = append(pods, task.Pod)
					}
				}
			}
		}
	}

	return pods, nil
}

type nodeLister struct {
	session *framework.Session
}

func (nl *nodeLister) List() ([]*v1.Node, error) {
	var nodes []*v1.Node
	for _, node := range nl.session.Nodes {
		nodes = append(nodes, node.Node)
	}
	return nodes, nil
}

//New function returns prioritizePlugin object
func New(aruguments map[string]string) framework.Plugin {
	return &nodeOrderPlugin{pluginArguments: aruguments}
}

func (pp *nodeOrderPlugin) Name() string {
	return "nodeorder"
}

func (pp *nodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (int, error) {

		pl := &podLister{
			session: ssn,
		}

		nl := &nodeLister{
			session: ssn,
		}

		cn := &cachedNodeInfo{
			session: ssn,
		}

		var nodeMap map[string]*cache.NodeInfo
		var nodeSlice []*v1.Node
		var interPodAffinityScore schedulerapi.HostPriorityList

		nodeMap, nodeSlice = generateNodeMapAndSlice(ssn.Nodes)

		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)
		var score = 0

		//TODO: Add ImageLocalityPriority Function once priorityMetadata is published
		//Issue: #74132 in kubernetes ( https://github.com/kubernetes/kubernetes/issues/74132 )

		host, err := priorities.LeastRequestedPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.Warningf("Least Requested Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.BalancedResourceAllocationMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.Warningf("Balanced Resource Allocation Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.CalculateNodeAffinityPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.Warningf("Calculate Node Affinity Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		mapFn := priorities.NewInterPodAffinityPriority(cn, nl, pl, v1.DefaultHardPodAffinitySymmetricWeight)
		interPodAffinityScore, err = mapFn(task.Pod, nodeMap, nodeSlice)
		if err != nil {
			glog.Warningf("Calculate Inter Pod Affinity Priority Failed because of Error: %v", err)
			return 0, err
		}
		hostScore := getInterPodAffinityScore(node.Name, interPodAffinityScore)
		score = score + hostScore

		glog.V(4).Infof("Total Score for that node is: %d", score)
		return score, nil
	}
	ssn.AddNodeOrderFn(pp.Name(), nodeOrderFn)
}

func (pp *nodeOrderPlugin) OnSessionClose(ssn *framework.Session) {
}
