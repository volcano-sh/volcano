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

package binpack

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "binpack"
)

const (
	// BinpackWeight is the key for providing Binpack Priority Weight in YAML
	BinpackWeight = "binpack.weight"
	// BinpackCPU is the key for weight of cpu
	BinpackCPU = "binpack.cpu"
	// BinpackMemory is the key for weight of memory
	BinpackMemory = "binpack.memory"

	// BinpackResources is the key for additional resource key name
	BinpackResources = "binpack.resources"
	// BinpackResourcesPrefix is the key prefix for additional resource key name
	BinpackResourcesPrefix = BinpackResources + "."
	// DynamicResourceAllocationEnable enables count-level DRA resource scoring.
	DynamicResourceAllocationEnable = "binpack.DynamicResourceAllocationEnable"

	resourceFmt = "%s[%d]"
)

type priorityWeight struct {
	BinPackingWeight    int
	BinPackingCPU       int
	BinPackingMemory    int
	BinPackingResources map[v1.ResourceName]int
}

func (w *priorityWeight) String() string {
	length := 3
	if extendLength := len(w.BinPackingResources); extendLength == 0 {
		length++
	} else {
		length += extendLength
	}
	msg := make([]string, 0, length)
	msg = append(msg,
		fmt.Sprintf(resourceFmt, BinpackWeight, w.BinPackingWeight),
		fmt.Sprintf(resourceFmt, BinpackCPU, w.BinPackingCPU),
		fmt.Sprintf(resourceFmt, BinpackMemory, w.BinPackingMemory),
	)

	if len(w.BinPackingResources) == 0 {
		msg = append(msg, "no extend resources.")
	} else {
		for name, weight := range w.BinPackingResources {
			msg = append(msg, fmt.Sprintf(resourceFmt, name, weight))
		}
	}
	return strings.Join(msg, ", ")
}

type binpackPlugin struct {
	// Arguments given for the plugin
	weight                          priorityWeight
	dynamicResourceAllocationEnable bool
}

// New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(aruguments)
	dynamicResourceAllocationEnable := utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DynamicResourceAllocation)
	aruguments.GetBool(&dynamicResourceAllocationEnable, DynamicResourceAllocationEnable)
	return &binpackPlugin{
		weight:                          weight,
		dynamicResourceAllocationEnable: dynamicResourceAllocationEnable,
	}
}

func calculateWeight(args framework.Arguments) priorityWeight {
	/*
	   User Should give priorityWeight in this format(binpack.weight, binpack.cpu, binpack.memory).
	   Support change the weight about cpu, memory and additional resource by arguments.

	   actions: "enqueue, reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: binpack
	       arguments:
	         binpack.weight: 10
	         binpack.cpu: 5
	         binpack.memory: 1
	         binpack.resources: nvidia.com/gpu, example.com/foo
	         binpack.resources.nvidia.com/gpu: 2
	         binpack.resources.example.com/foo: 3
	*/
	// Values are initialized to 1.
	weight := priorityWeight{
		BinPackingWeight:    1,
		BinPackingCPU:       1,
		BinPackingMemory:    1,
		BinPackingResources: make(map[v1.ResourceName]int),
	}

	// Checks whether binpack.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.BinPackingWeight, BinpackWeight)
	// Checks whether binpack.cpu is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.BinPackingCPU, BinpackCPU)
	if weight.BinPackingCPU < 0 {
		weight.BinPackingCPU = 1
	}
	// Checks whether binpack.memory is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.BinPackingMemory, BinpackMemory)
	if weight.BinPackingMemory < 0 {
		weight.BinPackingMemory = 1
	}

	resourcesStr, ok := args[BinpackResources].(string)
	if !ok {
		resourcesStr = ""
	}

	resources := strings.Split(resourcesStr, ",")
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		// binpack.resources.[ResourceName]
		resourceKey := BinpackResourcesPrefix + resource
		resourceWeight := 1
		args.GetInt(&resourceWeight, resourceKey)
		if resourceWeight < 0 {
			resourceWeight = 1
		}
		weight.BinPackingResources[v1.ResourceName(resource)] = resourceWeight
	}

	weight.BinPackingResources[v1.ResourceCPU] = weight.BinPackingCPU
	weight.BinPackingResources[v1.ResourceMemory] = weight.BinPackingMemory

	return weight
}

func (bp *binpackPlugin) Name() string {
	return PluginName
}

func (bp *binpackPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter binpack plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving binpack plugin. %s ...", bp.weight.String())
	}()
	if klog.V(4).Enabled() {
		notFoundResource := []string{}
		for resource := range bp.weight.BinPackingResources {
			found := false
			for _, nodeInfo := range ssn.Nodes {
				if nodeInfo.Allocatable.Get(resource) > 0 {
					found = true
					break
				}
			}
			if !found {
				notFoundResource = append(notFoundResource, string(resource))
			}
		}
		klog.V(4).Infof("resources [%s] record in weight but not found on any node", strings.Join(notFoundResource, ", "))
	}

	if bp.weight.BinPackingWeight == 0 {
		klog.Infof("binpack weight is zero, skip node order function")
		return
	}

	if !bp.dynamicResourceAllocationEnable {
		ssn.AddNodeOrderFn(bp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
			score := BinPackingScore(task, node, bp.weight)
			klog.V(4).Infof("Binpack score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
			return score, nil
		})
		return
	}

	// DRA state can change after each task is reserved. Calculate it once per
	// task scoring cycle, then use the same snapshot for all candidate nodes.
	ssn.AddBatchNodeOrderFn(bp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		var (
			draResources draNodeResources
			draRequest   map[string]*api.DRAResource
			err          error
		)
		if len(task.DRAResreq) > 0 {
			draResources, draRequest, err = calculateDRAScoringState(context.Background(), ssn.SharedDRAManager(), task)
			if err != nil {
				return nil, err
			}
		}

		scores := make(map[string]float64, len(nodes))
		for _, node := range nodes {
			score := binPackingScore(task, node, bp.weight, draRequest, draResources[node.Name])
			scores[node.Name] = score
			klog.V(4).Infof("Binpack score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		}
		return scores, nil
	})
}

func (bp *binpackPlugin) OnSessionClose(ssn *framework.Session) {
}

// BinPackingScore use the best fit polices during scheduling.
// Goals:
// - Schedule Jobs using BestFit Policy using Resource Bin Packing Priority Function
// - Reduce Fragmentation of scarce resources on the Cluster
func BinPackingScore(task *api.TaskInfo, node *api.NodeInfo, weight priorityWeight) float64 {
	return binPackingScore(task, node, weight, nil, nil)
}

func binPackingScore(task *api.TaskInfo, node *api.NodeInfo, weight priorityWeight, draRequest map[string]*api.DRAResource, draResources map[string]*draNodeResource) float64 {
	score := 0.0
	weightSum := 0
	requested := task.Resreq
	allocatable := node.Allocatable
	used := node.Used

	for _, resource := range requested.ResourceNames() {
		request := requested.Get(resource)
		if request == 0 {
			continue
		}
		allocate := allocatable.Get(resource)
		nodeUsed := used.Get(resource)

		resourceWeight, found := weight.BinPackingResources[resource]
		if !found {
			continue
		}

		resourceScore, err := ResourceBinPackingScore(request, allocate, nodeUsed, resourceWeight)
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

	for deviceClass, request := range draRequest {
		if request == nil || request.Count == 0 {
			continue
		}

		var allocatable, used int64
		if nodeResource := draResources[deviceClass]; nodeResource != nil {
			allocatable = nodeResource.Allocatable.Count
			used = nodeResource.Used.Count
		}
		resourceScore, err := ResourceBinPackingScore(
			float64(request.Count),
			float64(allocatable),
			float64(used),
			1,
		)
		if err != nil {
			klog.V(4).Infof("task %s/%s cannot binpack node %s: DeviceClass %s is %s, need %d, used %d, allocatable %d",
				task.Namespace, task.Name, node.Name, deviceClass, err.Error(), request.Count, used, allocatable)
			return 0
		}
		klog.V(5).Infof("task %s/%s on node %s DeviceClass %s, need %d, used %d, allocatable %d, score %f",
			task.Namespace, task.Name, node.Name, deviceClass, request.Count, used, allocatable, resourceScore)
		score += resourceScore
		weightSum++
	}

	// mapping the result from [0, weightSum] to [0, 10(MaxPriority)]
	if weightSum > 0 {
		score /= float64(weightSum)
	}
	score *= float64(fwk.MaxNodeScore * int64(weight.BinPackingWeight))

	return score
}

// ResourceBinPackingScore calculate the binpack score for resource with provided info
func ResourceBinPackingScore(requested, capacity, used float64, weight int) (float64, error) {
	if capacity == 0 || weight == 0 {
		return 0, nil
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("not enough")
	}

	score := usedFinally * float64(weight) / capacity
	return score, nil
}
