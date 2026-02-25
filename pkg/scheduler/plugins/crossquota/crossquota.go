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

package crossquota

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "crossquota"

	// GPUResourceNames is the key for GPU resource names configuration (regex list, comma-separated)
	GPUResourceNames = "gpu-resource-names"

	// ResourcesList is the key for the list of resource names we want to quota (comma-separated)
	ResourcesList = "quota-resources"

	// QuotaPrefix is the key prefix for absolute quota of a resource, e.g. quota.cpu: "2", quota.memory: "4Gi"
	QuotaPrefix = "quota."

	// QuotaPercentagePrefix is the key prefix for percentage quota of a resource, e.g. quota-percentage.memory: "50"
	QuotaPercentagePrefix = "quota-percentage."

	// Annotations follow highest priority:
	// volcano.sh/crossquota-<resName>: absolute quantity, e.g. "2", "4Gi"
	// volcano.sh/crossquota-percentage-<resName>: percentage in [1,100]
	AnnQuotaPrefix           = "volcano.sh/crossquota-"
	AnnQuotaPercentagePrefix = "volcano.sh/crossquota-percentage-"

	// CrossQuotaWeight is the key for the plugin weight configuration
	CrossQuotaWeight = "crossQuotaWeight"

	// WeightPrefix is the key prefix for resource weight configuration, e.g. weight.cpu: "10", weight.memory: "1"
	WeightPrefix = "weight."

	// DefaultCrossQuotaWeight is the default weight for crossquota plugin
	DefaultCrossQuotaWeight = 10

	// Default resource weights for scoring
	DefaultCPUWeight    = 10
	DefaultMemoryWeight = 1

	// ScoringStrategyAnnotation is the annotation key for scoring strategy
	ScoringStrategyAnnotation = "volcano.sh/crossquota-scoring-strategy"

	// ScoringStrategyMostAllocated indicates most allocated strategy
	ScoringStrategyMostAllocated = "most-allocated"

	// ScoringStrategyLeastAllocated indicates least allocated strategy
	ScoringStrategyLeastAllocated = "least-allocated"

	// LabelSelector is the key for pod label selector configuration
	LabelSelector = "label-selector"

	minMilliScalarResources float64 = 10
)

type ScoringStrategy string

type crossQuotaPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	// GPU resource names patterns (support regex)
	gpuResourcePatterns []*regexp.Regexp

	// The resource names that should be quota-controlled
	quotaResourceNames []corev1.ResourceName

	// Pre-parsed quota configurations from plugin arguments
	// Key: resource name, Value: parsed quota value
	parsedQuotas map[corev1.ResourceName]float64
	// Key: resource name, Value: percentage value (0-100)
	parsedQuotaPercentages map[corev1.ResourceName]float64

	// Track allocatable resources for each GPU node
	NodeQuotas map[string]*api.Resource

	// Plugin weight for node ordering
	pluginWeight int

	// Resource weights for scoring
	resourceWeights map[corev1.ResourceName]int

	// Label selector for filtering pods
	labelSelector labels.Selector
}

// New returns crossquota plugin
func New(arguments framework.Arguments) framework.Plugin {
	cq := &crossQuotaPlugin{
		pluginArguments:        arguments,
		parsedQuotas:           make(map[corev1.ResourceName]float64),
		parsedQuotaPercentages: make(map[corev1.ResourceName]float64),
		NodeQuotas:             make(map[string]*api.Resource),
		resourceWeights:        make(map[corev1.ResourceName]int),
	}
	cq.parseArguments()
	cq.parseWeightArguments()
	return cq
}

func (cq *crossQuotaPlugin) Name() string {
	return PluginName
}

// parseArguments parses the plugin arguments
func (cq *crossQuotaPlugin) parseArguments() {
	// Parse GPU resource name regex patterns (to detect GPU nodes and GPU pods)
	if gpuResourceNames, ok := cq.pluginArguments[GPUResourceNames].(string); ok && strings.TrimSpace(gpuResourceNames) != "" {
		for _, p := range strings.Split(gpuResourceNames, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			re, err := regexp.Compile(p)
			if err != nil {
				klog.Warningf("crossquota: invalid GPU resource pattern %q: %v", p, err)
				continue
			}
			cq.gpuResourcePatterns = append(cq.gpuResourcePatterns, re)
		}
	}

	// Parse resource names to be quota-controlled
	if resList, ok := cq.pluginArguments[ResourcesList].(string); ok && strings.TrimSpace(resList) != "" {
		for _, r := range strings.Split(resList, ",") {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			cq.quotaResourceNames = append(cq.quotaResourceNames, corev1.ResourceName(r))
		}
	} else {
		// default to CPU only to be backward-safe
		cq.quotaResourceNames = []corev1.ResourceName{corev1.ResourceCPU}
	}

	// Pre-parse quota configurations from plugin arguments
	cq.parseQuotaConfigurations()

	// Parse label selector configuration
	cq.parseLabelSelector()

	klog.V(3).Infof("crossquota initialized. GPUPatterns=%v, quotaResources=%v, labelSelector=%v", cq.gpuResourcePatterns, cq.quotaResourceNames, cq.labelSelector)
}

// parseQuotaConfigurations pre-parses quota configurations from plugin arguments
func (cq *crossQuotaPlugin) parseQuotaConfigurations() {
	// Parse absolute quota configurations
	for key, value := range cq.pluginArguments {
		if strings.HasPrefix(key, QuotaPrefix) {
			resourceName := corev1.ResourceName(strings.TrimPrefix(key, QuotaPrefix))
			if quotaStr, ok := value.(string); ok && strings.TrimSpace(quotaStr) != "" {
				quota, err := parseAbsoluteQuota(quotaStr, resourceName, key)
				if err != nil {
					klog.Warningf("crossquota: invalid quota configuration %q: %v", key, err)
					continue
				}
				cq.parsedQuotas[resourceName] = quota
				klog.V(4).Infof("crossquota: parsed quota %s = %f", resourceName, quota)
			}
		}
	}

	// Parse percentage quota configurations
	for key, value := range cq.pluginArguments {
		if strings.HasPrefix(key, QuotaPercentagePrefix) {
			resourceName := corev1.ResourceName(strings.TrimPrefix(key, QuotaPercentagePrefix))
			if percentageStr, ok := value.(string); ok && strings.TrimSpace(percentageStr) != "" {
				percentage, err := strconv.ParseFloat(percentageStr, 64)
				if err != nil {
					klog.Warningf("crossquota: invalid percentage configuration %q: %v", key, err)
					continue
				}
				if percentage <= 0 || percentage > 100 {
					klog.Warningf("crossquota: percentage configuration %q must be in (0,100], got %f", key, percentage)
					continue
				}
				cq.parsedQuotaPercentages[resourceName] = percentage
				klog.V(4).Infof("crossquota: parsed percentage %s = %f%%", resourceName, percentage)
			}
		}
	}
}

// parseWeightArguments parses weight configurations from plugin arguments
func (cq *crossQuotaPlugin) parseWeightArguments() {
	// Initialize with default weight
	cq.pluginWeight = DefaultCrossQuotaWeight

	// Initialize default resource weights
	cq.resourceWeights[corev1.ResourceCPU] = DefaultCPUWeight
	cq.resourceWeights[corev1.ResourceMemory] = DefaultMemoryWeight

	// Parse plugin weight
	if weight, ok := cq.pluginArguments[CrossQuotaWeight]; ok {
		if weightInt, ok := weight.(int); ok {
			cq.pluginWeight = weightInt
		} else if weightFloat, ok := weight.(float64); ok {
			cq.pluginWeight = int(weightFloat)
		}
	}

	// Parse resource weights from arguments with "weight." prefix
	// Format: "weight.cpu": 10, "weight.memory": 1, etc.
	for key, value := range cq.pluginArguments {
		if strings.HasPrefix(key, WeightPrefix) {
			resourceName := corev1.ResourceName(strings.TrimPrefix(key, WeightPrefix))

			// Try to parse weight value as int or float
			if weightInt, ok := value.(int); ok {
				cq.resourceWeights[resourceName] = weightInt
				klog.V(4).Infof("crossquota: parsed resource weight %s = %d", resourceName, weightInt)
			} else if weightFloat, ok := value.(float64); ok {
				weightIntValue := int(weightFloat)
				cq.resourceWeights[resourceName] = weightIntValue
				klog.V(4).Infof("crossquota: parsed resource weight %s = %d", resourceName, weightIntValue)
			} else if weightStr, ok := value.(string); ok {
				// Try to parse string as int
				if weightIntValue, err := strconv.Atoi(weightStr); err == nil {
					cq.resourceWeights[resourceName] = weightIntValue
					klog.V(4).Infof("crossquota: parsed resource weight %s = %d", resourceName, weightIntValue)
				} else {
					klog.Warningf("crossquota: invalid weight configuration %q: cannot parse as int", key)
				}
			}
		}
	}

	klog.V(3).Infof("crossquota: plugin weight=%d, resource weights=%v", cq.pluginWeight, cq.resourceWeights)
}

// parseLabelSelector parses the label selector configuration from plugin arguments
// It supports structured format with matchLabels and matchExpressions
func (cq *crossQuotaPlugin) parseLabelSelector() {
	// Default to nil selector (matches everything)
	cq.labelSelector = labels.Everything()

	selectorConfig, exists := cq.pluginArguments[LabelSelector]
	if !exists {
		return
	}

	// Parse as structured map (format with matchLabels and matchExpressions)
	selectorMap, ok := selectorConfig.(map[string]interface{})
	if !ok {
		klog.Warningf("crossquota: label selector must be a map, got %T, will match all pods", selectorConfig)
		return
	}

	labelSelector, err := cq.parseLabelSelectorFromMap(selectorMap)
	if err != nil {
		klog.Warningf("crossquota: invalid label selector map: %v, will match all pods", err)
		return
	}

	// Convert to labels.Selector
	if labelSelector != nil {
		parsedSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			klog.Warningf("crossquota: failed to convert label selector: %v, will match all pods", err)
			return
		}

		cq.labelSelector = parsedSelector
		klog.V(4).Infof("crossquota: parsed label selector: %s", parsedSelector.String())
	}
}

// parseLabelSelectorFromMap parses a structured label selector map
// Supports matchLabels and matchExpressions
func (cq *crossQuotaPlugin) parseLabelSelectorFromMap(selectorMap map[string]interface{}) (*metav1.LabelSelector, error) {
	// Convert map to JSON and then unmarshal to LabelSelector
	jsonBytes, err := json.Marshal(selectorMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal selector map: %v", err)
	}

	var labelSelector metav1.LabelSelector
	if err := json.Unmarshal(jsonBytes, &labelSelector); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to LabelSelector: %v", err)
	}

	return &labelSelector, nil
}

// matchesLabelSelector checks if a task's pod matches the configured label selector
func (cq *crossQuotaPlugin) matchesLabelSelector(task *api.TaskInfo) bool {
	// If no label selector is configured, match everything
	if cq.labelSelector == nil || cq.labelSelector.Empty() {
		return true
	}

	// If task has no pod, don't match
	if task.Pod == nil {
		return false
	}

	// Check if pod labels match the selector
	return cq.labelSelector.Matches(labels.Set(task.Pod.Labels))
}

// calculateCurrentUsage calculates current CPU pod resource usage on a node
// Only counts tasks that are CPU pods, allocated, and match the label selector
func (cq *crossQuotaPlugin) calculateCurrentUsage(node *api.NodeInfo) *api.Resource {
	currentUsage := &api.Resource{}
	for _, existingTask := range node.Tasks {
		if cq.isCPUPod(existingTask) && api.AllocatedStatus(existingTask.Status) && cq.matchesLabelSelector(existingTask) {
			// Add this task's resource usage to current usage
			for _, rn := range cq.quotaResourceNames {
				req := getTaskRequest(existingTask, rn)
				switch rn {
				case corev1.ResourceCPU:
					currentUsage.MilliCPU += req
				case corev1.ResourceMemory:
					currentUsage.Memory += req
				default:
					if currentUsage.ScalarResources == nil {
						currentUsage.ScalarResources = make(map[corev1.ResourceName]float64)
					}
					currentUsage.ScalarResources[rn] += req
				}
			}
		}
	}
	return currentUsage
}

// -------- Node & Task classification --------
// isGPUNode checks if a node is a GPU node based on its allocatable resources
func (cq *crossQuotaPlugin) isGPUNode(node *api.NodeInfo) bool {
	if len(cq.gpuResourcePatterns) == 0 {
		return false
	}
	for resName, quantity := range node.Node.Status.Allocatable {
		for _, pattern := range cq.gpuResourcePatterns {
			if pattern.MatchString(string(resName)) {
				// If GPU resource exists and quantity > 0, it's a GPU node
				if quantity.Cmp(resource.MustParse("0")) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// isCPUPod checks if a task is a CPU pod (does not request any GPU resources matched by patterns)
func (cq *crossQuotaPlugin) isCPUPod(task *api.TaskInfo) bool {
	if len(cq.gpuResourcePatterns) == 0 {
		// If no GPU patterns configured, we consider nothing as CPU-only for the purpose of this plugin.
		return false
	}
	for resName, value := range task.Resreq.ScalarResources {
		for _, pattern := range cq.gpuResourcePatterns {
			if pattern.MatchString(string(resName)) && value > minMilliScalarResources {
				return false
			}
		}
	}
	return true
}

// -------- Resource helpers (get/set allocatable & used/required) --------

func getNodeAllocatable(node *api.Resource, rn corev1.ResourceName) float64 {
	switch rn {
	case corev1.ResourceCPU:
		return node.MilliCPU
	case corev1.ResourceMemory:
		return node.Memory
	default:
		if node.ScalarResources == nil {
			return 0
		}
		return node.ScalarResources[rn]
	}
}

func setNodeAllocatable(node *api.Resource, rn corev1.ResourceName, v float64) {
	switch rn {
	case corev1.ResourceCPU:
		node.MilliCPU = v
	case corev1.ResourceMemory:
		node.Memory = v
	default:
		if node.ScalarResources == nil {
			node.ScalarResources = map[corev1.ResourceName]float64{}
		}
		node.ScalarResources[rn] = v
	}
}

func getTaskRequest(task *api.TaskInfo, rn corev1.ResourceName) float64 {
	if task.Resreq == nil {
		return 0
	}
	switch rn {
	case corev1.ResourceCPU:
		return task.Resreq.MilliCPU
	case corev1.ResourceMemory:
		return task.Resreq.Memory
	default:
		if task.Resreq.ScalarResources == nil {
			return 0
		}
		return task.Resreq.ScalarResources[rn]
	}
}

func getNodeUsed(node *api.Resource, rn corev1.ResourceName) float64 {
	switch rn {
	case corev1.ResourceCPU:
		return node.MilliCPU
	case corev1.ResourceMemory:
		return node.Memory
	default:
		if node.ScalarResources == nil {
			return 0
		}
		return node.ScalarResources[rn]
	}
}

// -------- Quota evaluation helpers --------

// parseAbsoluteQuota parses an absolute quota value and converts it to the appropriate unit
func parseAbsoluteQuota(quotaStr string, rn corev1.ResourceName, source string) (float64, error) {
	q, err := resource.ParseQuantity(quotaStr)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %v", source, quotaStr, err)
	}

	// CPU and scalar resources use MilliValue, memory and pods use Value
	if rn == corev1.ResourceCPU {
		return float64(q.MilliValue()), nil
	}
	if rn == corev1.ResourceMemory || rn == corev1.ResourcePods {
		return float64(q.Value()), nil
	}
	// All other scalar resources (including ephemeral-storage) use MilliValue
	return float64(q.MilliValue()), nil
}

// parsePercentageQuota parses a percentage quota value and calculates the actual quota
func parsePercentageQuota(percentageStr string, allocatable float64, source string) (float64, error) {
	pct, err := strconv.ParseFloat(percentageStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %v", source, percentageStr, err)
	}
	if pct <= 0 || pct > 100 {
		return 0, fmt.Errorf("%s must be in (0,100], got %f", source, pct)
	}
	return allocatable * pct / 100.0, nil
}

// -------- Quota evaluation (annotation > args > full allocatable) --------

func (cq *crossQuotaPlugin) calculateQuota(node *api.NodeInfo, rn corev1.ResourceName) (float64, error) {
	name := string(rn)

	// 1) Node annotation: absolute quantity
	if qs, ok := node.Node.Annotations[AnnQuotaPrefix+name]; ok && strings.TrimSpace(qs) != "" {
		return parseAbsoluteQuota(qs, rn, AnnQuotaPrefix+name)
	}

	// 2) Node annotation: percentage
	if ps, ok := node.Node.Annotations[AnnQuotaPercentagePrefix+name]; ok && strings.TrimSpace(ps) != "" {
		alloc := getNodeAllocatable(node.Allocatable, rn)
		return parsePercentageQuota(ps, alloc, AnnQuotaPercentagePrefix+name)
	}

	// 3) Plugin args: absolute quantity (pre-parsed)
	if quota, exists := cq.parsedQuotas[rn]; exists {
		return quota, nil
	}

	// 4) Plugin args: percentage (pre-parsed)
	if percentage, exists := cq.parsedQuotaPercentages[rn]; exists {
		alloc := getNodeAllocatable(node.Allocatable, rn)
		return alloc * percentage / 100.0, nil
	}

	// 5) Default: full allocatable
	return getNodeAllocatable(node.Allocatable, rn), nil
}

// -------- Session lifecycle --------

func (cq *crossQuotaPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter crossquota plugin %v ...", cq.Name())
	defer func() {
		klog.V(4).Infof("Leaving crossquota plugin. weight: %v ...", cq.pluginWeight)
	}()

	for _, node := range ssn.Nodes {
		// Only apply on GPU nodes
		if !cq.isGPUNode(node) {
			continue
		}

		// Create quota resource for this GPU node
		quotaResource := api.EmptyResource()
		for _, rn := range cq.quotaResourceNames {
			q, err := cq.calculateQuota(node, rn)
			if err != nil {
				klog.Warningf("crossquota: node %s resource %s quota calc failed: %v", node.Name, rn, err)
				continue
			}
			setNodeAllocatable(quotaResource, rn, q)
			klog.V(4).Infof("crossquota: node %s quota %s set to %.0f (original %.0f)",
				node.Name, rn, q, getNodeAllocatable(node.Allocatable, rn))
		}
		cq.NodeQuotas[node.Name] = quotaResource
	}

	// Predicate: block CPU pods that would exceed any configured resource quota on GPU nodes
	ssn.AddPredicateFn(cq.Name(), cq.predicateFn)

	// Node ordering: score CPU pods on GPU nodes based on resource utilization
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		score := cq.calculateScore(task, node)
		klog.V(4).Infof("Crossquota score for Task %s/%s on node %s is: %v",
			task.Namespace, task.Name, node.Name, score)
		return score, nil
	}

	if cq.pluginWeight != 0 {
		ssn.AddNodeOrderFn(cq.Name(), nodeOrderFn)
	} else {
		klog.Infof("Crossquota weight is zero, skip node order function")
	}
}

// predicateFn checks if a task can be scheduled on a node based on resource quotas
func (cq *crossQuotaPlugin) predicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
	// Only control CPU pods
	if !cq.isCPUPod(task) {
		return nil
	}
	// Only on GPU nodes
	if !cq.isGPUNode(node) {
		return nil
	}
	// Only control pods that match the label selector
	if !cq.matchesLabelSelector(task) {
		return nil
	}
	// Must be tracked
	quotaResource, ok := cq.NodeQuotas[node.Name]
	if !ok {
		return api.NewFitError(task, node, "node not tracked by crossquota plugin")
	}

	// Calculate current CPU pod resource usage on this node
	currentUsage := cq.calculateCurrentUsage(node)

	// Check all configured resources
	for _, rn := range cq.quotaResourceNames {
		req := getTaskRequest(task, rn)
		used := getNodeUsed(currentUsage, rn)
		quota := getNodeAllocatable(quotaResource, rn)
		if used+req > quota {
			return api.NewFitError(task, node, fmt.Sprintf(
				"%s quota exceeded: required=%.0f used=%.0f allocatable=%.0f", rn, req, used, quota))
		}
	}
	return nil
}

func (cq *crossQuotaPlugin) OnSessionClose(ssn *framework.Session) {
	cq.NodeQuotas = make(map[string]*api.Resource)
}

// -------- Node ordering / scoring --------

// calculateScore calculates the score for a task on a specific node
func (cq *crossQuotaPlugin) calculateScore(task *api.TaskInfo, node *api.NodeInfo) float64 {
	// Only control CPU pods
	if !cq.isCPUPod(task) {
		return 0
	}
	// Only on GPU nodes
	if !cq.isGPUNode(node) {
		return 0
	}

	// Get quota resource for this node
	quotaResource, ok := cq.NodeQuotas[node.Name]
	if !ok {
		klog.V(4).Infof("Task %s/%s: node %s not tracked by crossquota, score=0",
			task.Namespace, task.Name, node.Name)
		return 0
	}

	// Get scoring strategy from Pod annotation
	strategy := cq.getScoringStrategy(task)

	score := 0.0
	totalWeight := 0

	// Calculate current CPU pod resource usage on this node
	currentUsage := cq.calculateCurrentUsage(node)

	// Calculate weighted score for each quota resource
	for _, resource := range cq.quotaResourceNames {
		request := getTaskRequest(task, resource)
		if request <= 0 {
			continue
		}

		total := getNodeAllocatable(quotaResource, resource)
		nodeUsed := getNodeUsed(currentUsage, resource)

		// Get resource weight, default to 1 if not configured
		resourceWeight := 1
		if weight, exists := cq.resourceWeights[resource]; exists {
			resourceWeight = weight
		}

		// Calculate the resource score based on the specified strategy
		resourceScore, err := cq.calculateResourceScore(strategy, request, nodeUsed, total)
		if err != nil {
			klog.V(4).Infof("Task %s/%s cannot calculate crossquota score on node %s: resource: %s, error: %s, need %f, used %f, total %f",
				task.Namespace, task.Name, node.Name, resource, err.Error(), request, nodeUsed, total)
			return 0
		}

		klog.V(5).Infof("Task %s/%s on node %s resource %s, strategy: %s, weight: %d, need %f, used %f, total %f, score %f",
			task.Namespace, task.Name, node.Name, resource, strategy, resourceWeight, request, nodeUsed, total, resourceScore)

		// Apply resource weight to score
		score += resourceScore * float64(resourceWeight)
		totalWeight += resourceWeight
	}

	// Normalize score by total weight if there are weighted resources
	if totalWeight > 0 {
		score = score / float64(totalWeight)
	}

	// Apply plugin weight
	score *= float64(cq.pluginWeight)
	return score
}

// getScoringStrategy gets the scoring strategy from Pod annotation
func (cq *crossQuotaPlugin) getScoringStrategy(task *api.TaskInfo) ScoringStrategy {
	if task.Pod == nil || task.Pod.Annotations == nil {
		return ScoringStrategyMostAllocated // Default strategy
	}

	strategy, exists := task.Pod.Annotations[ScoringStrategyAnnotation]
	if !exists {
		return ScoringStrategyMostAllocated // Default strategy
	}

	// Validate strategy
	switch ScoringStrategy(strategy) {
	case ScoringStrategyMostAllocated, ScoringStrategyLeastAllocated:
		return ScoringStrategy(strategy)
	default:
		klog.V(3).Infof("Invalid scoring strategy '%s' for task %s/%s, using default",
			strategy, task.Namespace, task.Name)
		return ScoringStrategyMostAllocated
	}
}

// calculateResourceScore determines the score based on the specified strategy.
// It checks if the requested resources will exceed the node's quota capacity.
func (cq *crossQuotaPlugin) calculateResourceScore(
	strategy ScoringStrategy,
	requested float64,
	used float64,
	total float64,
) (float64, error) {
	if total == 0 {
		return 0, nil
	}

	usedFinally := requested + used
	// Check if the requested resources will fit within quota
	if usedFinally > total {
		return 0, fmt.Errorf("node resources are not enough")
	}

	var score float64
	if strategy == ScoringStrategyMostAllocated {
		// Score formula: (used + requested) / total
		// A higher score indicates higher resource utilization.
		score = usedFinally / total
	} else {
		// Score formula: (total - used - requested) / total
		// A higher score indicates lower resource utilization.
		score = (total - usedFinally) / total
	}
	return score, nil
}
