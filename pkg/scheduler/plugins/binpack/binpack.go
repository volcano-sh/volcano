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
	"fmt"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	schedulerapi "k8s.io/kubernetes/pkg/scheduler/api"

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
	// BinpackMemory is the key for memory of cpu
	BinpackMemory = "binpack.memory"

	// BinpackResources is the key for additional resource key name
	BinpackResources = "binpack.resources"
	// BinpackResourcesPrefix is the key prefix for additional resource key name
	BinpackResourcesPrefix = BinpackResources + "."

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
	weight priorityWeight
}

//New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(aruguments)
	return &binpackPlugin{weight: weight}
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

	resourcesStr := args[BinpackResources]
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

	return weight
}

func (bp *binpackPlugin) Name() string {
	return PluginName
}

func (bp *binpackPlugin) OnSessionOpen(ssn *framework.Session) {
	glog.V(3).Infof("Enter binpack plugin ...")
	if glog.V(3) {
		defer func() {
			glog.V(3).Infof("Leaving binpack plugin. %s ...", bp.weight.String())
		}()
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		binPackingScore := BinPackingScore(task, node, bp.weight)

		glog.V(4).Infof("Binpack score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, binPackingScore)
		return binPackingScore, nil
	}
	if bp.weight.BinPackingWeight != 0 {
		ssn.AddNodeOrderFn(bp.Name(), nodeOrderFn)
	} else {
		glog.Infof("binpack weight is zero, skip node order function")
	}
}

func (bp *binpackPlugin) OnSessionClose(ssn *framework.Session) {
}

// BinPackingScore use the best fit polices during scheduling.
// Goals:
// - Schedule Jobs using BestFit Policy using Resource Bin Packing Priority Function
// - Reduce Fragmentation of scarce resources on the Cluster
func BinPackingScore(task *api.TaskInfo, node *api.NodeInfo, weight priorityWeight) float64 {
	score := 0.0
	weightSum := 0
	requested := task.Resreq
	allocatable := node.Allocatable
	used := node.Used

	score += ResourceBinPackingScore(requested.MilliCPU, allocatable.MilliCPU, used.MilliCPU, weight.BinPackingCPU)
	weightSum += weight.BinPackingCPU
	score += ResourceBinPackingScore(requested.Memory, allocatable.Memory, used.Memory, weight.BinPackingMemory)
	weightSum += weight.BinPackingMemory

	// All resource with weight should be calculated, because the weightSum need it,
	// even the node have no this resource.
	for name, weight := range weight.BinPackingResources {
		weightSum += weight
		score += ResourceBinPackingScore(
			requested.ScalarResources[name], allocatable.ScalarResources[name],
			used.ScalarResources[name], weight,
		)
	}

	// mapping the result from [0, weightSum] to [0, 10(MaxPriority)]
	if weightSum > 0 {
		score = score / float64(weightSum)
	}
	score *= schedulerapi.MaxPriority * float64(weight.BinPackingWeight)

	return score
}

// ResourceBinPackingScore calculate the binpack score for resource with provided info
func ResourceBinPackingScore(requested, capacity, used float64, weight int) float64 {
	if capacity == 0 || weight == 0 {
		return 0
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0
	}

	score := usedFinally * float64(weight) / capacity
	return score
}
