/*
Copyright 2025 The Volcano Authors.

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

package resourcestrategyfit

import (
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "resource-strategy-fit"
	// DefaultResourceStrategyFitPluginWeight is the default weight of resourceStrategyFit plugin
	DefaultResourceStrategyFitPluginWeight = 10
	// SraPolicy is the key for providing SRA policy
	SraPolicy = "sra.policy"
	// SraRetentionWeight is the key for providing retention policy Weight in YAML
	SraRetentionWeight = "sra.retention.weight"
	// resourceFmt is the format for resource key
	resourceFmt = "%s[%d]"
)

type ResourceStrategyFit struct {
	ResourceStrategyFitWeight int                               `json:"resourceStrategyFitWeight"`
	Resources                 map[v1.ResourceName]ResourcesType `json:"resources"`
}

type ResourcesType struct {
	Type   config.ScoringStrategyType `json:"type"`
	Weight int                        `json:"weight"`
}

func (w *ResourceStrategyFit) String() string {
	marshal, err := json.Marshal(w)
	if err != nil {
		return ""
	}
	return string(marshal)
}

type Sra struct {
	Policy       string
	Retention    retentionWeight
	Proportional map[v1.ResourceName]baseResource
}

type sraConfig struct {
	Policy       string             `json:"policy"`
	Resources    string             `json:"resources"`
	Retention    map[string]int     `json:"retention"`
	Proportional map[string]float64 `json:"proportional"`
}

type resourceStrategyFitPlugin struct {
	// Arguments given for the plugin
	weight ResourceStrategyFit
	// Policy for sra
	sraPolicy Sra
}

// New function returns prioritizePlugin object
func New(arguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(arguments)
	rsf := resourceStrategyFitPlugin{weight: weight}
	applySraPolicy(&rsf, arguments)

	return &rsf
}

func calculateWeight(args framework.Arguments) ResourceStrategyFit {
	/*
		actions: "enqueue, allocate, backfill, reclaim, preempt"
		tiers:
		- plugins:
		  - name: resource-strategy-fit
		    arguments:
		       resourceStrategyFitWeight: 10
		       resources:
		         nvidia.com/gpu:
		           type: MostAllocated
		           weight: 2
		         cpu:
		           type: LeastAllocated
		           weight: 1
		       sra:
		         policy: retention
		         resources: nvidia.com/gpu
		         retention:
		           weight: 10
		           nvidia.com/gpu: 1
		         proportional:
		           nvidia.com/gpu.cpu: 4
		           nvidia.com/gpu.memory: 8
	*/

	var weight ResourceStrategyFit

	resourceStrategyFitPluginWeight, b := framework.Get[int](args, "resourceStrategyFitWeight")
	if !b || resourceStrategyFitPluginWeight < 0 {
		resourceStrategyFitPluginWeight = DefaultResourceStrategyFitPluginWeight
	}
	weight.ResourceStrategyFitWeight = resourceStrategyFitPluginWeight

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

	for k, v := range resources {
		if v.Weight <= 0 {
			v.Weight = 1
		}
		if v.Type != config.LeastAllocated && v.Type != config.MostAllocated {
			v.Type = config.LeastAllocated
		}
		resources[k] = v
	}
	weight.Resources = resources
	return weight
}

func applySraPolicy(rsf *resourceStrategyFitPlugin, args framework.Arguments) {
	sraPolicy := Sra{
		Policy:       "",
		Retention:    retentionWeight{},
		Proportional: map[v1.ResourceName]baseResource{},
	}

	cfg, _ := framework.Get[sraConfig](args, "sra")
	klog.V(4).Infof("sra resources: %v is provided", cfg.Resources)

	// Obtain sra resource key name
	resources := strings.Split(cfg.Resources, ",")

	switch strings.ToLower(cfg.Policy) {
	case "":
		klog.V(4).Infof("%s is not provided", SraPolicy)
	case RetentionPolicy:
		sraPolicy.Policy = RetentionPolicy
		sraPolicy.Retention = calculateRetentionWeight(resources, cfg)
		klog.V(4).Infof("%s: %s is provided", SraPolicy, RetentionPolicy)
	case ProportionalPolicy:
		sraPolicy.Policy = ProportionalPolicy
		sraPolicy.Proportional = calculateProportionalResources(resources, cfg)
		klog.V(4).Infof("%s: %s is provided", SraPolicy, ProportionalPolicy)
	default:
		klog.V(4).Infof("%s: %s is not supported", SraPolicy, sraPolicy.Policy)
		sraPolicy.Policy = ""
	}

	rsf.sraPolicy = sraPolicy
}

func (rsf *resourceStrategyFitPlugin) Name() string {
	return PluginName
}

func (rsf *resourceStrategyFitPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter resourceStrategyFit plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving resourceStrategyFit plugin. %s ...", rsf.weight.String())
	}()

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		predicateStatus := make([]*api.Status, 0)

		// Check ProportionalPredicate
		proportionalStatus, _ := checkNodeResourceIsProportional(task, node, rsf.sraPolicy.Proportional)
		if proportionalStatus.Code != api.Success {
			predicateStatus = append(predicateStatus, proportionalStatus)
			if util.ShouldAbort(proportionalStatus) {
				return api.NewFitErrWithStatus(task, node, predicateStatus...)
			}
		}
		klog.V(5).Infof("proportional policy filter for task <%s/%s> on node <%s> pass.",
			task.Namespace, task.Name, node.Name)
		return nil
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		score := 0.0

		if rsf.weight.ResourceStrategyFitWeight > 0 {
			score += Score(task, node, rsf.weight)
			klog.V(4).Infof("resourceStrategyFit policy score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		}

		if rsf.sraPolicy.Policy == RetentionPolicy && rsf.sraPolicy.Retention.Weight > 0 {
			klog.V(5).Infof("sra.retention weight is %d", rsf.sraPolicy.Retention.Weight)
			if klog.V(4).Enabled() {
				var notFoundResource []string
				for resource := range rsf.sraPolicy.Retention.Resources {
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
					klog.V(4).Infof("resources [%s] record in sra.resources but not found on any node",
						strings.Join(notFoundResource, ", "))
				}
			}

			rs := retentionScore(task, node, rsf.sraPolicy.Retention)
			klog.V(4).Infof("sra.retention score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, rs)
			score += rs
		}

		klog.V(4).Infof("resource-strategy-fit plugin total score (sra + resourceStrategyFit) for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		return score, nil
	}

	if rsf.sraPolicy.Policy == ProportionalPolicy {
		ssn.AddPredicateFn(rsf.Name(), predicateFn)
	} else {
		klog.V(4).Infof("proportional policy is not enabled, skip predicate function")
	}

	if rsf.weight.ResourceStrategyFitWeight <= 0 && (rsf.sraPolicy.Policy != RetentionPolicy || rsf.sraPolicy.Retention.Weight <= 0) {
		klog.V(4).Infof("The weights of both resourceStrategyFit and sra.retention are zero, skip node order function")
	} else {
		ssn.AddNodeOrderFn(rsf.Name(), nodeOrderFn)
	}
}

func Score(task *api.TaskInfo, node *api.NodeInfo, weight ResourceStrategyFit) float64 {
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

		scoringType := weight.Resources[resource].Type
		resourceWeight := weight.Resources[resource].Weight

		var resourceScore float64
		var err error
		switch scoringType {
		case config.MostAllocated:
			resourceScore, err = mostRequestedScore(request, nodeUsed, allocate, resourceWeight)
		case config.LeastAllocated:
			resourceScore, err = leastRequestedScore(request, nodeUsed, allocate, resourceWeight)
		}

		if err != nil {
			klog.V(4).Infof("task %s/%s cannot resourceStrategyFit node %s: resource: %s is %s, need %f, used %f, allocatable %f",
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
	score *= float64(k8sFramework.MaxNodeScore * int64(weight.ResourceStrategyFitWeight))
	return score
}

func mostRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if capacity == 0 || weight == 0 {
		return 0, nil
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("node resources are not enough")
	}

	score := usedFinally * float64(weight) / capacity
	return score, nil
}

func leastRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if capacity == 0 || weight == 0 {
		return 0, nil
	}

	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("node resources are not enough")
	}

	score := (capacity - usedFinally) * float64(weight) / capacity
	return score, nil
}

func (rsf *resourceStrategyFitPlugin) OnSessionClose(ssn *framework.Session) {
}
