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

package cpuquota

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "cpuquota"

	// GPUResourceNames is the key for GPU resource names configuration
	GPUResourceNames = "cpuquota.gpu-resource-names"

	// CPUQuota is the key for CPU quota configuration (specific amount)
	CPUQuota = "cpuquota.cpu-quota"

	// CPUQuotaPercentage is the key for CPU quota configuration (percentage)
	CPUQuotaPercentage = "cpuquota.cpu-quota-percentage"

	// NodeAnnotationCPUQuota is the annotation key for node-specific CPU quota
	NodeAnnotationCPUQuota = "volcano.sh/cpu-quota"

	// NodeAnnotationCPUQuotaPercentage is the annotation key for node-specific CPU quota percentage
	NodeAnnotationCPUQuotaPercentage = "volcano.sh/cpu-quota-percentage"
)

type cpuQuotaPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	// GPU resource names patterns (support regex)
	gpuResourcePatterns []*regexp.Regexp

	// Global CPU quota configuration
	cpuQuota           float64
	cpuQuotaPercentage float64

	// Track CPU pod resource usage on each GPU node
	Nodes map[string]*api.NodeInfo
}

// New return cpuquota plugin
func New(arguments framework.Arguments) framework.Plugin {
	cp := &cpuQuotaPlugin{
		pluginArguments: arguments,
		Nodes:           make(map[string]*api.NodeInfo),
	}

	cp.parseArguments()
	return cp
}

func (cp *cpuQuotaPlugin) Name() string {
	return PluginName
}

// parseArguments parses the plugin arguments
func (cp *cpuQuotaPlugin) parseArguments() {
	// Parse GPU resource names
	if gpuResourceNames, ok := cp.pluginArguments[GPUResourceNames].(string); ok && gpuResourceNames != "" {
		patterns := strings.Split(gpuResourceNames, ",")
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				regex, err := regexp.Compile(pattern)
				if err != nil {
					klog.Warningf("Invalid GPU resource pattern %s: %v", pattern, err)
					continue
				}
				cp.gpuResourcePatterns = append(cp.gpuResourcePatterns, regex)
			}
		}
	}

	// Parse CPU quota configuration
	cp.pluginArguments.GetFloat64(&cp.cpuQuota, CPUQuota)
	cp.pluginArguments.GetFloat64(&cp.cpuQuotaPercentage, CPUQuotaPercentage)

	// If both quota and percentage are specified, quota takes precedence
	if cp.cpuQuota > 0 {
		cp.cpuQuotaPercentage = 0
	}

	klog.V(4).Infof("CPUQuota plugin initialized with GPU patterns: %v, CPU quota: %f, CPU quota percentage: %f",
		cp.gpuResourcePatterns, cp.cpuQuota, cp.cpuQuotaPercentage)
}

// isGPUNode checks if a node is a GPU node based on its allocatable resources
func (cp *cpuQuotaPlugin) isGPUNode(node *api.NodeInfo) bool {
	if len(cp.gpuResourcePatterns) == 0 {
		return false
	}

	for resourceName := range node.Node.Status.Allocatable {
		for _, pattern := range cp.gpuResourcePatterns {
			if pattern.MatchString(string(resourceName)) {
				return true
			}
		}
	}
	return false
}

// isCPUPod checks if a pod is a CPU pod (does not request GPU resources)
func (cp *cpuQuotaPlugin) isCPUPod(task *api.TaskInfo) bool {
	if len(cp.gpuResourcePatterns) == 0 {
		return false
	}

	for resourceName := range task.Resreq.ScalarResources {
		for _, pattern := range cp.gpuResourcePatterns {
			if pattern.MatchString(string(resourceName)) {
				return false
			}
		}
	}
	return true
}

// calculateCPUQuota calculates the CPU quota for a node based on configuration and annotations
func (cp *cpuQuotaPlugin) calculateCPUQuota(node *api.NodeInfo) (float64, error) {
	// Check node annotations first (highest priority)
	if quotaStr, exists := node.Node.Annotations[NodeAnnotationCPUQuota]; exists && quotaStr != "" {
		quota, err := resource.ParseQuantity(quotaStr)
		if err != nil {
			return 0, fmt.Errorf("invalid CPU quota annotation %s: %v", quotaStr, err)
		}
		return float64(quota.MilliValue()), nil
	}

	if percentageStr, exists := node.Node.Annotations[NodeAnnotationCPUQuotaPercentage]; exists && percentageStr != "" {
		percentage, err := strconv.ParseFloat(percentageStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid CPU quota percentage annotation %s: %v", percentageStr, err)
		}
		if percentage <= 0 || percentage > 100 {
			return 0, fmt.Errorf("CPU quota percentage must be between 0 and 100, got %f", percentage)
		}
		return node.Allocatable.MilliCPU * percentage / 100.0, nil
	}

	// Check plugin configuration
	if cp.cpuQuota > 0 {
		return cp.cpuQuota * 1000.0, nil // Convert cores to milliCPU
	}

	if cp.cpuQuotaPercentage > 0 {
		if cp.cpuQuotaPercentage <= 0 || cp.cpuQuotaPercentage > 100 {
			return 0, fmt.Errorf("CPU quota percentage must be between 0 and 100, got %f", cp.cpuQuotaPercentage)
		}
		return node.Allocatable.MilliCPU * cp.cpuQuotaPercentage / 100.0, nil
	}

	// No quota configured, return full allocatable CPU
	return node.Allocatable.MilliCPU, nil
}

func (cp *cpuQuotaPlugin) OnSessionOpen(ssn *framework.Session) {
	// Process each node
	for _, node := range ssn.Nodes {
		// Skip non-GPU nodes
		if !cp.isGPUNode(node) {
			continue
		}

		// Calculate CPU quota for this node
		cpuQuota, err := cp.calculateCPUQuota(node)
		if err != nil {
			klog.Warningf("Failed to calculate CPU quota for node %s: %v", node.Name, err)
			continue
		}

		// Create a new NodeInfo with modified allocatable CPU
		newNodeInfo := node.Clone()
		newNodeInfo.Allocatable.MilliCPU = cpuQuota
		cp.Nodes[node.Name] = newNodeInfo

		klog.V(4).Infof("Node %s CPU quota set to %.2f (original: %.2f)",
			node.Name, cpuQuota, node.Allocatable.MilliCPU)

		// Add existing CPU tasks to the tracking
		for _, task := range node.Tasks {
			if cp.isCPUPod(task) {
				cp.Nodes[node.Name].AddTask(task)
				klog.V(4).Infof("Added existing CPU task %s to node %s tracking", task.Name, node.Name)
			}
		}
	}

	// Add predicate function
	ssn.AddPredicateFn(cp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		// Skip non-CPU pods
		if !cp.isCPUPod(task) {
			return nil
		}

		// Skip non-GPU nodes
		if !cp.isGPUNode(node) {
			return nil
		}

		// Check if node is being tracked
		trackedNode, exists := cp.Nodes[node.Name]
		if !exists {
			return nil
		}

		// Check if adding this task would exceed the CPU quota
		requiredCPU := task.Resreq.MilliCPU
		usedCPU := trackedNode.Used.MilliCPU
		allocatableCPU := trackedNode.Allocatable.MilliCPU

		if usedCPU+requiredCPU > allocatableCPU {
			return api.NewFitError(task, node, fmt.Sprintf(
				"CPU quota exceeded: required %.2f, used %.2f, available %.2f",
				requiredCPU, usedCPU, allocatableCPU))
		}

		return nil
	})

	// Register event handlers
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			// Skip non-CPU pods
			if !cp.isCPUPod(event.Task) {
				return
			}

			// Skip if node is not being tracked
			trackedNode, exists := cp.Nodes[event.Task.NodeName]
			if !exists {
				return
			}

			// Add task to tracking
			trackedNode.AddTask(event.Task)
			klog.V(4).Infof("CPU task %s allocated to node %s, CPU usage: %.2f/%.2f",
				event.Task.Name, event.Task.NodeName,
				trackedNode.Used.MilliCPU, trackedNode.Allocatable.MilliCPU)
		},
		DeallocateFunc: func(event *framework.Event) {
			// Skip non-CPU pods
			if !cp.isCPUPod(event.Task) {
				return
			}

			// Skip if node is not being tracked
			trackedNode, exists := cp.Nodes[event.Task.NodeName]
			if !exists {
				return
			}

			// Remove task from tracking
			trackedNode.RemoveTask(event.Task)
			klog.V(4).Infof("CPU task %s deallocated from node %s, CPU usage: %.2f/%.2f",
				event.Task.Name, event.Task.NodeName,
				trackedNode.Used.MilliCPU, trackedNode.Allocatable.MilliCPU)
		},
	})
}

func (cp *cpuQuotaPlugin) OnSessionClose(ssn *framework.Session) {
	// Clean up tracking data
	cp.Nodes = make(map[string]*api.NodeInfo)
}
