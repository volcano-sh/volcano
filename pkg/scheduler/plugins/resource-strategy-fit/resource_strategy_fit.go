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
	// SraWeight is the key for providing sra weight in YAML
	SraWeight = "sra.weight"
	// resourceFmt is the format for resource key
	resourceFmt = "%s[%d]"
)

type ResourceStrategyFit struct {
	Weight    int                               `json:"resourceStrategyFitWeight"`
	Resources map[v1.ResourceName]ResourcesType `json:"resources"`
}

type ResourcesType struct {
	Type   config.ScoringStrategyType `json:"type"`
	Weight int                        `json:"weight"`
}

func (w *ResourceStrategyFit) String() string {
	marshal, err := json.Marshal(w)
	if err != nil {
		klog.Errorf("Failed to marshal ResourceStrategyFit: %v", err)
		return ""
	}
	return string(marshal)
}

type sraConfig struct {
	Enable         bool           `json:"enable"`
	Resources      string         `json:"resources"`
	Weight         int            `json:"weight"`
	ResourceWeight map[string]int `json:"resourceWeight"`
}

type proportionalConfig struct {
	Enable             bool               `json:"enable"`
	Resources          string             `json:"resources"`
	ResourceProportion map[string]float64 `json:"resourceProportion"`
}

type resourceStrategyFitPlugin struct {
	// Arguments given for the plugin
	ResourceStrategyFit ResourceStrategyFit
	Sra                 sraWeight
	Proportional        map[v1.ResourceName]baseResource
}

// New function returns prioritizePlugin object
func New(arguments framework.Arguments) framework.Plugin {
	weight := calculateWeight(arguments)
	rsf := resourceStrategyFitPlugin{ResourceStrategyFit: weight}
	applySraPolicy(&rsf, arguments)
	applyProportionalPolicy(&rsf, arguments)

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
		         enable: true
		         resources: nvidia.com/gpu
		         weight: 10
		         resourceWeight:
		           nvidia.com/gpu: 1
		       proportional:
		         enable: false
		         resources: nvidia.com/gpu
		         resourceProportion:
		           nvidia.com/gpu.cpu: 4
		           nvidia.com/gpu.memory: 8
	*/

	var weight ResourceStrategyFit

	resourceStrategyFitPluginWeight, b := framework.Get[int](args, "resourceStrategyFitWeight")
	if !b || resourceStrategyFitPluginWeight < 0 {
		resourceStrategyFitPluginWeight = DefaultResourceStrategyFitPluginWeight
	}
	weight.Weight = resourceStrategyFitPluginWeight

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
		configKeyStr := string(k)

		if !isValidWildcardPattern(configKeyStr) {
			if configKeyStr == "*" {
				klog.Warningf("Single asterisk '*' is not supported as default strategy. Please use specific resource patterns like 'cloudml.gpu/*'. Pattern ignored.")
			} else {
				klog.Warningf("Invalid wildcard pattern '%s': only single trailing asterisk is supported (e.g., 'cloudml.gpu/*'). Pattern ignored.", configKeyStr)
			}
			delete(resources, k)
			continue
		}
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

func applyProportionalPolicy(rsf *resourceStrategyFitPlugin, args framework.Arguments) {
	cfg, _ := framework.Get[proportionalConfig](args, "proportional")
	if !cfg.Enable {
		return
	}

	klog.V(4).Infof("proportional resources: %v is provided", cfg.Resources)
	// Obtain proportional resource key name
	resources := strings.Split(cfg.Resources, ",")
	rsf.Proportional = calculateProportionalResources(resources, cfg)
}

func applySraPolicy(rsf *resourceStrategyFitPlugin, args framework.Arguments) {
	cfg, _ := framework.Get[sraConfig](args, "sra")
	if !cfg.Enable {
		return
	}

	klog.V(4).Infof("sra resources: %v is provided", cfg.Resources)
	// Obtain sra resource key name
	resources := strings.Split(cfg.Resources, ",")
	rsf.Sra = calculateSraWeight(resources, cfg)
}

func (rsf *resourceStrategyFitPlugin) Name() string {
	return PluginName
}

func (rsf *resourceStrategyFitPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter resourceStrategyFit plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving resourceStrategyFit plugin. %s ...", rsf.ResourceStrategyFit.String())
	}()

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		predicateStatus := make([]*api.Status, 0)

		// Check ProportionalPredicate
		proportionalStatus, _ := checkNodeResourceIsProportional(task, node, rsf.Proportional)
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

		if rsf.ResourceStrategyFit.Weight > 0 {
			score += Score(task, node, rsf.ResourceStrategyFit)
			klog.V(4).Infof("resourceStrategyFit policy score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		}

		if rsf.Sra.Weight > 0 {
			klog.V(5).Infof("sra weight is %d", rsf.Sra.Weight)
			if klog.V(4).Enabled() {
				var notFoundResource []string
				for resource := range rsf.Sra.Resources {
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

			rs := sraScore(task, node, rsf.Sra)
			klog.V(4).Infof("sra score for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, rs)
			score += rs
		}

		klog.V(4).Infof("resource-strategy-fit plugin total score (sra + resourceStrategyFit) for Task %s/%s on node %s is: %v", task.Namespace, task.Name, node.Name, score)
		return score, nil
	}

	if rsf.Proportional != nil {
		ssn.AddPredicateFn(rsf.Name(), predicateFn)
	} else {
		klog.V(4).Infof("proportional policy is not enabled, skip predicate function")
	}

	if rsf.ResourceStrategyFit.Weight <= 0 && rsf.Sra.Weight <= 0 {
		klog.V(4).Infof("The weights of both resourceStrategyFit and sra are zero, skip node order function")
	} else {
		ssn.AddNodeOrderFn(rsf.Name(), nodeOrderFn)
	}
}

func isValidWildcardPattern(pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return true
	}
	if pattern == "*" {
		return false
	}

	if !strings.HasSuffix(pattern, "*") {
		return false
	}

	return strings.Count(pattern, "*") == 1
}

func findResourceConfigWithPrefix(resourceName string, resources map[v1.ResourceName]ResourcesType) (ResourcesType, bool) {
	// Check for exact match first - exact match takes precedence over wildcard patterns
	if config, exists := resources[v1.ResourceName(resourceName)]; exists {
		return config, true
	}
	// If no exact match found, search for wildcard patterns
	var bestMatch string
	var bestConfig ResourcesType
	var found bool

	for configKey, config := range resources {
		configKeyStr := string(configKey)
		// All wildcard patterns have been validated by isValidWildcardPattern
		if strings.Contains(configKeyStr, "*") {
			prefix := strings.TrimSuffix(configKeyStr, "*")
			if strings.HasPrefix(resourceName, prefix) {
				// When multiple wildcard patterns match the same resource, choose the one with the longest prefix
				// to ensure the most specific configuration takes precedence.
				// Example: for resource "nvidia.com/gpu", if both "nvidia.com/*" and "nvidia.*" patterns exist,
				// "nvidia.com/*" (longer prefix) will be selected over "nvidia.*" (shorter prefix)
				if len(prefix) > len(bestMatch) {
					bestMatch = prefix
					bestConfig = config
					found = true
				}
			}
		}
	}

	return bestConfig, found
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
		resourceConfig, found := findResourceConfigWithPrefix(string(resource), weight.Resources)
		if !found {
			continue
		}

		scoringType := resourceConfig.Type
		resourceWeight := resourceConfig.Weight

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
	score *= float64(k8sFramework.MaxNodeScore) * float64(weight.Weight)
	return score
}

// validateAndCalcUsedFinally returns the used finally, if usedFinally > capacity, return error
func validateAndCalcUsedFinally(requested float64, used float64, capacity float64, weight int) (float64, error) {
	usedFinally := requested + used
	if usedFinally > capacity {
		return 0, fmt.Errorf("node resources are not enough")
	}
	return usedFinally, nil
}

func mostRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if weight == 0 || capacity == 0 {
		return 0, nil
	}

	usedFinally, err := validateAndCalcUsedFinally(requested, used, capacity, weight)
	if err != nil {
		return 0, err
	}

	score := usedFinally * float64(weight) / capacity
	return score, nil
}

func leastRequestedScore(requested float64, used float64, capacity float64, weight int) (float64, error) {
	if weight == 0 || capacity == 0 {
		return 0, nil
	}

	usedFinally, err := validateAndCalcUsedFinally(requested, used, capacity, weight)
	if err != nil {
		return 0, err
	}

	score := (capacity - usedFinally) * float64(weight) / capacity
	return score, nil
}

func (rsf *resourceStrategyFitPlugin) OnSessionClose(ssn *framework.Session) {
}
