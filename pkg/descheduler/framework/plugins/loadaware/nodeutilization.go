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

package loadaware

import (
	"context"
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/descheduler/pkg/api"
	"sigs.k8s.io/descheduler/pkg/descheduler/evictions"
	"sigs.k8s.io/descheduler/pkg/descheduler/node"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
	"sigs.k8s.io/descheduler/pkg/utils"

	source "volcano.sh/volcano/pkg/descheduler/framework/plugins/loadaware/metrics"
)

// NodeUsage stores a node's info, pods on it, thresholds and its resource usage
type NodeUsage struct {
	node             *v1.Node
	usage            map[v1.ResourceName]*resource.Quantity
	allPods          []*v1.Pod
	podMetrics       map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity
	overUseResources *[]v1.ResourceName
}

type NodeThresholds struct {
	lowResourceThreshold  map[v1.ResourceName]*resource.Quantity
	highResourceThreshold map[v1.ResourceName]*resource.Quantity
}

type NodeInfo struct {
	NodeUsage
	thresholds NodeThresholds
}

type continueEvictionCond func(nodeInfo NodeInfo, desNodeAvailableRes map[v1.ResourceName]*resource.Quantity) bool

// NodePodsMap is a set of (node, pods) pairs
type NodePodsMap map[*v1.Node][]*v1.Pod

const (
	// MinResourcePercentage is the minimum value of a resource's percentage
	MinResourcePercentage = 0
	// MaxResourcePercentage is the maximum value of a resource's percentage
	MaxResourcePercentage = 100
)

func normalizePercentage(percent api.Percentage) api.Percentage {
	if percent > MaxResourcePercentage {
		return MaxResourcePercentage
	}
	if percent < MinResourcePercentage {
		return MinResourcePercentage
	}
	return percent
}

func getNodeThresholds(
	nodes []*v1.Node,
	lowThreshold, highThreshold api.ResourceThresholds,
	resourceNames []v1.ResourceName,
	getPodsAssignedToNode podutil.GetPodsAssignedToNodeFunc,
	useDeviationThresholds bool,
) map[string]NodeThresholds {
	nodeThresholdsMap := map[string]NodeThresholds{}

	averageResourceUsagePercent := api.ResourceThresholds{}
	if useDeviationThresholds {
		averageResourceUsagePercent = averageNodeBasicresources(nodes, getPodsAssignedToNode, resourceNames)
	}

	for _, node := range nodes {
		nodeCapacity := node.Status.Capacity
		if len(node.Status.Allocatable) > 0 {
			nodeCapacity = node.Status.Allocatable
		}

		nodeThresholdsMap[node.Name] = NodeThresholds{
			lowResourceThreshold:  map[v1.ResourceName]*resource.Quantity{},
			highResourceThreshold: map[v1.ResourceName]*resource.Quantity{},
		}

		for _, resourceName := range resourceNames {
			if useDeviationThresholds {
				capacity := nodeCapacity[resourceName]
				if lowThreshold[resourceName] == MinResourcePercentage {
					nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = &capacity
					nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = &capacity
				} else {
					nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, normalizePercentage(averageResourceUsagePercent[resourceName]-lowThreshold[resourceName]))
					nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, normalizePercentage(averageResourceUsagePercent[resourceName]+highThreshold[resourceName]))
				}
			} else {
				nodeThresholdsMap[node.Name].lowResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, lowThreshold[resourceName])
				nodeThresholdsMap[node.Name].highResourceThreshold[resourceName] = resourceThreshold(nodeCapacity, resourceName, highThreshold[resourceName])
			}
		}

	}
	return nodeThresholdsMap
}

func (l *LoadAwareUtilization) getNodeUsage(
	nodes []*v1.Node,
	resourceNames []v1.ResourceName,
	getPodsAssignedToNode podutil.GetPodsAssignedToNodeFunc,
) []NodeUsage {
	klog.V(5).Infof("start get node usage metrics")
	var nodeUsageList []NodeUsage
	nodesMetrics := make(map[string]*source.NodeMetrics)
	nodesInfo := make(map[string]*v1.Node)
	for _, node := range nodes {
		nodesMetrics[node.Name] = &source.NodeMetrics{}
		nodesInfo[node.Name] = node
	}

	client, err := source.NewMetricsClient(l.args.MetricsConfiguration)
	if err != nil {
		klog.Errorf("New metrics client failed, metrics config is %v, error is %v", l.args.MetricsConfiguration, err)
		return nodeUsageList
	}
	client.NodesMetricsAvg(context.TODO(), nodesMetrics, l.args.Duration)
	for nodeName, nodeMetrics := range nodesMetrics {
		pods, err := podutil.ListPodsOnANode(nodeName, getPodsAssignedToNode, nil)
		if err != nil {
			klog.V(2).Infof("Failed to obtain the pod information of the node(%s), error is %v", nodeName, err)
			continue
		}
		nodeUsage := getNodeUtilization(nodesInfo[nodeName], pods, nodeMetrics)
		podMetric, err := client.PodsMetricsAvg(context.TODO(), pods, l.args.Duration)
		if err != nil {
			klog.V(2).Infof("Failed to get pod metrics average, error is %v", err)
			continue
		}
		overUseResources := make([]v1.ResourceName, 0)
		nodeUsageList = append(nodeUsageList, NodeUsage{
			node:             nodesInfo[nodeName],
			usage:            nodeUsage,
			allPods:          pods,
			podMetrics:       podMetric,
			overUseResources: &overUseResources,
		})
	}
	return nodeUsageList
}

func resourceThreshold(nodeCapacity v1.ResourceList, resourceName v1.ResourceName, threshold api.Percentage) *resource.Quantity {
	defaultFormat := resource.DecimalSI
	if resourceName == v1.ResourceMemory {
		defaultFormat = resource.BinarySI
	}

	resourceCapacityFraction := func(resourceNodeCapacity int64) int64 {
		// A threshold is in percentages but in <0;100> interval.
		// Performing `threshold * 0.01` will convert <0;100> interval into <0;1>.
		// Multiplying it with capacity will give fraction of the capacity corresponding to the given resource threshold in Quantity units.
		return int64(float64(threshold) * 0.01 * float64(resourceNodeCapacity))
	}

	resourceCapacityQuantity := nodeCapacity.Name(resourceName, defaultFormat)

	if resourceName == v1.ResourceCPU {
		return resource.NewMilliQuantity(resourceCapacityFraction(resourceCapacityQuantity.MilliValue()), defaultFormat)
	}
	return resource.NewQuantity(resourceCapacityFraction(resourceCapacityQuantity.Value()), defaultFormat)
}

func resourceUsagePercentages(nodeUsage NodeUsage) map[v1.ResourceName]float64 {
	nodeCapacity := nodeUsage.node.Status.Capacity
	if len(nodeUsage.node.Status.Allocatable) > 0 {
		nodeCapacity = nodeUsage.node.Status.Allocatable
	}

	resourceUsagePercentage := map[v1.ResourceName]float64{}
	for resourceName, resourceUsage := range nodeUsage.usage {
		capacity := nodeCapacity[resourceName]
		if !capacity.IsZero() {
			resourceUsagePercentage[resourceName] = 100 * float64(resourceUsage.MilliValue()) / float64(capacity.MilliValue())
		}
	}

	return resourceUsagePercentage
}

// classifyNodes classifies the nodes into low-utilization or high-utilization nodes. If a node lies between
// low and high thresholds, it is simply ignored.
func classifyNodes(
	nodeUsages []NodeUsage,
	nodeThresholds map[string]NodeThresholds,
	lowThresholdFilter, highThresholdFilter func(node *v1.Node, usage NodeUsage, threshold NodeThresholds) bool,
) ([]NodeInfo, []NodeInfo) {
	lowNodes, highNodes := []NodeInfo{}, []NodeInfo{}

	for _, nodeUsage := range nodeUsages {
		nodeInfo := NodeInfo{
			NodeUsage:  nodeUsage,
			thresholds: nodeThresholds[nodeUsage.node.Name],
		}
		if lowThresholdFilter(nodeUsage.node, nodeUsage, nodeThresholds[nodeUsage.node.Name]) {
			klog.InfoS("Node is underutilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
			lowNodes = append(lowNodes, nodeInfo)
		} else if highThresholdFilter(nodeUsage.node, nodeUsage, nodeThresholds[nodeUsage.node.Name]) {
			klog.InfoS("Node is overutilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
			highNodes = append(highNodes, nodeInfo)
		} else {
			klog.InfoS("Node is appropriately utilized", "node", klog.KObj(nodeUsage.node), "usage", nodeUsage.usage, "usagePercentage", resourceUsagePercentages(nodeUsage))
		}
	}

	return lowNodes, highNodes
}

// evictPodsFromSourceNodes evicts pods based on priority, if all the pods on the node have priority, if not
// evicts them based on QoS as fallback option.
// TODO: @ravig Break this function into smaller functions.
func evictPodsFromSourceNodes(
	ctx context.Context,
	evictableNamespaces *api.Namespaces,
	sourceNodes, destinationNodes []NodeInfo,
	podEvictor frameworktypes.Evictor,
	podFilter func(pod *v1.Pod) bool,
	resourceNames []v1.ResourceName,
	continueEviction continueEvictionCond,
) {
	// upper bound on total number of pods/cpu/memory and optional extended resources to be moved
	desNodeAvailableRes := map[v1.ResourceName]*resource.Quantity{
		v1.ResourcePods:   {},
		v1.ResourceCPU:    {},
		v1.ResourceMemory: {},
	}

	taintsOfDestinationNodes := make(map[string][]v1.Taint, len(destinationNodes))
	for _, node := range destinationNodes {
		taintsOfDestinationNodes[node.node.Name] = node.node.Spec.Taints

		for _, name := range resourceNames {
			if _, ok := desNodeAvailableRes[name]; !ok {
				desNodeAvailableRes[name] = resource.NewQuantity(0, resource.DecimalSI)
			}
			desNodeAvailableRes[name].Add(*node.thresholds.highResourceThreshold[name])
			desNodeAvailableRes[name].Sub(*node.usage[name])
		}
	}

	// log message in one line
	keysAndValues := []interface{}{
		"CPU", desNodeAvailableRes[v1.ResourceCPU].MilliValue(),
		"Mem", desNodeAvailableRes[v1.ResourceMemory].Value(),
		"Pods", desNodeAvailableRes[v1.ResourcePods].Value(),
	}
	for name := range desNodeAvailableRes {
		if !node.IsBasicResource(name) {
			keysAndValues = append(keysAndValues, string(name), desNodeAvailableRes[name].Value())
		}
	}
	klog.V(1).InfoS("Total capacity to be moved", keysAndValues...)

	for _, node := range sourceNodes {
		klog.V(3).InfoS("Evicting pods from node", "node", klog.KObj(node.node), "usage", node.usage)

		nonRemovablePods, removablePods := classifyPods(node.allPods, podFilter)
		klog.V(2).InfoS("Pods on node", "node", klog.KObj(node.node), "allPods", len(node.allPods), "nonRemovablePods", len(nonRemovablePods), "removablePods", len(removablePods))

		if len(removablePods) == 0 {
			klog.V(1).InfoS("No removable pods on node, try next node", "current node is ", klog.KObj(node.node))
			continue
		}

		klog.V(1).InfoS("Evicting pods based on used quantity")
		// sort the evictable Pods based on used quantity
		keyResource := v1.ResourceMemory
		for _, value := range *node.overUseResources {
			if value == v1.ResourceCPU {
				keyResource = v1.ResourceCPU
				break
			}
		}
		klog.V(3).Infof("Removeable pod sort by %s resources", keyResource)
		sort.Slice(removablePods, func(i, j int) bool {
			podLeft := types.NamespacedName{
				Namespace: removablePods[i].Namespace,
				Name:      removablePods[i].Name,
			}
			podRight := types.NamespacedName{
				Namespace: removablePods[j].Namespace,
				Name:      removablePods[j].Name,
			}
			resoureCPULeft := make(map[v1.ResourceName]*resource.Quantity)
			resouceCPURight := make(map[v1.ResourceName]*resource.Quantity)
			var exist bool
			if resoureCPULeft, exist = node.podMetrics[podLeft]; !exist {
				// The podmetrics table does not contain the usage of the current pod.
				// The pod is sorted at the end of the queue.
				return false
			} else if resouceCPURight, exist = node.podMetrics[podRight]; !exist {
				// The podmetrics table does not contain the podRight usage information.
				// The podLeft field is sorted before the podRight field.
				return true
			}

			if quantityLeft, ok := resoureCPULeft[keyResource]; !ok {
				// The podmetrics table does not contain the usage of the current pod.
				// The pod is sorted at the end of the queue.
				return false
			} else if quantityRight, ok := resouceCPURight[keyResource]; !ok {
				// The podmetrics table does not contain the podRight usage information.
				// The podLeft field is sorted before the podRight field.
				return true
			} else {
				return quantityLeft.Cmp(*quantityRight) > 0
			}
		})

		evictPods(ctx, evictableNamespaces, removablePods, node, desNodeAvailableRes, taintsOfDestinationNodes, podEvictor, continueEviction)
	}
}

func evictPods(
	ctx context.Context,
	evictableNamespaces *api.Namespaces,
	inputPods []*v1.Pod,
	nodeInfo NodeInfo,
	desNodeAvailableRes map[v1.ResourceName]*resource.Quantity,
	taintsOfLowNodes map[string][]v1.Taint,
	podEvictor frameworktypes.Evictor,
	continueEviction continueEvictionCond,
) {
	var excludedNamespaces sets.Set[string]
	if evictableNamespaces != nil {
		excludedNamespaces = sets.New(evictableNamespaces.Exclude...)
	}

	if continueEviction(nodeInfo, desNodeAvailableRes) {
		for _, pod := range inputPods {
			if !utils.PodToleratesTaints(pod, taintsOfLowNodes) {
				klog.V(3).InfoS("Skipping eviction for pod, doesn't tolerate node taint", "pod", klog.KObj(pod))
				continue
			}

			preEvictionFilterWithOptions, err := podutil.NewOptions().
				WithFilter(podEvictor.PreEvictionFilter).
				WithoutNamespaces(excludedNamespaces).
				BuildFilterFunc()
			if err != nil {
				klog.ErrorS(err, "could not build preEvictionFilter with namespace exclusion")
				continue
			}

			if preEvictionFilterWithOptions(pod) {
				if podEvictor.Evict(ctx, pod, evictions.EvictOptions{}) {
					klog.V(3).InfoS("Evicted pods", "pod", klog.KObj(pod))

					for resourceName := range desNodeAvailableRes {
						if resourceName == v1.ResourcePods {
							nodeInfo.usage[resourceName].Sub(*resource.NewQuantity(1, resource.DecimalSI))
							desNodeAvailableRes[resourceName].Sub(*resource.NewQuantity(1, resource.DecimalSI))
						} else {
							podNamespaceAndName := types.NamespacedName{
								Namespace: pod.Namespace,
								Name:      pod.Name,
							}
							if resourceMetrics, ok := nodeInfo.podMetrics[podNamespaceAndName]; !ok {
								klog.Warningf("The actual pod usage is not obtained from the cache information of the node. "+
									"Therefore, the pod(%s) is not evicted.", podNamespaceAndName)
								continue
							} else if quantity, ok := resourceMetrics[resourceName]; !ok {
								klog.Warningf("The %v indicator of the pod is not obtained from the node information. The pod(%v) is not evicted.", resourceName, podNamespaceAndName)
								continue
							} else {
								nodeInfo.usage[resourceName].Sub(*quantity)
								desNodeAvailableRes[resourceName].Sub(*quantity)
							}
						}
					}

					keysAndValues := []interface{}{
						"node", nodeInfo.node.Name,
						"CPU", nodeInfo.usage[v1.ResourceCPU].MilliValue(),
						"Mem", nodeInfo.usage[v1.ResourceMemory].Value(),
						"Pods", nodeInfo.usage[v1.ResourcePods].Value(),
					}
					for name := range desNodeAvailableRes {
						if !nodeutil.IsBasicResource(name) {
							keysAndValues = append(keysAndValues, string(name), desNodeAvailableRes[name].Value())
						}
					}

					klog.V(3).InfoS("Updated node usage", keysAndValues...)
					// check if pods can be still evicted
					if !continueEviction(nodeInfo, desNodeAvailableRes) {
						break
					}
				}
			}
			if podEvictor.NodeLimitExceeded(nodeInfo.node) {
				return
			}
		}
	}
}

// sortNodesByUsage sorts nodes based on usage according to the given plugin.
func sortNodesByUsage(nodes []NodeInfo, ascending bool) {
	sort.Slice(nodes, func(i, j int) bool {
		ti := nodes[i].usage[v1.ResourceMemory].Value() + nodes[i].usage[v1.ResourceCPU].MilliValue() + nodes[i].usage[v1.ResourcePods].Value()
		tj := nodes[j].usage[v1.ResourceMemory].Value() + nodes[j].usage[v1.ResourceCPU].MilliValue() + nodes[j].usage[v1.ResourcePods].Value()

		// extended resources
		for name := range nodes[i].usage {
			if !nodeutil.IsBasicResource(name) {
				ti = ti + nodes[i].usage[name].Value()
				tj = tj + nodes[j].usage[name].Value()
			}
		}

		// Return ascending order for HighNodeUtilization plugin
		if ascending {
			return ti < tj
		}

		// Return descending order for LowNodeUtilization plugin
		return ti > tj
	})
}

// isNodeAboveTargetUtilization checks if a node is overutilized
// At least one resource has to be above the high threshold
func isNodeAboveTargetUtilization(usage NodeUsage, threshold map[v1.ResourceName]*resource.Quantity) bool {
	for name, nodeValue := range usage.usage {
		// usage.highResourceThreshold[name] < nodeValue
		if threshold[name].Cmp(*nodeValue) == -1 {
			*usage.overUseResources = append(*usage.overUseResources, name)
			return true
		}
	}
	return false
}

// isNodeWithLowUtilization checks if a node is underutilized
// All resources have to be below the low threshold
func isNodeWithLowUtilization(usage NodeUsage, threshold map[v1.ResourceName]*resource.Quantity) bool {
	for name, nodeValue := range usage.usage {
		// usage.lowResourceThreshold[name] < nodeValue
		if threshold[name].Cmp(*nodeValue) == -1 {
			return false
		}
	}

	return true
}

// getResourceNames returns list of resource names in resource thresholds
func getResourceNames(thresholds api.ResourceThresholds) []v1.ResourceName {
	resourceNames := make([]v1.ResourceName, 0, len(thresholds))
	for name := range thresholds {
		resourceNames = append(resourceNames, name)
	}
	return resourceNames
}

func classifyPods(pods []*v1.Pod, filter func(pod *v1.Pod) bool) ([]*v1.Pod, []*v1.Pod) {
	var nonRemovablePods, removablePods []*v1.Pod

	for _, pod := range pods {
		if !filter(pod) {
			nonRemovablePods = append(nonRemovablePods, pod)
		} else {
			removablePods = append(removablePods, pod)
		}
	}

	return nonRemovablePods, removablePods
}

func averageNodeBasicresources(nodes []*v1.Node, getPodsAssignedToNode podutil.GetPodsAssignedToNodeFunc, resourceNames []v1.ResourceName) api.ResourceThresholds {
	total := api.ResourceThresholds{}
	average := api.ResourceThresholds{}
	numberOfNodes := len(nodes)
	for _, node := range nodes {
		pods, err := podutil.ListPodsOnANode(node.Name, getPodsAssignedToNode, nil)
		if err != nil {
			numberOfNodes--
			continue
		}
		usage := nodeutil.NodeUtilization(pods, resourceNames)
		nodeCapacity := node.Status.Capacity
		if len(node.Status.Allocatable) > 0 {
			nodeCapacity = node.Status.Allocatable
		}
		for resource, value := range usage {
			nodeCapacityValue := nodeCapacity[resource]
			if resource == v1.ResourceCPU {
				total[resource] += api.Percentage(value.MilliValue()) / api.Percentage(nodeCapacityValue.MilliValue()) * 100.0
			} else {
				total[resource] += api.Percentage(value.Value()) / api.Percentage(nodeCapacityValue.Value()) * 100.0
			}
		}
	}
	for resource, value := range total {
		average[resource] = value / api.Percentage(numberOfNodes)
	}
	return average
}

func getNodeUtilization(node *v1.Node, pods []*v1.Pod, nodeMetrics *source.NodeMetrics) map[v1.ResourceName]*resource.Quantity {
	nodeResource := make(map[v1.ResourceName]*resource.Quantity)

	nodeStatusCapacityCpu := node.Status.Capacity[v1.ResourceCPU]
	nodeStatusCpuUsed := resource.MustParse(fmt.Sprintf("%f", float64(nodeStatusCapacityCpu.Value())*nodeMetrics.CPU))
	nodeResource[v1.ResourceCPU] = &nodeStatusCpuUsed

	nodeStatusCapacityMemory := node.Status.Capacity[v1.ResourceMemory]
	nodeStatusUsedMemory := resource.MustParse(fmt.Sprintf("%f", float64(nodeStatusCapacityMemory.Value())*nodeMetrics.Memory))
	nodeResource[v1.ResourceMemory] = &nodeStatusUsedMemory

	nodeResource[v1.ResourcePods] = resource.NewQuantity(int64(len(pods)), resource.DecimalSI)
	return nodeResource
}
