/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Migrated from custom predicate logic to Kubernetes native scheduler plugins (NodeAffinity, NodePorts, InterPodAffinity, VolumeBinding, DRA, etc.) for better compatibility
- Added multiple extension points: PrePredicate, BatchNodeOrder, SimulateAddTask, SimulateRemoveTask, etc.

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

package predicates

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeports"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeunschedulable"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodevolumelimits"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumezone"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	vbcap "volcano.sh/volcano/pkg/scheduler/capabilities/volumebinding"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodescore"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "predicates"

	// NodeAffinityEnable is the key for enabling Node Affinity Predicates in scheduler configmap
	NodeAffinityEnable = "predicate.NodeAffinityEnable"

	// NodePortsEnable is the key for enabling Node Port Predicates in scheduler configmap
	NodePortsEnable = "predicate.NodePortsEnable"

	// TaintTolerationEnable is the key for enabling Taint Toleration Predicates in scheduler configmap
	TaintTolerationEnable = "predicate.TaintTolerationEnable"

	// PodAffinityEnable is the key for enabling Pod Affinity Predicates in scheduler configmap
	PodAffinityEnable = "predicate.PodAffinityEnable"

	// NodeVolumeLimitsEnable is the key for enabling Node Volume Limits Predicates in scheduler configmap
	NodeVolumeLimitsEnable = "predicate.NodeVolumeLimitsEnable"

	// VolumeZoneEnable is the key for enabling Volume Zone Predicates in scheduler configmap
	VolumeZoneEnable = "predicate.VolumeZoneEnable"

	// PodTopologySpreadEnable is the key for enabling Pod Topology Spread Predicates in scheduler configmap
	PodTopologySpreadEnable = "predicate.PodTopologySpreadEnable"

	// VolumeBindingEnable is the key for enabling Volume Binding Predicates in scheduler configmap
	VolumeBindingEnable = "predicate.VolumeBindingEnable"

	// DynamicResourceAllocationEnable is the key for enabling Dynamic Resource Allocation Predicates in scheduler configmap
	DynamicResourceAllocationEnable = "predicate.DynamicResourceAllocationEnable"

	// CachePredicate control cache predicate feature
	CachePredicate = "predicate.CacheEnable"

	// ProportionalPredicate is the key for enabling Proportional Predicate in YAML
	ProportionalPredicate = "predicate.ProportionalEnable"
	// ProportionalResource is the key for additional resource key name
	ProportionalResource = "predicate.resources"
	// ProportionalResourcesPrefix is the key prefix for additional resource key name
	ProportionalResourcesPrefix = ProportionalResource + "."
)

var (
	volumeBindingPluginInstance *vbcap.VolumeBinding
	volumeBindingPluginOnce     sync.Once
)

type predicatesPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	// The VolumeBindingPlugin needs to store to execute in PreBind and PreBindRollBack
	volumeBindingPlugin *vbcap.VolumeBinding
	// The DynamicResourceAllocationPlugin needs to store to execute in PreBind and PreBindRollBack
	dynamicResourceAllocationPlugin *dynamicresources.DynamicResources
}

// New return predicate plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &predicatesPlugin{pluginArguments: arguments}
}

func (pp *predicatesPlugin) Name() string {
	return PluginName
}

type baseResource struct {
	CPU    float64
	Memory float64
}

type predicateEnable struct {
	nodeAffinityEnable              bool
	nodePortEnable                  bool
	taintTolerationEnable           bool
	podAffinityEnable               bool
	nodeVolumeLimitsEnable          bool
	volumeZoneEnable                bool
	podTopologySpreadEnable         bool
	cacheEnable                     bool
	proportionalEnable              bool
	volumeBindingEnable             bool
	dynamicResourceAllocationEnable bool
	proportional                    map[v1.ResourceName]baseResource
}

// bind context extension information of predicates
type bindContextExtension struct {
	State *k8sframework.CycleState
}

func enablePredicate(args framework.Arguments) predicateEnable {
	/*
	   User Should give predicatesEnable in this format(predicate.GPUSharingEnable).
	   Currently supported only GPUSharing predicate checks.

	   actions: "reclaim, allocate, backfill, preempt"
	   tiers:
	   - plugins:
	     - name: priority
	     - name: gang
	     - name: conformance
	   - plugins:
	     - name: drf
	     - name: predicates
	       arguments:
	         predicate.NodeAffinityEnable: true
	         predicate.NodePortsEnable: true
	         predicate.TaintTolerationEnable: true
	         predicate.PodAffinityEnable: true
	         predicate.NodeVolumeLimitsEnable: true
	         predicate.VolumeZoneEnable: true
	         predicate.PodTopologySpreadEnable: true
	         predicate.GPUSharingEnable: true
	         predicate.GPUNumberEnable: true
	         predicate.CacheEnable: true
	         predicate.ProportionalEnable: true
	         predicate.resources: nvidia.com/gpu
	         predicate.resources.nvidia.com/gpu.cpu: 4
	         predicate.resources.nvidia.com/gpu.memory: 8
	     - name: proportion
	     - name: nodeorder
	*/

	predicate := predicateEnable{
		nodeAffinityEnable:              true,
		nodePortEnable:                  true,
		taintTolerationEnable:           true,
		podAffinityEnable:               true,
		nodeVolumeLimitsEnable:          true,
		volumeZoneEnable:                true,
		podTopologySpreadEnable:         true,
		cacheEnable:                     false,
		proportionalEnable:              false,
		volumeBindingEnable:             true,
		dynamicResourceAllocationEnable: false,
	}

	// Checks whether predicate enable args is provided or not.
	// If args were given by scheduler configmap, cover the values in predicateEnable struct.
	args.GetBool(&predicate.nodeAffinityEnable, NodeAffinityEnable)
	args.GetBool(&predicate.nodePortEnable, NodePortsEnable)
	args.GetBool(&predicate.taintTolerationEnable, TaintTolerationEnable)
	args.GetBool(&predicate.podAffinityEnable, PodAffinityEnable)
	args.GetBool(&predicate.nodeVolumeLimitsEnable, NodeVolumeLimitsEnable)
	args.GetBool(&predicate.volumeZoneEnable, VolumeZoneEnable)
	args.GetBool(&predicate.podTopologySpreadEnable, PodTopologySpreadEnable)
	args.GetBool(&predicate.volumeBindingEnable, VolumeBindingEnable)
	args.GetBool(&predicate.dynamicResourceAllocationEnable, DynamicResourceAllocationEnable)

	args.GetBool(&predicate.cacheEnable, CachePredicate)
	// Checks whether predicate.ProportionalEnable is provided or not, if given, modifies the value in predicateEnable struct.
	args.GetBool(&predicate.proportionalEnable, ProportionalPredicate)
	resourcesProportional := make(map[v1.ResourceName]baseResource)
	resourcesStr, ok := args[ProportionalResource].(string)
	if !ok {
		resourcesStr = ""
	}
	resources := strings.Split(resourcesStr, ",")
	for _, resource := range resources {
		resource = strings.TrimSpace(resource)
		if resource == "" {
			continue
		}
		// proportional.resources.[ResourceName]
		cpuResourceKey := ProportionalResourcesPrefix + resource + ".cpu"
		cpuResourceRate := 1.0
		args.GetFloat64(&cpuResourceRate, cpuResourceKey)
		if cpuResourceRate < 0 {
			cpuResourceRate = 1.0
		}
		memoryResourceKey := ProportionalResourcesPrefix + resource + ".memory"
		memoryResourceRate := 1.0
		args.GetFloat64(&memoryResourceRate, memoryResourceKey)
		if memoryResourceRate < 0 {
			memoryResourceRate = 1.0
		}
		r := baseResource{
			CPU:    cpuResourceRate,
			Memory: memoryResourceRate,
		}
		resourcesProportional[v1.ResourceName(resource)] = r
	}
	predicate.proportional = resourcesProportional

	return predicate
}

func (pp *predicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := ssn.PodLister
	nodeMap := ssn.NodeMap

	pCache := predicateCacheNew()
	predicate := enablePredicate(pp.pluginArguments)

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, allocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, event.Task.NodeName)
			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Errorf("predicates, update pod %s/%s allocate to NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
				return
			}
			nodeInfo, ok := ssn.Nodes[nodeName]
			if !ok {
				klog.Errorf("Failed to get node %s info from cache", nodeName)
				return
			}
			// run reserve plugins
			pp.runReservePlugins(ssn, event)
			//predicate gpu sharing
			for _, val := range api.RegisteredDevices {
				if devices, ok := nodeInfo.Others[val].(api.Devices); ok {
					if !devices.HasDeviceRequest(pod) {
						continue
					}

					err := devices.Allocate(ssn.KubeClient(), pod)
					if err != nil {
						klog.Errorf("AllocateToPod failed %s", err.Error())
						return
					}
				} else {
					klog.Warningf("Devices %s assertion conversion failed, skip", val)
				}
			}
			node.AddPod(pod)
			klog.V(4).Infof("predicates, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, nodeName)
		},
		DeallocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, deallocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, "")
			nodeName := event.Task.NodeName
			node, found := nodeMap[nodeName]
			if !found {
				klog.Errorf("predicates, update pod %s/%s allocate from NOT EXIST node [%s]", pod.Namespace, pod.Name, nodeName)
				return
			}

			nodeInfo, ok := ssn.Nodes[nodeName]
			if !ok {
				klog.Errorf("Failed to get node %s info from cache", nodeName)
				return
			}

			// run unReserve plugins
			pp.runUnReservePlugins(ssn, event)

			for _, val := range api.RegisteredDevices {
				if devices, ok := nodeInfo.Others[val].(api.Devices); ok {
					if !devices.HasDeviceRequest(pod) {
						continue
					}

					// deallocate pod gpu id
					err := devices.Release(ssn.KubeClient(), pod)
					if err != nil {
						klog.Errorf("Device %s release failed for pod %s/%s, err:%s", val, pod.Namespace, pod.Name, err.Error())
						return
					}
				} else {
					klog.Warningf("Devices %s assertion conversion failed, skip", val)
				}
			}

			err := node.RemovePod(klog.FromContext(context.TODO()), pod)
			if err != nil {
				klog.Errorf("predicates, remove pod %s/%s from node [%s] error: %v", pod.Namespace, pod.Name, nodeName, err)
				return
			}
			klog.V(4).Infof("predicates, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
		},
	})

	features := feature.Features{
		EnableStorageCapacityScoring:                 utilFeature.DefaultFeatureGate.Enabled(features.StorageCapacityScoring),
		EnableNodeInclusionPolicyInPodTopologySpread: utilFeature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      utilFeature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
		EnableSidecarContainers:                      utilFeature.DefaultFeatureGate.Enabled(features.SidecarContainers),
		EnableDRAAdminAccess:                         utilFeature.DefaultFeatureGate.Enabled(features.DRAAdminAccess),
		EnableDynamicResourceAllocation:              utilFeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation),
	}
	// Initialize k8s plugins
	// TODO: Add more predicates, k8s.io/kubernetes/pkg/scheduler/framework/plugins/legacy_registry.go
	handle := k8s.NewFrameworkHandle(nodeMap, ssn.KubeClient(), ssn.InformerFactory(),
		k8s.WithSharedDRAManager(ssn.SharedDRAManager()))
	// 1. NodeUnschedulable
	plugin, _ := nodeunschedulable.New(context.TODO(), nil, handle, features)
	nodeUnscheduleFilter := plugin.(*nodeunschedulable.NodeUnschedulable)
	// 2. NodeAffinity
	nodeAffinityArgs := config.NodeAffinityArgs{
		AddedAffinity: &v1.NodeAffinity{},
	}
	plugin, _ = nodeaffinity.New(context.TODO(), &nodeAffinityArgs, handle, features)
	nodeAffinityFilter := plugin.(*nodeaffinity.NodeAffinity)
	// 3. NodePorts
	plugin, _ = nodeports.New(context.TODO(), nil, handle, features)
	nodePortFilter := plugin.(*nodeports.NodePorts)
	// 4. TaintToleration
	plugin, _ = tainttoleration.New(context.TODO(), nil, handle, features)
	tolerationFilter := plugin.(*tainttoleration.TaintToleration)
	// 5. InterPodAffinity
	plArgs := &config.InterPodAffinityArgs{}
	plugin, _ = interpodaffinity.New(context.TODO(), plArgs, handle, features)
	podAffinityFilter := plugin.(*interpodaffinity.InterPodAffinity)
	// 6. NodeVolumeLimits
	plugin, _ = nodevolumelimits.NewCSI(context.TODO(), nil, handle, features)
	nodeVolumeLimitsCSIFilter := plugin.(*nodevolumelimits.CSILimits)
	// 7. VolumeZone
	plugin, _ = volumezone.New(context.TODO(), nil, handle, features)
	volumeZoneFilter := plugin.(*volumezone.VolumeZone)
	// 8. PodTopologySpread
	// Setting cluster level default constraints is not support for now.
	ptsArgs := &config.PodTopologySpreadArgs{DefaultingType: config.SystemDefaulting}
	plugin, _ = podtopologyspread.New(context.TODO(), ptsArgs, handle, features)
	podTopologySpreadFilter := plugin.(*podtopologyspread.PodTopologySpread)
	// 9. VolumeBinding
	vbArgs := defaultVolumeBindingArgs()
	if predicate.volumeBindingEnable {
		// Currently, we support initializing the VolumeBinding plugin once, but do not support hot loading the plugin after modifying the VolumeBinding parameters.
		// This is because VolumeBinding involves AssumeCache, which contains eventHandler. If VolumeBinding is initialized multiple times,
		// eventHandler will be added continuously, causing memory leaks. For details, see: https://github.com/volcano-sh/volcano/issues/2554.
		// Therefore, if users need to modify the VolumeBinding parameters, users need to restart the scheduler.
		volumeBindingPluginOnce.Do(func() {
			setUpVolumeBindingArgs(vbArgs, pp.pluginArguments)

			var err error
			plugin, err = vbcap.New(context.TODO(), vbArgs.VolumeBindingArgs, handle, features)
			if err != nil {
				klog.Fatalf("failed to create volume binding plugin with args %+v: %v", vbArgs, err)
			}
			volumeBindingPluginInstance = plugin.(*vbcap.VolumeBinding)
		})

		pp.volumeBindingPlugin = volumeBindingPluginInstance
	}
	// 10. DRA
	var dynamicResourceAllocationPlugin *dynamicresources.DynamicResources
	if predicate.dynamicResourceAllocationEnable {
		var err error
		plugin, err = dynamicresources.New(context.TODO(), nil, handle, features)
		if err != nil {
			klog.Fatalf("failed to create dra plugin with err: %v", err)
		}
		dynamicResourceAllocationPlugin = plugin.(*dynamicresources.DynamicResources)
		pp.dynamicResourceAllocationPlugin = dynamicResourceAllocationPlugin
	}

	ssn.AddPrePredicateFn(pp.Name(), func(task *api.TaskInfo) error {
		// It is safe here to directly use the state to run plugins because we have already initialized the cycle state
		// for each pending pod when open session and will not meet nil state
		state := ssn.GetCycleState(task.UID)
		// Check NodePorts
		if predicate.nodePortEnable {
			_, status := nodePortFilter.PreFilter(context.TODO(), state, task.Pod)
			if err := handleSkipPrePredicatePlugin(status, state, task, nodeports.Name); err != nil {
				return err
			}
		}
		// Check restartable container
		if !features.EnableSidecarContainers && task.HasRestartableInitContainer {
			// Scheduler will calculate resources usage for a Pod containing
			// restartable init containers that will be equal or more than kubelet will
			// require to run the Pod. So there will be no overbooking. However, to
			// avoid the inconsistency in resource calculation between the scheduler
			// and the older (before v1.28) kubelet, make the Pod unschedulable.
			return fmt.Errorf("pod has a restartable init container and the SidecarContainers feature is disabled")
		}

		// InterPodAffinity Predicate
		// TODO: Update the node information to be processed by the filer based on the node list returned by the prefilter.
		// In K8S V1.25, the return value result is added to the Prefile interface,
		// indicating the list of nodes that meet filtering conditions.
		// If the value of result is nil, all nodes meet the conditions.
		// If the specified node information exists, only the node information in result meets the conditions.
		// The value of Prefile in the current InterPodAffinity package always returns nil.
		// The outer layer does not need to be processed temporarily.
		// If the filtering logic is added to the Prefile node in the Volumebinding package in the future,
		// the processing logic needs to be added to the return value result.
		if predicate.podAffinityEnable {
			klog.Infof("Executing podAffinityFilter PreFilter for task %s/%s", task.Namespace, task.Name)
			_, status := podAffinityFilter.PreFilter(context.TODO(), state, task.Pod)
			if err := handleSkipPrePredicatePlugin(status, state, task, interpodaffinity.Name); err != nil {
				return err
			}
		}

		// Check PodTopologySpread
		// TODO: Update the node information to be processed by the filer based on the node list returned by the prefilter.
		// In K8S V1.25, the return value result is added to the Prefile interface,
		// indicating the list of nodes that meet filtering conditions.
		// If the value of result is nil, all nodes meet the conditions.
		// If the specified node information exists, only the node information in result meets the conditions.
		// The value of Prefile in the current PodTopologySpread package always returns nil.
		// The outer layer does not need to be processed temporarily.
		// If the filtering logic is added to the Prefile node in the Volumebinding package in the future,
		// the processing logic needs to be added to the return value result.
		if predicate.podTopologySpreadEnable {
			_, status := podTopologySpreadFilter.PreFilter(context.TODO(), state, task.Pod)
			if err := handleSkipPrePredicatePlugin(status, state, task, podTopologySpreadFilter.Name()); err != nil {
				return err
			}
		}

		// VolumeBinding Predicate
		if predicate.volumeBindingEnable {
			_, status := pp.volumeBindingPlugin.PreFilter(context.TODO(), state, task.Pod)
			if err := handleSkipPrePredicatePlugin(status, state, task, pp.volumeBindingPlugin.Name()); err != nil {
				return err
			}
		}

		// DRA Predicate
		if predicate.dynamicResourceAllocationEnable {
			_, status := pp.dynamicResourceAllocationPlugin.PreFilter(context.TODO(), state, task.Pod)
			if err := handleSkipPrePredicatePlugin(status, state, task, pp.dynamicResourceAllocationPlugin.Name()); err != nil {
				return err
			}
		}

		return nil
	})

	ssn.AddPredicateFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		state := ssn.GetCycleState(task.UID)
		predicateStatus := make([]*api.Status, 0)
		nodeInfo, found := nodeMap[node.Name]
		if !found {
			klog.V(4).Infof("NodeInfo predicates Task <%s/%s> on Node <%s> failed, node info not found",
				task.Namespace, task.Name, node.Name)
			nodeInfoStatus := &api.Status{
				Code:   api.Error,
				Reason: "node info not found",
				Plugin: pp.Name(),
			}
			predicateStatus = append(predicateStatus, nodeInfoStatus)
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}

		if node.Allocatable.MaxTaskNum <= len(nodeInfo.Pods) {
			klog.V(4).Infof("NodePodNumber predicates Task <%s/%s> on Node <%s> failed, allocatable <%d>, existed <%d>",
				task.Namespace, task.Name, node.Name, node.Allocatable.MaxTaskNum, len(nodeInfo.Pods))
			podsNumStatus := &api.Status{
				Code:   api.Unschedulable,
				Reason: api.NodePodNumberExceeded,
				Plugin: pp.Name(),
			}
			predicateStatus = append(predicateStatus, podsNumStatus)
		}

		predicateByStablefilter := func(nodeInfo *k8sframework.NodeInfo) ([]*api.Status, bool, error) {
			// CheckNodeUnschedulable
			predicateStatus := make([]*api.Status, 0)
			status := nodeUnscheduleFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			nodeUnscheduleStatus := api.ConvertPredicateStatus(status)
			if nodeUnscheduleStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, nodeUnscheduleStatus)
				if ShouldAbort(nodeUnscheduleStatus) {
					return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", nodeUnscheduleFilter.Name(), status.Message())
				}
			}

			// Check NodeAffinity
			if predicate.nodeAffinityEnable {
				status := nodeAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				nodeAffinityStatus := api.ConvertPredicateStatus(status)
				if nodeAffinityStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, nodeAffinityStatus)
					if ShouldAbort(nodeAffinityStatus) {
						return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", nodeAffinityFilter.Name(), status.Message())
					}
				}
			}

			// PodToleratesNodeTaints: TaintToleration
			if predicate.taintTolerationEnable {
				status := tolerationFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				tolerationStatus := api.ConvertPredicateStatus(status)
				if tolerationStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, tolerationStatus)
					if ShouldAbort(tolerationStatus) {
						return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", tolerationFilter.Name(), status.Message())
					}
				}
			}

			return predicateStatus, true, nil
		}

		// Check PredicateWithCache
		var err error
		var fit bool
		predicateCacheStatus := make([]*api.Status, 0)
		if predicate.cacheEnable {
			fit, err = pCache.PredicateWithCache(node.Name, task.Pod)
			if err != nil {
				predicateCacheStatus, fit, _ = predicateByStablefilter(nodeInfo)
				pCache.UpdateCache(node.Name, task.Pod, fit)
			} else {
				if !fit {
					err = fmt.Errorf("plugin equivalence cache predicates failed")
					predicateCacheStatus = append(predicateCacheStatus, &api.Status{
						Code: api.Error, Reason: err.Error(), Plugin: CachePredicate,
					})
				}
			}
		} else {
			predicateCacheStatus, fit, _ = predicateByStablefilter(nodeInfo)
		}

		predicateStatus = append(predicateStatus, predicateCacheStatus...)
		if !fit {
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}

		// Check NodePort
		if predicate.nodePortEnable {
			isSkipNodePorts := handleSkipPredicatePlugin(state, nodePortFilter.Name())
			if !isSkipNodePorts {
				status := nodePortFilter.Filter(context.TODO(), state, nil, nodeInfo)
				nodePortStatus := api.ConvertPredicateStatus(status)
				if nodePortStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, nodePortStatus)
					if ShouldAbort(nodePortStatus) {
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
				}
			}
		}

		// Check PodAffinity
		if predicate.podAffinityEnable {
			isSkipInterPodAffinity := handleSkipPredicatePlugin(state, podAffinityFilter.Name())
			if !isSkipInterPodAffinity {
				status := podAffinityFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				podAffinityStatus := api.ConvertPredicateStatus(status)
				if podAffinityStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, podAffinityStatus)
					if ShouldAbort(podAffinityStatus) {
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
				}
			}
		}

		// Check NodeVolumeLimits
		if predicate.nodeVolumeLimitsEnable {
			status := nodeVolumeLimitsCSIFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			nodeVolumeStatus := api.ConvertPredicateStatus(status)
			if nodeVolumeStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, nodeVolumeStatus)
				if ShouldAbort(nodeVolumeStatus) {
					return api.NewFitErrWithStatus(task, node, predicateStatus...)
				}
			}
		}

		// Check VolumeZone
		if predicate.volumeZoneEnable {
			status := volumeZoneFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
			volumeZoneStatus := api.ConvertPredicateStatus(status)
			if volumeZoneStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, volumeZoneStatus)
				if ShouldAbort(volumeZoneStatus) {
					return api.NewFitErrWithStatus(task, node, predicateStatus...)
				}
			}
		}

		// Check PodTopologySpread
		if predicate.podTopologySpreadEnable {
			isSkipPodTopologySpreadFilter := handleSkipPredicatePlugin(state, podTopologySpreadFilter.Name())
			if !isSkipPodTopologySpreadFilter {
				status := podTopologySpreadFilter.Filter(context.TODO(), state, task.Pod, nodeInfo)
				podTopologyStatus := api.ConvertPredicateStatus(status)
				if podTopologyStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, podTopologyStatus)
					if ShouldAbort(podTopologyStatus) {
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
				}
			}
		}

		if predicate.proportionalEnable {
			// Check ProportionalPredicate
			proportionalStatus, _ := checkNodeResourceIsProportional(task, node, predicate.proportional)
			if proportionalStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, proportionalStatus)
				if ShouldAbort(proportionalStatus) {
					return api.NewFitErrWithStatus(task, node, predicateStatus...)
				}
			}
			klog.V(4).Infof("checkNodeResourceIsProportional predicates Task <%s/%s> on Node <%s>: fit %v",
				task.Namespace, task.Name, node.Name, fit)
		}

		// Check VolumeBinding
		if predicate.volumeBindingEnable {
			isSkipVolumeBinding := handleSkipPredicatePlugin(state, pp.volumeBindingPlugin.Name())
			if !isSkipVolumeBinding {
				status := pp.volumeBindingPlugin.Filter(context.TODO(), state, task.Pod, nodeInfo)
				volumeBindingStatus := api.ConvertPredicateStatus(status)
				if volumeBindingStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, volumeBindingStatus)
					if ShouldAbort(volumeBindingStatus) {
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
				}
			}
		}

		// Check DRA
		if predicate.dynamicResourceAllocationEnable {
			isSkipDRA := handleSkipPredicatePlugin(state, pp.dynamicResourceAllocationPlugin.Name())
			if !isSkipDRA {
				status := pp.dynamicResourceAllocationPlugin.Filter(context.TODO(), state, task.Pod, nodeInfo)
				dynamicResourceAllocationStatus := api.ConvertPredicateStatus(status)
				if dynamicResourceAllocationStatus.Code != api.Success {
					predicateStatus = append(predicateStatus, dynamicResourceAllocationStatus)
					if ShouldAbort(dynamicResourceAllocationStatus) {
						return api.NewFitErrWithStatus(task, node, predicateStatus...)
					}
				}
			}
		}

		if len(predicateStatus) > 0 {
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}

		return nil
	})

	// TODO: Need to unify the plugins in nodeorder to predicates.
	// Currently, if the volumebinding plugin crosses predicates plugins and nodeorder plugins, it involves two initializations, which increases memory overhead.
	// Therefore, a BatchNodeOrder extension point needs to be added in here, as volumebinding involves PreScore and Score extension points
	ssn.AddBatchNodeOrderFn(pp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		state := ssn.GetCycleState(task.UID)
		nodeList := slices.Collect(maps.Values(ssn.NodeMap))
		nodeScores := make(map[string]float64, len(nodeList))

		volumeBindingScores, err := volumeBindingScore(predicate.volumeBindingEnable, pp.volumeBindingPlugin, state, task.Pod, nodeList, vbArgs.Weight)
		if err != nil {
			return nil, err
		}

		for _, node := range nodes {
			nodeScores[node.Name] += volumeBindingScores[node.Name]
		}

		klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, nodeScores)
		return nodeScores, nil
	})

	ssn.RegisterBinder(pp.Name(), pp)

	// Add SimulateAddTask function
	ssn.AddSimulateAddTaskFn(pp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, taskToSchedule *api.TaskInfo, taskToAdd *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		podInfoToAdd, err := k8sframework.NewPodInfo(taskToAdd.Pod)
		if err != nil {
			return fmt.Errorf("failed to create pod info: %w", err)
		}

		k8sNodeInfo := k8sframework.NewNodeInfo(nodeInfo.Pods()...)
		k8sNodeInfo.SetNode(nodeInfo.Node)

		if predicate.podAffinityEnable {
			isSkipInterPodAffinity := handleSkipPredicatePlugin(cycleState, podAffinityFilter.Name())
			if !isSkipInterPodAffinity {
				status := podAffinityFilter.AddPod(ctx, cycleState, taskToSchedule.Pod, podInfoToAdd, k8sNodeInfo)
				if !status.IsSuccess() {
					return fmt.Errorf("failed to remove pod from node %s: %w", nodeInfo.Name, status.AsError())
				}
			}
		}

		return nil
	})

	// Add SimulateRemoveTask function
	ssn.AddSimulateRemoveTaskFn(pp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, taskToSchedule *api.TaskInfo, taskToRemove *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		podInfoToRemove, err := k8sframework.NewPodInfo(taskToRemove.Pod)
		if err != nil {
			return fmt.Errorf("failed to create pod info: %w", err)
		}

		k8sNodeInfo := k8sframework.NewNodeInfo(nodeInfo.Pods()...)
		k8sNodeInfo.SetNode(nodeInfo.Node)

		if predicate.podAffinityEnable {
			isSkipInterPodAffinity := handleSkipPredicatePlugin(cycleState, podAffinityFilter.Name())
			if !isSkipInterPodAffinity {
				status := podAffinityFilter.RemovePod(ctx, cycleState, taskToSchedule.Pod, podInfoToRemove, k8sNodeInfo)
				if !status.IsSuccess() {
					return fmt.Errorf("failed to remove pod from node %s: %w", nodeInfo.Name, status.AsError())
				}
			}
		}
		return nil
	})

	// Add SimulatePredicate function
	ssn.AddSimulatePredicateFn(pp.Name(), func(ctx context.Context, cycleState *k8sframework.CycleState, task *api.TaskInfo, node *api.NodeInfo) error {
		k8sNodeInfo := k8sframework.NewNodeInfo(node.Pods()...)
		k8sNodeInfo.SetNode(node.Node)

		if predicate.podAffinityEnable {
			isSkipInterPodAffinity := handleSkipPredicatePlugin(cycleState, podAffinityFilter.Name())
			if !isSkipInterPodAffinity {
				status := podAffinityFilter.Filter(ctx, cycleState, task.Pod, k8sNodeInfo)

				if !status.IsSuccess() {
					return fmt.Errorf("failed to filter pod on node %s: %w", node.Name, status.AsError())
				} else {
					klog.Infof("pod affinity for task %s/%s filter success on node %s", task.Namespace, task.Name, node.Name)
				}
			}
		}
		return nil
	})
}

func (pp *predicatesPlugin) runReservePlugins(ssn *framework.Session, event *framework.Event) {
	state := ssn.GetCycleState(event.Task.UID)

	// Volume Binding Reserve
	if pp.volumeBindingPlugin != nil {
		status := pp.volumeBindingPlugin.Reserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			event.Err = status.AsError()
			return
		}
	}

	// DRA Reserve
	if pp.dynamicResourceAllocationPlugin != nil {
		status := pp.dynamicResourceAllocationPlugin.Reserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			event.Err = status.AsError()
			return
		}
	}
}

func (pp *predicatesPlugin) runUnReservePlugins(ssn *framework.Session, event *framework.Event) {
	state := ssn.GetCycleState(event.Task.UID)

	// Volume Binding UnReserve
	if pp.volumeBindingPlugin != nil {
		pp.volumeBindingPlugin.Unreserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
	}

	// DRA UnReserve
	if pp.dynamicResourceAllocationPlugin != nil {
		pp.dynamicResourceAllocationPlugin.Unreserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
	}
}

// needsPreBind judges whether the pod needs set up extension information in bind context
// to execute extra extension points like PreBind.
// Currently, if a pod has any of the following resources, we assume that the pod needs to pass extension information:
// 1. With volumes
// 2. With resourceClaims
// ...
// If a pod doesn't contain any of the above resources, the pod doesn't need to set up extension information in bind context.
// If necessary, more refined filtering can be added in this func.
func (pp *predicatesPlugin) needsPreBind(task *api.TaskInfo) bool {
	// 1. With volumes
	if pp.volumeBindingPlugin != nil {
		for _, vol := range task.Pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil || vol.Ephemeral != nil {
				return true
			}
		}
	}

	// 2. With resourceClaims
	if pp.dynamicResourceAllocationPlugin != nil {
		if len(task.Pod.Spec.ResourceClaims) > 0 {
			return true
		}
	}

	return false
}

func (pp *predicatesPlugin) PreBind(ctx context.Context, bindCtx *cache.BindContext) error {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return nil
	}

	state := bindCtx.Extensions[pp.Name()].(*bindContextExtension).State

	// VolumeBinding PreBind
	if pp.volumeBindingPlugin != nil {
		status := pp.volumeBindingPlugin.PreBind(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			return status.AsError()
		}
	}

	// DRA PreBind
	if pp.dynamicResourceAllocationPlugin != nil {
		status := pp.dynamicResourceAllocationPlugin.PreBind(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			return status.AsError()
		}
	}

	return nil
}

func (pp *predicatesPlugin) PreBindRollBack(ctx context.Context, bindCtx *cache.BindContext) {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	state := bindCtx.Extensions[pp.Name()].(*bindContextExtension).State

	// VolumeBinding UnReserve
	if pp.volumeBindingPlugin != nil {
		pp.volumeBindingPlugin.Unreserve(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
	}

	// DRA UnReserve
	if pp.dynamicResourceAllocationPlugin != nil {
		pp.dynamicResourceAllocationPlugin.Unreserve(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
	}
}

func (pp *predicatesPlugin) SetupBindContextExtension(ssn *framework.Session, bindCtx *cache.BindContext) {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	bindCtx.Extensions[pp.Name()] = &bindContextExtension{State: ssn.GetCycleState(bindCtx.TaskInfo.UID)}
}

// ShouldAbort determines if the given status indicates that execution should be aborted.
// It checks if the status code corresponds to any of the following conditions:
// - UnschedulableAndUnresolvable: Indicates the task cannot be scheduled and resolved.
// - Error: Represents an error state that prevents further execution.
// - Wait: Suggests that the process should pause and not proceed further.
// - Skip: Indicates that the operation should be skipped entirely.
//
// Parameters:
// - status (*api.Status): The status object to evaluate.
//
// Returns:
// - bool: True if the status code matches any of the abort conditions; false otherwise.
func ShouldAbort(status *api.Status) bool {
	return status.Code == api.UnschedulableAndUnresolvable ||
		status.Code == api.Error ||
		status.Code == api.Wait ||
		status.Code == api.Skip
}

func handleSkipPredicatePlugin(state *k8sframework.CycleState, pluginName string) bool {
	return state.SkipFilterPlugins.Has(pluginName)
}

func handleSkipPrePredicatePlugin(status *k8sframework.Status, state *k8sframework.CycleState, task *api.TaskInfo, pluginName string) error {
	if state.SkipFilterPlugins == nil {
		state.SkipFilterPlugins = sets.New[string]()
	}

	if status.IsSkip() {
		state.SkipFilterPlugins.Insert(pluginName)
		klog.V(5).Infof("The predicate of plugin %s will skip execution for pod <%s/%s>, because the status returned by pre-predicate is skip",
			pluginName, task.Namespace, task.Name)
	} else if !status.IsSuccess() {
		return fmt.Errorf("plugin %s pre-predicates failed %s", pluginName, status.Message())
	}

	return nil
}

func volumeBindingScore(
	volumeBindingEnable bool,
	volumeBinding *vbcap.VolumeBinding,
	cycleState *k8sframework.CycleState,
	pod *v1.Pod,
	nodeInfos []*k8sframework.NodeInfo,
	volumeBindingWeight int,
) (map[string]float64, error) {
	if !volumeBindingEnable {
		return map[string]float64{}, nil
	}
	return nodescore.CalculatePluginScore(volumeBinding.Name(), volumeBinding, &nodescore.EmptyNormalizer{},
		cycleState, pod, nodeInfos, volumeBindingWeight)
}

func (pp *predicatesPlugin) OnSessionClose(ssn *framework.Session) {}

// ResetVolumeBindingPluginForTest resets the volumeBindingPluginInstance and volumeBindingPluginOnce for testing purposes only.
// This function is necessary because volumeBindingPluginInstance is a global variable initialized with sync.Once,
// which ensures it's only initialized once. However, in test environments, especially when running multiple tests that use the predicates plugin,
// we need to reset this instance to ensure each test has a clean state with its own PVC/PV configurations.
// Without this reset, tests that run later would use the volumeBindingPlugin instance initialized by earlier tests,
// which may not have the correct PVC/PV information for the current test, leading to failures.
//
// WARNING: This function should NEVER be used in production code, only in tests.
func ResetVolumeBindingPluginForTest() {
	volumeBindingPluginInstance = nil
	volumeBindingPluginOnce = sync.Once{}
}
