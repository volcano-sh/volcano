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
	//klog.Infof("Calculating utilization for node %s", nodeName)
	pods, err := sc.listPodsOnNode(nodeName)
	if err != nil {
		return nil, err
	}
	//klog.Infof("Found %d pods on node %s for utilization calculation", len(pods), nodeName)

	node, err := sc.nodeLister.Get(nodeName)
	if err != nil {
		return nil, err
	}
	//klog.Infof("Fetched node %s object for utilization calculation", nodeName)

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
			sc.enqueueSyncEvent()
		}
	}
}

// updateNodeUtilization updates node utilization in state
// func (sc *ShardingController) updateNodeUtilization(nodeName string, util *NodeUtilization) {
// 	sc.nodeMutex.Lock()
// 	defer sc.nodeMutex.Unlock()

// 	if state, exists := sc.nodeStates[nodeName]; exists {
// 		state.CPUUtilization = util.CPUUtilization
// 		state.MemoryUtilization = util.MemoryUtilization
// 		state.LastUpdated = util.LastUpdated
// 	} else {
// 		// Create new state if not exists
// 		sc.nodeStates[nodeName] = &NodeState{
// 			Name:              nodeName,
// 			CPUUtilization:    util.CPUUtilization,
// 			MemoryUtilization: util.MemoryUtilization,
// 			LastUpdated:       util.LastUpdated,
// 		}
// 	}
// }

// func byIndex(indexName, indexKey string) func([]string) bool {
// 	return func(indexValues []string) bool {
// 		for _, v := range indexValues {
// 			if v == indexKey {
// 				return true
// 			}
// 		}
// 		return false
// 	}
// }

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

	// err := sc.podInformer.Informer().AddIndexers(cache.Indexers{
	// 	"node": func(obj interface{}) ([]string, error) {
	// 		pod, ok := obj.(*corev1.Pod)
	// 		if !ok {
	// 			return []string{}, nil
	// 		}
	// 		if pod.Spec.NodeName == "" {
	// 			return []string{}, nil
	// 		}
	// 		return []string{pod.Spec.NodeName}, nil
	// 	},
	// })
	// if err != nil {
	// 	klog.Errorf("Failed to add pod index by node: %v", err)
	// }
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

// func (sc *ShardingController) listPodsOnNode(nodeName string) ([]*corev1.Pod, error) {
// 	// List all pods in all namespaces
// 	podList, err := sc.podLister.List(metav1.ListOptions{
// 		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Failed,status.phase!=Succeeded", nodeName),
// 	})
// 	// podList, err := sc.podInformer.Informer().GetIndexer().ByIndex("spec.nodeName", nodeName)
// 	// if err != nil {
// 	// 	return nil, fmt.Errorf("failed to list pods on node %s: %v", nodeName, err)
// 	// }

// 	// Filter and copy pods to avoid cache modification
// 	activePods := make([]*corev1.Pod, 0, len(podList))

// 	for i := range podList {
// 		pod := podList[i].(*corev1.Pod)

// 		// Skip completed pods
// 		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
// 			continue
// 		}

// 		// Skip mirror pods (static pods managed by kubelet)
// 		if _, isMirror := pod.Annotations["kubernetes.io/config.mirror"]; isMirror {
// 			continue
// 		}

// 		// Create a copy to avoid modifying cache
// 		podCopy := pod.DeepCopy()
// 		activePods = append(activePods, podCopy)
// 	}

// 	klog.V(5).Infof("Found %d active pods on node %s", len(activePods), nodeName)
// 	return activePods, nil
// }

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
			//klog.Infof("Pod %s/%s container %s requests CPU: %d mCPU, Memory: %d bytes",
			//	pod.Namespace, pod.Name, container.Name, cpuReq.MilliValue(), memoryReq.Value())
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

// func (sc *ShardingController) calculateMetricsFromPodsAndNode(pods []*corev1.Pod, node *corev1.Node) (*NodeMetrics, error) {
// 	// Calculate total resource requests from pods
// 	totalCPURequest := resource.Quantity{}
// 	totalMemoryRequest := resource.Quantity{}

// 	for _, pod := range pods {
// 		for _, container := range pod.Spec.Containers {
// 			// Sum CPU requests
// 			if cpuReq, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
// 				totalCPURequest.Add(cpuReq)
// 			}

// 			// Sum memory requests
// 			if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
// 				totalMemoryRequest.Add(memReq)
// 			}
// 		}
// 	}

// 	// Get node capacity and allocatable
// 	cpuCapacity := node.Status.Capacity.Cpu()
// 	cpuAllocatable := node.Status.Allocatable.Cpu()
// 	memoryCapacity := node.Status.Capacity.Memory()
// 	memoryAllocatable := node.Status.Allocatable.Memory()

// 	// Calculate utilization percentages
// 	cpuUtilization := 0.0
// 	memoryUtilization := 0.0

// 	if cpuCapacity.MilliValue() > 0 {
// 		cpuUtilization = float64(totalCPURequest.MilliValue()) / float64(cpuCapacity.MilliValue())
// 		// Cap utilization at 1.0 (100%)
// 		if cpuUtilization > 1.0 {
// 			cpuUtilization = 1.0
// 		}
// 	}

// 	if memoryCapacity.Value() > 0 {
// 		memoryUtilization = float64(totalMemoryRequest.Value()) / float64(memoryCapacity.Value())
// 		// Cap utilization at 1.0 (100%)
// 		if memoryUtilization > 1.0 {
// 			memoryUtilization = 1.0
// 		}
// 	}

// 	// Check if node is warmup node
// 	isWarmup := node.Labels["node.volcano.sh/warmup"] == "true"

// 	// Create metrics object
// 	metrics := &NodeMetrics{
// 		NodeName:        node.Name,
// 		ResourceVersion: node.ResourceVersion,
// 		LastUpdated:     time.Now(),

// 		CPUCapacity:       *cpuCapacity,
// 		CPUAllocatable:    *cpuAllocatable,
// 		MemoryCapacity:    *memoryCapacity,
// 		MemoryAllocatable: *memoryAllocatable,

// 		CPUUtilization:    cpuUtilization,
// 		MemoryUtilization: memoryUtilization,

// 		IsWarmupNode: isWarmup,
// 		PodCount:     len(pods),
// 		Labels:       make(map[string]string),
// 		Annotations:  make(map[string]string),
// 	}

// 	// Copy labels and annotations
// 	for k, v := range node.Labels {
// 		metrics.Labels[k] = v
// 	}
// 	for k, v := range node.Annotations {
// 		metrics.Annotations[k] = v
// 	}

// 	// Log detailed metrics for debugging
// 	klog.V(5).Infof("Node %s metrics calculated: CPU=%.2f (%d/%d mCPU), Memory=%.2f (%d/%d bytes), Pods=%d, Warmup=%v",
// 		node.Name,
// 		cpuUtilization, totalCPURequest.MilliValue(), cpuCapacity.MilliValue(),
// 		memoryUtilization, totalMemoryRequest.Value(), memoryCapacity.Value(),
// 		len(pods), isWarmup)

// 	return metrics, nil
// }

// func (sc *ShardingController) calculateUtilizationFromPodsAndNode(pods []*corev1.Pod, node *corev1.Node) (*NodeUtilization, error) {
// 	totalCPURequest := int64(0)
// 	totalMemoryRequest := int64(0)

// 	for _, pod := range pods {
// 		for _, container := range pod.Spec.Containers {
// 			if cpuReq, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
// 				totalCPURequest += cpuReq.MilliValue()
// 			}
// 			if memReq, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
// 				totalMemoryRequest += memReq.Value()
// 			}
// 		}
// 	}

// 	nodeCPUCapacity := node.Status.Capacity.Cpu().MilliValue()
// 	nodeMemoryCapacity := node.Status.Capacity.Memory().Value()

// 	cpuUtil := 0.0
// 	memUtil := 0.0

// 	if nodeCPUCapacity > 0 {
// 		cpuUtil = float64(totalCPURequest) / float64(nodeCPUCapacity)
// 	}
// 	if nodeMemoryCapacity > 0 {
// 		memUtil = float64(totalMemoryRequest) / float64(nodeMemoryCapacity)
// 	}

// 	return &NodeUtilization{
// 		CPUUtilization:    cpuUtil,
// 		MemoryUtilization: memUtil,
// 		LastUpdated:       time.Now(),
// 	}, nil
// }

// pkg/controllers/sharding/node_utilization.go
// func (sc *ShardingController) isUtilizationSignificantlyChanged(nodeName string, newUtil *NodeMetrics) bool {
// 	sc.metricsMutex.RLock()
// 	defer sc.metricsMutex.RUnlock()

// 	// Get current state
// 	state, exists := sc.nodeMetricsCache[nodeName]
// 	if !exists {
// 		return true // New node always considered significant
// 	}

// 	// Calculate changes
// 	cpuChange := newUtil.CPUUtilization - state.CPUUtilization
// 	if cpuChange < 0 {
// 		cpuChange = -cpuChange
// 	}

// 	memChange := newUtil.MemoryUtilization - state.MemoryUtilization
// 	if memChange < 0 {
// 		memChange = -memChange
// 	}

// 	// Log significant changes for debugging
// 	if cpuChange > 0.1 || memChange > 0.1 {
// 		klog.V(4).Infof("Node %s utilization changed significantly: CPU %.2f->%.2f (%.2f), Memory %.2f->%.2f (%.2f)",
// 			nodeName,
// 			state.CPUUtilization, newUtil.CPUUtilization, cpuChange,
// 			state.MemoryUtilization, newUtil.MemoryUtilization, memChange)
// 	}

// 	// Return true if either resource changed by more than 10%
// 	return cpuChange > 0.1 || memChange > 0.1
// }
