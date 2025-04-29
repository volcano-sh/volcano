/*
Copyright 2024 The Volcano Authors.

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

package sra

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"strings"

	"k8s.io/klog/v2"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "sra"
)

const (
	// SraWeight is the key for providing Sra Weight in YAML
	SraWeight = "sra.weight"
	// SraResources is the key for additional resource key name
	SraResources = "sra.resources"
	// sraResourcesPrefix is the key prefix for additional resource key name
	SraResourcesPrefix = SraResources + "."
	resourceFmt        = "%s[%d]"
)

type priorityWeight struct {
	SraPluginWeight int
	SraResources    map[v1.ResourceName]int
	SraWeightSum    int
}

func (w *priorityWeight) String() string {
	length := 1
	if extendLength := len(w.SraResources); extendLength == 0 {
		length++
	} else {
		length += extendLength
	}

	msg := make([]string, 0, length)
	msg = append(msg,
		fmt.Sprintf(resourceFmt, SraWeight, w.SraPluginWeight),
	)

	if len(w.SraResources) == 0 {
		msg = append(msg, "no extend resources.")
	} else {
		for name, weight := range w.SraResources {
			msg = append(msg, fmt.Sprintf(resourceFmt, name, weight))
		}
	}

	return strings.Join(msg, ", ")
}

type sraPlugin struct {
	// Arguments given for the plugin
	weight priorityWeight
}

// New function returns prioritizePlugin object
func New(aruguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(aruguments)
	return &sraPlugin{weight: weight}
}

func calculateWeight(args framework.Arguments) priorityWeight {
	/*
		   User Should give priorityWeight in this format(sra.weight, sra.resources.[ResourceName]).
		   Support change the weight about cpu, memory and additional resource by arguments.

		   actions: "enqueue, reclaim, allocate, backfill, preempt"
		   tiers:
		   - plugins:
		     - name: sra
		       arguments:
		         sra.weight: 10
				 sra.resources: nvidia.com/t4, nvidia.com/a10
		         sra.resources.nvidia.com/t4: 1
		         sra.resources.nvidia.com/a10: 1
	*/
	// Values are initialized to 1.
	weight := priorityWeight{
		SraPluginWeight: 1,
		SraResources:    make(map[v1.ResourceName]int),
		SraWeightSum:    0,
	}

	// Checks whether sra.weight is provided or not, if given, modifies the value in weight struct.
	args.GetInt(&weight.SraPluginWeight, SraWeight)

	resourcesStr, ok := args[SraResources].(string)
	if !ok {
		resourcesStr = ""
	}

	resources := strings.Split(resourcesStr, ",")
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}

		// sra.resources.[ResourceName]
		resourceKey := SraResourcesPrefix + resource
		resourceWeight := 1
		args.GetInt(&resourceWeight, resourceKey)
		if resourceWeight < 0 {
			resourceWeight = 1
		}
		weight.SraResources[v1.ResourceName(resource)] = resourceWeight
		weight.SraWeightSum += resourceWeight
	}

	return weight
}

func (sp *sraPlugin) Name() string {
	return PluginName
}

func (sp *sraPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter sra plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving sra plugin. %s ...", sp.weight.String())
	}()
	if klog.V(4).Enabled() {
		notFoundResource := []string{}
		for resource := range sp.weight.SraResources {
			found := false
			for _, nodeInfo := range ssn.Nodes {
				if nodeInfo.Capacity.Get(resource) > 0 {
					found = true
					break
				}
			}
			if !found {
				notFoundResource = append(notFoundResource, string(resource))
			}
		}

		if len(notFoundResource) > 0 {
			klog.V(4).Infof("resources [%s] record in weight but not found on any node", strings.Join(notFoundResource, ", "))
		}
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		sraScore := sraScore(task, node, sp.weight)

		klog.V(4).Infof("Sra score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, sraScore)
		return sraScore, nil
	}
	if sp.weight.SraPluginWeight != 0 {
		ssn.AddNodeOrderFn(sp.Name(), nodeOrderFn)
	} else {
		klog.Infof("sra weight is zero, skip node order function")
	}
}

func (sp *sraPlugin) OnSessionClose(ssn *framework.Session) {
}

// SraScore use the best fit polices during scheduling.
// Goals:
// - Schedule Tasks using BestFit Policy using scarce resource avoidance strategy
// - Improve the utilization of scarce resources on the cluster
func sraScore(task *api.TaskInfo, node *api.NodeInfo, weight priorityWeight) float64 {
	requested := task.Resreq
	capacity := node.Capacity

	// check if the node has the requested resource item
	for _, resource := range requested.ResourceNames() {
		resourceCapacity := capacity.Get(resource)

		if resourceCapacity == 0 {
			// node resources can't meet the task request, so it can be disregarded.
			klog.V(4).Infof("task %s/%s cannot sra node %s, because node capacity: %v , task need: %v",
				task.Namespace, task.Name, node.Name, capacity, requested)
			return 0
		}
	}

	score, err := ResourceSraScore(weight.SraResources, capacity)
	if err != nil {
		klog.V(4).Infof("task %s/%s cannot sra node %s, node capacity: %v , task need: %v , error: %s",
			task.Namespace, task.Name, node.Name, capacity, requested, err.Error())
		return 0
	}
	klog.V(5).Infof("task %s/%s on node %s , node capacity: %s, task need: %v, score %v",
		task.Namespace, task.Name, node.Name, capacity, requested, score)

	// mapping the result from [0, weightSum] to [0, MaxNodeScore]
	if weight.SraWeightSum > 0 {
		score /= float64(weight.SraWeightSum)
		score = 1.0 - score
	}
	score *= float64(k8sFramework.MaxNodeScore * int64(weight.SraPluginWeight))

	return score
}

// ResourceSraScore calculate the sra score for resource with provided info
func ResourceSraScore(sraResource map[v1.ResourceName]int, capacity *api.Resource) (float64, error) {
	score := 0.0

	for resource, weight := range sraResource {
		resourceCapacity := capacity.Get(resource)

		if resourceCapacity > 0 {
			score += float64(weight)
		}
	}

	return score, nil
}
