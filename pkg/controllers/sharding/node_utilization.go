package sharding

import (
	"fmt"
	"math"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// calculateNodeUtilization calculates utilization directly in controller
func (sc *ShardingController) calculateNodeUtilization(nodeName string) (*NodeMetrics, error) {
	pods, err := sc.listPodsOnNode(nodeName)
	if err != nil {
		return nil, err
	}

	node, err := sc.nodeLister.Get(nodeName)
	if err != nil {
		return nil, err
	}

	return sc.calculateMetricsFromPodsAndNode(pods, node)
}

// updateNodeUtilization updates node metrics in cache
func (sc *ShardingController) updateNodeUtilization(nodeName string, metrics *NodeMetrics) {
	sc.UpdateNodeMetrics(nodeName, metrics)

	// Log significant changes
	if prevMetrics := sc.GetNodeMetrics(nodeName); prevMetrics != nil {
		cpuChange := math.Abs(metrics.CPUUtilization - prevMetrics.CPUUtilization)
		memChange := math.Abs(metrics.MemoryUtilization - prevMetrics.MemoryUtilization)

		if cpuChange > 0.1 || memChange > 0.1 {
			klog.Infof("Node %s utilization changed significantly: CPU %.2f->%.2f, Memory %.2f->%.2f",
				nodeName, prevMetrics.CPUUtilization, metrics.CPUUtilization,
				prevMetrics.MemoryUtilization, metrics.MemoryUtilization)
			sc.enqueueNodeEvent(nodeName, "node-updated", "node-utilization-manager")
		}
	}
}

// initNodeIndices initializes indexes for efficient node-based queries
func (sc *ShardingController) initNodeIndices() {
	// Create index for pods by node name
	// Only add if not already exists
	if _, exists := sc.podInformer.Informer().GetIndexer().GetIndexers()["node"]; !exists {
		if err := sc.podInformer.Informer().AddIndexers(cache.Indexers{
			"node": func(obj interface{}) ([]string, error) {
				pod, ok := obj.(*corev1.Pod)
				if !ok || pod.Spec.NodeName == "" {
					return []string{}, nil
				}
				return []string{pod.Spec.NodeName}, nil
			},
		}); err != nil {
			klog.Errorf("Failed to add pod index by node: %v", err)
		} else {
			klog.Info("Successfully added pod index by node")
		}
	}
}

// listPodsOnNode lists all active pods on a node
func (sc *ShardingController) listPodsOnNode(nodeName string) ([]*corev1.Pod, error) {
	// Use the node index to get pods
	// This is more efficient than listing all pods
	objs, err := sc.podInformer.Informer().GetIndexer().ByIndex("node", nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get pods by node index: %v", err)
	}

	activePods := make([]*corev1.Pod, 0, len(objs))

	for _, obj := range objs {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			continue
		}

		// Filter out completed pods
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}

		// Skip mirror pods (static pods)
		if _, isMirror := pod.Annotations["kubernetes.io/config.mirror"]; isMirror {
			continue
		}

		// Create a copy to avoid cache modification
		podCopy := pod.DeepCopy()
		activePods = append(activePods, podCopy)
	}

	return activePods, nil
}

// calculateMetricsFromPodsAndNode calculates comprehensive node metrics from pods and node object
func (sc *ShardingController) calculateMetricsFromPodsAndNode(pods []*corev1.Pod, node *corev1.Node) (*NodeMetrics, error) {
	// Calculate total resource requests
	totalCPURequest := int64(0)
	totalMemoryRequest := int64(0)

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			// Sum CPU requests
			cpuReq := container.Resources.Requests.Cpu()
			if cpuReq != nil {
				totalCPURequest += cpuReq.MilliValue()
			}

			// Sum memory requests
			memoryReq := container.Resources.Requests.Memory()
			if memoryReq != nil {
				totalMemoryRequest += memoryReq.Value()
			}
		}
	}

	// Get node capacity
	cpuCapacity := node.Status.Capacity.Cpu()
	memoryCapacity := node.Status.Capacity.Memory()
	cpuAllocatable := node.Status.Allocatable.Cpu()
	memoryAllocatable := node.Status.Allocatable.Memory()

	// Calculate utilization
	cpuUtilization := 0.0
	memoryUtilization := 0.0

	if cpuCapacity != nil && cpuCapacity.MilliValue() > 0 {
		cpuUtilization = float64(totalCPURequest) / float64(cpuCapacity.MilliValue())
		if cpuUtilization > 1.0 {
			cpuUtilization = 1.0
		}
	}

	if memoryCapacity != nil && memoryCapacity.Value() > 0 {
		memoryUtilization = float64(totalMemoryRequest) / float64(memoryCapacity.Value())
		if memoryUtilization > 1.0 {
			memoryUtilization = 1.0
		}
	}

	// Create metrics
	metrics := &NodeMetrics{
		NodeName:        node.Name,
		ResourceVersion: node.ResourceVersion,
		LastUpdated:     time.Now(),

		CPUCapacity:       *cpuCapacity,
		CPUAllocatable:    *cpuAllocatable,
		MemoryCapacity:    *memoryCapacity,
		MemoryAllocatable: *memoryAllocatable,

		CPUUtilization:    cpuUtilization,
		MemoryUtilization: memoryUtilization,

		IsWarmupNode: node.Labels["node.volcano.sh/warmup"] == "true",
		PodCount:     len(pods),

		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}

	// Copy labels and annotations
	for k, v := range node.Labels {
		metrics.Labels[k] = v
	}
	for k, v := range node.Annotations {
		metrics.Annotations[k] = v
	}

	klog.V(5).Infof("Node %s metrics: CPU=%.2f (%d/%d mCPU), Memory=%.2f (%d/%d bytes), Pods=%d",
		node.Name, cpuUtilization, totalCPURequest, cpuCapacity.MilliValue(),
		memoryUtilization, totalMemoryRequest, memoryCapacity.Value(), len(pods))

	return metrics, nil
}
