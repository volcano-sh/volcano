/*
Copyright 2018 The Kubernetes Authors.

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

package priority

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	priority "github.com/kubernetes-sigs/kube-batch/pkg/scheduler/util/priorities"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/scheduler/algorithm"
	"k8s.io/kubernetes/pkg/scheduler/algorithm/priorities"
	"k8s.io/kubernetes/pkg/scheduler/cache"
)

type priorityPlugin struct {
}

func generateNodeMapAndSlice(nodes []*api.NodeInfo) (map[string]*cache.NodeInfo, []*v1.Node) {
	var nodeMap map[string]*cache.NodeInfo
	var nodeSlice []*v1.Node
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
	node, found := c.session.NodeIndex[name]
	if !found {
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
					pod := task.Pod.DeepCopy()
					pod.Spec.NodeName = task.NodeName
					pods = append(pods, pod)
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
					pod := task.Pod.DeepCopy()
					pod.Spec.NodeName = task.NodeName
					pods = append(pods, pod)
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

func New() framework.Plugin {
	return &priorityPlugin{}
}

func (pp *priorityPlugin) Name() string {
	return "priority"
}

func (pp *priorityPlugin) OnSessionOpen(ssn *framework.Session) {
	taskOrderFn := func(l interface{}, r interface{}) int {
		lv := l.(*api.TaskInfo)
		rv := r.(*api.TaskInfo)

		glog.V(4).Infof("Priority TaskOrder: <%v/%v> prority is %v, <%v/%v> priority is %v",
			lv.Namespace, lv.Name, lv.Priority, rv.Namespace, rv.Name, rv.Priority)

		if lv.Priority == rv.Priority {
			return 0
		}

		if lv.Priority > rv.Priority {
			return -1
		}

		return 1
	}

	// Add Task Order function
	ssn.AddTaskOrderFn(pp.Name(), taskOrderFn)

	jobOrderFn := func(l, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		glog.V(4).Infof("Priority JobOrderFn: <%v/%v> is ready: %d, <%v/%v> is ready: %d",
			lv.Namespace, lv.Name, lv.Priority, rv.Namespace, rv.Name, rv.Priority)

		if lv.Priority > rv.Priority {
			return -1
		}

		if lv.Priority < rv.Priority {
			return 1
		}

		return 0
	}

	ssn.AddJobOrderFn(pp.Name(), jobOrderFn)

	priorityFn := func(task *api.TaskInfo, node *api.NodeInfo, nodes []*api.NodeInfo) (int, error) {
		nodeInfo := cache.NewNodeInfo(node.Pods()...)
		nodeInfo.SetNode(node.Node)
		var score int = 0

		host, err := priority.ImageLocalityPriorityMap(task.Pod, len(nodes), nodeInfo)
		if err != nil {
			glog.V(3).Infof("Image Locality Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.BalancedResourceAllocationMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Balanced Resource Allocation Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.LeastRequestedPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Least Requested Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.MostRequestedPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Most Requested Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.CalculateNodePreferAvoidPodsPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Calculate Node Prefer Avoid Pods Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.ResourceLimitsPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Resource Limits Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.ComputeTaintTolerationPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Compute Taint Toleration Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		host, err = priorities.CalculateNodeAffinityPriorityMap(task.Pod, nil, nodeInfo)
		if err != nil {
			glog.V(3).Infof("Calculate Node Affinity Priority Failed because of Error: %v", err)
			return 0, err
		}
		score = score + host.Score

		for label, _ := range task.Pod.Labels {
			mapFn, _ := priorities.NewNodeLabelPriority(label, true)
			host, err := mapFn(task.Pod, nil, nodeInfo)
			if err != nil {
				glog.V(3).Infof("Calculate Node Label Priority Failed because of Error: %v", err)
				return 0, err
			}
			score = score + host.Score
		}

		glog.V(3).Infof("Total Score for that node is: %f", score)
		return score, nil
	}
	ssn.AddPriorityFn(pp.Name(), priorityFn)
}

func (pp *priorityPlugin) OnSessionClose(ssn *framework.Session) {}
