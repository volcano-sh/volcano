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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	nodeutil "sigs.k8s.io/descheduler/pkg/descheduler/node"
	podutil "sigs.k8s.io/descheduler/pkg/descheduler/pod"
	frameworktypes "sigs.k8s.io/descheduler/pkg/framework/types"
	resourceutil "sigs.k8s.io/descheduler/pkg/utils"
)

const LoadAwareUtilizationPluginName = "LoadAware"

// LoadAwareUtilization evicts pods from overutilized nodes to underutilized nodes. Note that CPU/Memory actual resource usage are used
// to calculate nodes' utilization and not the Request.

type LoadAwareUtilization struct {
	handle    frameworktypes.Handle
	args      *LoadAwareUtilizationArgs
	podFilter func(pod *v1.Pod) bool
	usages    []NodeUsage
}

var _ frameworktypes.BalancePlugin = &LoadAwareUtilization{}

// NewLoadAwareUtilization builds plugin from its arguments while passing a handle
func NewLoadAwareUtilization(args runtime.Object, handle frameworktypes.Handle) (frameworktypes.Plugin, error) {
	lowNodeUtilizationArgsArgs, ok := args.(*LoadAwareUtilizationArgs)
	if !ok {
		return nil, fmt.Errorf("want args to be of type LoadAwareUtilizationArgs, got %T", args)
	}

	podFilter, err := podutil.NewOptions().
		WithFilter(handle.Evictor().Filter).
		BuildFilterFunc()
	if err != nil {
		return nil, fmt.Errorf("error initializing pod filter function: %v", err)
	}
	usages := make([]NodeUsage, 0)

	return &LoadAwareUtilization{
		handle:    handle,
		args:      lowNodeUtilizationArgsArgs,
		podFilter: podFilter,
		usages:    usages,
	}, nil
}

// Name retrieves the plugin name
func (l *LoadAwareUtilization) Name() string {
	return LoadAwareUtilizationPluginName
}

// Balance extension point implementation for the plugin
func (l *LoadAwareUtilization) Balance(ctx context.Context, nodes []*v1.Node) *frameworktypes.Status {
	useDeviationThresholds := l.args.UseDeviationThresholds
	thresholds := l.args.Thresholds
	targetThresholds := l.args.TargetThresholds

	// check if CPU/Mem are set, if not, set thresholds and targetThresholds to same value, so that the unseted resource(cpu or memory) will not affect the descheduler
	if _, ok := thresholds[v1.ResourcePods]; !ok {
		if useDeviationThresholds {
			thresholds[v1.ResourcePods] = MinResourcePercentage
			targetThresholds[v1.ResourcePods] = MinResourcePercentage
		} else {
			thresholds[v1.ResourcePods] = MaxResourcePercentage
			targetThresholds[v1.ResourcePods] = MaxResourcePercentage
		}
	}
	if _, ok := thresholds[v1.ResourceCPU]; !ok {
		if useDeviationThresholds {
			thresholds[v1.ResourceCPU] = MinResourcePercentage
			targetThresholds[v1.ResourceCPU] = MinResourcePercentage
		} else {
			thresholds[v1.ResourceCPU] = MaxResourcePercentage
			targetThresholds[v1.ResourceCPU] = MaxResourcePercentage
		}
	}
	if _, ok := thresholds[v1.ResourceMemory]; !ok {
		if useDeviationThresholds {
			thresholds[v1.ResourceMemory] = MinResourcePercentage
			targetThresholds[v1.ResourceMemory] = MinResourcePercentage
		} else {
			thresholds[v1.ResourceMemory] = MaxResourcePercentage
			targetThresholds[v1.ResourceMemory] = MaxResourcePercentage
		}
	}
	resourceNames := getResourceNames(thresholds)

	l.usages = l.getNodeUsage(nodes, resourceNames, l.handle.GetPodsAssignedToNodeFunc())
	lowNodes, sourceNodes := classifyNodes(
		l.usages,
		getNodeThresholds(nodes, thresholds, targetThresholds, resourceNames, l.handle.GetPodsAssignedToNodeFunc(), useDeviationThresholds),
		// The node has to be schedulable (to be able to move workload there)
		func(node *v1.Node, usage NodeUsage, threshold NodeThresholds) bool {
			if nodeutil.IsNodeUnschedulable(node) {
				klog.V(2).InfoS("Node is unschedulable, thus not considered as underutilized", "node", klog.KObj(node))
				return false
			}
			return isNodeWithLowUtilization(usage, threshold.lowResourceThreshold)
		},
		func(node *v1.Node, usage NodeUsage, threshold NodeThresholds) bool {
			return isNodeAboveTargetUtilization(usage, threshold.highResourceThreshold)
		},
	)

	// log message for nodes with low utilization
	underutilizationCriteria := []interface{}{
		"CPU", thresholds[v1.ResourceCPU],
		"Mem", thresholds[v1.ResourceMemory],
		"Pods", thresholds[v1.ResourcePods],
	}
	for name := range thresholds {
		if !nodeutil.IsBasicResource(name) {
			underutilizationCriteria = append(underutilizationCriteria, string(name), int64(thresholds[name]))
		}
	}
	klog.V(1).InfoS("Criteria for a node under utilization", underutilizationCriteria...)
	klog.V(1).InfoS("Number of underutilized nodes", "totalNumber", len(lowNodes))

	// log message for over utilized nodes
	overutilizationCriteria := []interface{}{
		"CPU", targetThresholds[v1.ResourceCPU],
		"Mem", targetThresholds[v1.ResourceMemory],
		"Pods", targetThresholds[v1.ResourcePods],
	}
	for name := range targetThresholds {
		if !nodeutil.IsBasicResource(name) {
			overutilizationCriteria = append(overutilizationCriteria, string(name), int64(targetThresholds[name]))
		}
	}
	klog.V(1).InfoS("Criteria for a node above target utilization", overutilizationCriteria...)
	klog.V(1).InfoS("Number of overutilized nodes", "totalNumber", len(sourceNodes))

	if len(lowNodes) == 0 {
		klog.V(1).InfoS("No node is underutilized, nothing to do here, you might tune your thresholds further")
		return nil
	}

	if len(lowNodes) <= l.args.NumberOfNodes {
		klog.V(1).InfoS("Number of nodes underutilized is less or equal than NumberOfNodes, nothing to do here", "underutilizedNodes", len(lowNodes), "numberOfNodes", l.args.NumberOfNodes)
		return nil
	}

	if len(lowNodes) == len(nodes) {
		klog.V(1).InfoS("All nodes are underutilized, nothing to do here")
		return nil
	}

	if len(sourceNodes) == 0 {
		klog.V(1).InfoS("All nodes are under target utilization, nothing to do here")
		return nil
	}

	// stop if node utilization drops below target threshold or any of required capacity (cpu, memory, pods) is moved
	continueEvictionCond := func(nodeInfo NodeInfo, desNodeAvailableRes map[v1.ResourceName]*resource.Quantity) bool {
		if !isNodeAboveTargetUtilization(nodeInfo.NodeUsage, nodeInfo.thresholds.highResourceThreshold) {
			return false
		}
		for name := range desNodeAvailableRes {
			if desNodeAvailableRes[name].CmpInt64(0) < 1 {
				return false
			}
		}

		return true
	}

	// Sort the nodes by the usage in descending order
	sortNodesByUsage(sourceNodes, false)

	evictPodsFromSourceNodes(
		ctx,
		l.args.EvictableNamespaces,
		sourceNodes,
		lowNodes,
		l.handle.Evictor(),
		l.podFilter,
		resourceNames,
		continueEvictionCond)

	return nil
}

func (l *LoadAwareUtilization) PreEvictionFilter(pod *v1.Pod) bool {
	if l.args.NodeFit {
		nodes, err := nodeutil.ReadyNodes(context.TODO(), l.handle.ClientSet(), l.handle.SharedInformerFactory().Core().V1().Nodes().Lister(), l.args.NodeSelector)
		if err != nil {
			klog.ErrorS(err, "unable to list ready nodes", "pod", klog.KObj(pod))
			return false
		}
		if !l.NewPodFitsAnyOtherNode(l.handle.GetPodsAssignedToNodeFunc(), pod, nodes) {
			klog.InfoS("pod does not fit on any other node because of nodeSelector(s), Taint(s), nodes marked as unschedulable, or nodeusage is over the threshold", "pod", klog.KObj(pod))
			return false
		}
		return true
	}
	return true
}

func (l *LoadAwareUtilization) Filter(pod *v1.Pod) bool {
	return true
}

// NewPodFitsAnyOtherNode checks if the given pod will fit any of the given nodes, besides the node
// the pod is already running on. The predicates used to determine if the pod will fit can be found in the NodeFit function.
func (l *LoadAwareUtilization) NewPodFitsAnyOtherNode(nodeIndexer podutil.GetPodsAssignedToNodeFunc, pod *v1.Pod, nodes []*v1.Node) bool {
	for _, node := range nodes {
		// Skip node pod is already on
		if node.Name == pod.Spec.NodeName {
			continue
		}

		var errors []error
		fitErrors := nodeutil.NodeFit(nodeIndexer, pod, node)
		errors = append(errors, fitErrors...)
		usageErrors := l.checkNodeUsage(pod, node)
		errors = append(errors, usageErrors...)
		if len(errors) == 0 {
			klog.V(1).InfoS("Pod fits on node", "pod", klog.KObj(pod), "node", klog.KObj(node))
			return true
		}
		klog.V(1).InfoS("Pod does not fit on node",
			"pod:", klog.KObj(pod), "node:", klog.KObj(node), "error:", utilerrors.NewAggregate(errors).Error())
	}

	return false
}

// Check whether the resources requested by the pod on the node exceed the threshold.
func (l *LoadAwareUtilization) checkNodeUsage(pod *v1.Pod, node *v1.Node) []error {
	var errors []error

	nodeUsage := NodeUsage{}
	for _, usage := range l.usages {
		if usage.node.Name == node.Name {
			nodeUsage = usage
			break
		}
	}

	if nodeUsage.overUseResources == nil {
		return errors
	}

	// Relationship between nodeUsage.overUseResources and pod resource request
	if resourceutil.GetResourceRequest(pod, v1.ResourceCPU) >= 0 {
		for _, value := range *nodeUsage.overUseResources {
			if value == v1.ResourceCPU {
				errors = append(errors, fmt.Errorf("node's cpu usage is over the threshold, request cpu:%v, overUseResources:%v", resourceutil.GetResourceRequest(pod, v1.ResourceCPU), value))
			}
		}
	}
	if resourceutil.GetResourceRequest(pod, v1.ResourceMemory) >= 0 {
		for _, value := range *nodeUsage.overUseResources {
			if value == v1.ResourceMemory {
				errors = append(errors, fmt.Errorf("node's memory usage is over the threshold,request memory:%v, overUseResources:%v", resourceutil.GetResourceRequest(pod, v1.ResourceMemory), value))
			}
		}
	}

	return errors
}
