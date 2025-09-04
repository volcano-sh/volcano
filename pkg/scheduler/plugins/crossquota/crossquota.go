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

package crossquota

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
)

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

	// Track CPU-pod resource usage on each GPU node (cloned NodeInfo with modified allocatables per quota)
	Nodes map[string]*api.NodeInfo
}

// New returns crossquota plugin
func New(arguments framework.Arguments) framework.Plugin {
	cq := &crossQuotaPlugin{
		pluginArguments:        arguments,
		parsedQuotas:           make(map[corev1.ResourceName]float64),
		parsedQuotaPercentages: make(map[corev1.ResourceName]float64),
		Nodes:                  make(map[string]*api.NodeInfo),
	}
	cq.parseArguments()
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

	klog.V(3).Infof("crossquota initialized. GPUPatterns=%v, quotaResources=%v", cq.gpuResourcePatterns, cq.quotaResourceNames)
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

// -------- Node & Task classification --------

// isGPUNode checks if a node is a GPU node based on its allocatable resources
func (cq *crossQuotaPlugin) isGPUNode(node *api.NodeInfo) bool {
	if len(cq.gpuResourcePatterns) == 0 {
		return false
	}
	for resName := range node.Node.Status.Allocatable {
		for _, pattern := range cq.gpuResourcePatterns {
			if pattern.MatchString(string(resName)) {
				return true
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
	for resName := range task.Resreq.ScalarResources {
		for _, pattern := range cq.gpuResourcePatterns {
			if pattern.MatchString(string(resName)) {
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
	for _, node := range ssn.Nodes {
		// Only apply on GPU nodes
		if !cq.isGPUNode(node) {
			continue
		}

		// Clone and apply quotas per configured resource
		newNodeInfo := node.Clone()
		for _, rn := range cq.quotaResourceNames {
			q, err := cq.calculateQuota(node, rn)
			if err != nil {
				klog.Warningf("crossquota: node %s resource %s quota calc failed: %v", node.Name, rn, err)
				continue
			}
			setNodeAllocatable(newNodeInfo.Allocatable, rn, q)
			klog.V(4).Infof("crossquota: node %s quota %s set to %.0f (original %.0f)",
				node.Name, rn, q, getNodeAllocatable(node.Allocatable, rn))
		}
		cq.Nodes[node.Name] = newNodeInfo

		// Seed existing CPU tasks into tracking
		for _, task := range node.Tasks {
			if cq.isCPUPod(task) {
				cq.Nodes[node.Name].AddTask(task)
			}
		}
	}

	// Predicate: block CPU pods that would exceed any configured resource quota on GPU nodes
	ssn.AddPredicateFn(cq.Name(), cq.predicateFn)

	// Event handlers to track usage
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc:   cq.allocateFunc,
		DeallocateFunc: cq.deallocateFunc,
	})
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
	// Must be tracked
	tracked, ok := cq.Nodes[node.Name]
	if !ok {
		return nil
	}

	// Check all configured resources
	for _, rn := range cq.quotaResourceNames {
		req := getTaskRequest(task, rn)
		used := getNodeUsed(tracked.Used, rn)
		alloc := getNodeAllocatable(tracked.Allocatable, rn)
		if used+req > alloc {
			return api.NewFitError(task, node, fmt.Sprintf(
				"%s quota exceeded: required=%.0f used=%.0f allocatable=%.0f", rn, req, used, alloc))
		}
	}
	return nil
}

// allocateFunc handles task allocation events
func (cq *crossQuotaPlugin) allocateFunc(e *framework.Event) {
	if !cq.isCPUPod(e.Task) {
		return
	}
	tracked, ok := cq.Nodes[e.Task.NodeName]
	if !ok {
		return
	}
	tracked.AddTask(e.Task)
	if klog.V(5).Enabled() {
		var parts []string
		for _, rn := range cq.quotaResourceNames {
			parts = append(parts, fmt.Sprintf("%s=%.0f/%.0f", rn,
				getNodeUsed(tracked.Used, rn), getNodeAllocatable(tracked.Allocatable, rn)))
		}
		klog.Infof("crossquota: task %s allocated on %s, usage {%s}", e.Task.Name, e.Task.NodeName, strings.Join(parts, " "))
	}
}

// deallocateFunc handles task deallocation events
func (cq *crossQuotaPlugin) deallocateFunc(e *framework.Event) {
	if !cq.isCPUPod(e.Task) {
		return
	}
	tracked, ok := cq.Nodes[e.Task.NodeName]
	if !ok {
		return
	}
	tracked.RemoveTask(e.Task)
	if klog.V(5).Enabled() {
		var parts []string
		for _, rn := range cq.quotaResourceNames {
			parts = append(parts, fmt.Sprintf("%s=%.0f/%.0f", rn,
				getNodeUsed(tracked.Used, rn), getNodeAllocatable(tracked.Allocatable, rn)))
		}
		klog.Infof("crossquota: task %s deallocated on %s, usage {%s}", e.Task.Name, e.Task.NodeName, strings.Join(parts, " "))
	}
}

func (cq *crossQuotaPlugin) OnSessionClose(ssn *framework.Session) {
	cq.Nodes = make(map[string]*api.NodeInfo)
}
