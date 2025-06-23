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

package noderesourcefitplus

import (
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "node-resource-fit-plus"
)

type nodeResourcesFitPlus struct {
	NodeResourcesFitPlusWeight int                               `json:"nodeResourcesFitPlusWeight"`
	Resources                  map[v1.ResourceName]ResourcesType `json:"resources"`
}

type ResourcesType struct {
	Type   config.ScoringStrategyType `json:"type"`
	Weight int                        `json:"weight"`
}

func (w *nodeResourcesFitPlus) String() string {
	marshal, err := json.Marshal(w)
	if err != nil {
		return ""
	}
	return string(marshal)
}

type nodeResourcesFitPlusPlugin struct {
	// Arguments given for the plugin
	weight nodeResourcesFitPlus
}

// New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(aruguments)
	return &nodeResourcesFitPlusPlugin{weight: weight}
}

func calculateWeight(args framework.Arguments) nodeResourcesFitPlus {

	/*
	   actions: "enqueue, reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: node-resource-fit-plus
	        arguments:
	          nodeResourcesFitPlusWeight: 10
	          resources:
	            cpu:
	              type: MostAllocated
	              weight: 1
	            memory:
	              type: LeastAllocated
	              weight: 2
	*/

	var weight nodeResourcesFitPlus

	nodeResourcesFitPlusWeight, b := framework.Get[int](args, "nodeResourcesFitPlusWeight")
	if !b || nodeResourcesFitPlusWeight == 0 {
		nodeResourcesFitPlusWeight = 10
	}
	weight.NodeResourcesFitPlusWeight = nodeResourcesFitPlusWeight

	resources, b := framework.Get[map[v1.ResourceName]ResourcesType](args, "resources")
	if !b || len(resources) == 0 {
		resources = map[v1.ResourceName]ResourcesType{
			"cpu": {
				Type:   config.LeastAllocated,
				Weight: 1,
			},
			"memory": {
				Type:   config.LeastAllocated,
				Weight: 1,
			},
		}
	}
	weight.Resources = resources
	return weight
}

func (bp *nodeResourcesFitPlusPlugin) Name() string {
	return PluginName
}

func (bp *nodeResourcesFitPlusPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter nodeResourcesFitPlus plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving nodeResourcesFitPlus plugin. %s ...", bp.weight.String())
	}()

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		binPackingScore := PlusScore(task, node, bp.weight)
		klog.V(4).Infof("Binpack score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, binPackingScore)
		return binPackingScore, nil
	}
	if bp.weight.NodeResourcesFitPlusWeight != 0 {
		ssn.AddNodeOrderFn(bp.Name(), nodeOrderFn)
	} else {
		klog.Infof("binpack weight is zero, skip node order function")
	}
}

func PlusScore(task *api.TaskInfo, node *api.NodeInfo, weight nodeResourcesFitPlus) float64 {
	score := 0.0
	weightSum := 0
	requested := task.Resreq
	allocatable := node.Allocatable
	used := node.Used

	for _, resource := range requested.ResourceNames() {
		request := requested.Get(resource)

		allocate := allocatable.Get(resource)
		nodeUsed := used.Get(resource)

		_, found := weight.Resources[resource]
		if !found {
			continue
		}

		resourcePloy := weight.Resources[resource].Type
		resourceWeight := weight.Resources[resource].Weight

		var resourceScore float64
		var err error
		switch resourcePloy {
		case config.MostAllocated:
			resourceScore, err = mostRequestedScore(request, nodeUsed, allocate, resourceWeight)
		case config.LeastAllocated:
			resourceScore, err = leastRequestedScore(request, nodeUsed, allocate, resourceWeight)
		}

		if err != nil {
			klog.V(4).Infof("task %s/%s cannot binpack node %s: resource: %s is %s, need %f, used %f, allocatable %f",
				task.Namespace, task.Name, node.Name, resource, err.Error(), request, nodeUsed, allocate)
			return 0
		}

		klog.V(5).Infof("task %s/%s on node %s resource %s, need %f, used %f, allocatable %f, weight %d, score %f",
			task.Namespace, task.Name, node.Name, resource, request, nodeUsed, allocate, resourceWeight, resourceScore)

		score += resourceScore
		weightSum += resourceWeight
	}

	// mapping the result from [0, weightSum] to [0, 10(MaxPriority)]
	if weightSum > 0 {
		score /= float64(weightSum)
	}
	score *= float64(k8sFramework.MaxNodeScore * int64(weight.NodeResourcesFitPlusWeight))
	return score
}

func mostRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if capacity == 0 || weight == 0 {
		return 0, fmt.Errorf("node capacity is zero")
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("not enough")
	}

	score := usedFinally * float64(weight) / capacity
	return score, nil
}

func leastRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if capacity == 0 || weight == 0 {
		return 0, fmt.Errorf("node capacity is zero")
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("not enough")
	}

	score := (capacity - usedFinally) * float64(weight) / capacity
	return score, nil
}

func (bp *nodeResourcesFitPlusPlugin) OnSessionClose(ssn *framework.Session) {
}
