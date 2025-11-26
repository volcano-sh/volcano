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
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	utilFeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
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
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
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
)

var (
	volumeBindingPluginInstance *vbcap.VolumeBinding
	volumeBindingPluginOnce     sync.Once
)

type PredicatesPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments

	enabledPredicates predicateEnable

	features feature.Features

	FilterPlugins       map[string]k8sframework.FilterPlugin
	StableFilterPlugins map[string]k8sframework.FilterPlugin // Subset of FilterPlugins for cache-stable filters
	PrefilterPlugins    map[string]k8sframework.PreFilterPlugin
	ReservePlugins      map[string]k8sframework.ReservePlugin
	PreBindPlugins      map[string]k8sframework.PreBindPlugin
	ScorePlugins        map[string]nodescore.BaseScorePlugin
	ScoreWeights        map[string]int // Weight for each score plugin
	PredicateCache      *predicateCache
	Handle              k8sframework.Handle
}

// New return predicate plugin
func New(arguments framework.Arguments) framework.Plugin {
	predicate := predicateEnable{
		nodeAffinityEnable:              true,
		nodePortEnable:                  true,
		taintTolerationEnable:           true,
		podAffinityEnable:               true,
		nodeVolumeLimitsEnable:          true,
		volumeZoneEnable:                true,
		podTopologySpreadEnable:         true,
		cacheEnable:                     false,
		volumeBindingEnable:             true,
		dynamicResourceAllocationEnable: false,
	}

	// Checks whether predicate enable args is provided or not.
	// If args were given by scheduler configmap, cover the values in predicateEnable struct.
	arguments.GetBool(&predicate.nodeAffinityEnable, NodeAffinityEnable)
	arguments.GetBool(&predicate.nodePortEnable, NodePortsEnable)
	arguments.GetBool(&predicate.taintTolerationEnable, TaintTolerationEnable)
	arguments.GetBool(&predicate.podAffinityEnable, PodAffinityEnable)
	arguments.GetBool(&predicate.nodeVolumeLimitsEnable, NodeVolumeLimitsEnable)
	arguments.GetBool(&predicate.volumeZoneEnable, VolumeZoneEnable)
	arguments.GetBool(&predicate.podTopologySpreadEnable, PodTopologySpreadEnable)
	arguments.GetBool(&predicate.volumeBindingEnable, VolumeBindingEnable)
	arguments.GetBool(&predicate.dynamicResourceAllocationEnable, DynamicResourceAllocationEnable)
	arguments.GetBool(&predicate.cacheEnable, CachePredicate)

	features := feature.Features{
		EnableStorageCapacityScoring:                 utilFeature.DefaultFeatureGate.Enabled(features.StorageCapacityScoring),
		EnableNodeInclusionPolicyInPodTopologySpread: utilFeature.DefaultFeatureGate.Enabled(features.NodeInclusionPolicyInPodTopologySpread),
		EnableMatchLabelKeysInPodTopologySpread:      utilFeature.DefaultFeatureGate.Enabled(features.MatchLabelKeysInPodTopologySpread),
		EnableSidecarContainers:                      utilFeature.DefaultFeatureGate.Enabled(features.SidecarContainers),
		EnableDRAAdminAccess:                         utilFeature.DefaultFeatureGate.Enabled(features.DRAAdminAccess),
		EnableDynamicResourceAllocation:              utilFeature.DefaultFeatureGate.Enabled(features.DynamicResourceAllocation),
		EnableVolumeAttributesClass:                  utilFeature.DefaultFeatureGate.Enabled(features.VolumeAttributesClass),
		EnableCSIMigrationPortworx:                   utilFeature.DefaultFeatureGate.Enabled(features.CSIMigrationPortworx),
		EnableDRAExtendedResource:                    utilFeature.DefaultFeatureGate.Enabled(features.DRAExtendedResource),
		EnableDRAPrioritizedList:                     utilFeature.DefaultFeatureGate.Enabled(features.DRAPrioritizedList),
		EnableConsumableCapacity:                     utilFeature.DefaultFeatureGate.Enabled(features.DRAConsumableCapacity),
		EnableDRADeviceTaints:                        utilFeature.DefaultFeatureGate.Enabled(features.DRADeviceTaints),
		EnableDRASchedulerFilterTimeout:              utilFeature.DefaultFeatureGate.Enabled(features.DRASchedulerFilterTimeout),
		EnableDRAResourceClaimDeviceStatus:           utilFeature.DefaultFeatureGate.Enabled(features.DRAResourceClaimDeviceStatus),
		EnableDRADeviceBindingConditions:             utilFeature.DefaultFeatureGate.Enabled(features.DRADeviceBindingConditions),
		EnablePartitionableDevices:                   utilFeature.DefaultFeatureGate.Enabled(features.DRAPartitionableDevices),
	}
	return &PredicatesPlugin{pluginArguments: arguments, enabledPredicates: predicate, features: features}
}

func (pp *PredicatesPlugin) Name() string {
	return PluginName
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
	volumeBindingEnable             bool
	dynamicResourceAllocationEnable bool
}

// bind context extension information of predicates
type BindContextExtension struct {
	State *k8sframework.CycleState
}

func (pp *PredicatesPlugin) OnSessionOpen(ssn *framework.Session) {
	pl := ssn.PodLister

	nodeMap := ssn.NodeMap
	handle := k8s.NewFramework(nodeMap,
		k8s.WithSharedDRAManager(ssn.SharedDRAManager()),
		k8s.WithClientSet(ssn.KubeClient()),
		k8s.WithInformerFactory(ssn.InformerFactory()),
	)
	pp.Handle = handle

	pp.InitPlugin()

	// Initialize PredicateCache if enabled
	if pp.enabledPredicates.cacheEnable {
		pp.PredicateCache = predicateCacheNew()
	}

	// Register event handlers to update task info in PodLister & nodeMap
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, allocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, event.Task.NodeName)
			nodeName := event.Task.NodeName
			node, err := pp.Handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
			if err != nil {
				klog.Errorf("predicates, get node %s info failed: %v", nodeName, err)
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
			// ignore this err since apiserver doesn't properly validate affinity terms
			// and we can't fix the validation for backwards compatibility.
			podInfo, _ := k8sframework.NewPodInfo(pod)
			node.AddPodInfo(podInfo)
			klog.V(4).Infof("predicates, update pod %s/%s allocate to node [%s]", pod.Namespace, pod.Name, nodeName)
		},
		DeallocateFunc: func(event *framework.Event) {
			klog.V(4).Infoln("predicates, deallocate", event.Task.NodeName)
			pod := pl.UpdateTask(event.Task, "")
			nodeName := event.Task.NodeName
			node, err := pp.Handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
			if err != nil {
				klog.Errorf("predicates, get node %s info failed: %v", nodeName, err)
				return
			}

			// Get volcano NodeInfo from session for device deallocation
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

			if err = node.RemovePod(klog.FromContext(context.TODO()), pod); err != nil {
				klog.Errorf("predicates, remove pod %s/%s from node [%s] error: %v", pod.Namespace, pod.Name, nodeName, err)
				return
			}
			klog.V(4).Infof("predicates, update pod %s/%s deallocate from node [%s]", pod.Namespace, pod.Name, nodeName)
		},
	})

	ssn.AddPrePredicateFn(pp.Name(), func(task *api.TaskInfo) error {
		state := ssn.GetCycleState(task.UID)
		nodeInfoList, err := pp.Handle.SnapshotSharedLister().NodeInfos().List()
		if err != nil {
			klog.Errorf("Failed to list nodes from snapshot: %v", err)
			return err
		}
		return pp.PrePredicate(task, state, nodeInfoList)
	})

	ssn.AddPredicateFn(pp.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		state := ssn.GetCycleState(task.UID)
		return pp.Predicate(task, node, state)
	})

	// TODO: Need to unify the plugins in nodeorder to predicates.
	// Currently, if the volumebinding plugin crosses predicates plugins and nodeorder plugins, it involves two initializations, which increases memory overhead.
	// Therefore, a BatchNodeOrder extension point needs to be added in here, as volumebinding involves PreScore and Score extension points
	ssn.AddBatchNodeOrderFn(pp.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		state := ssn.GetCycleState(task.UID)
		nodeInfoList, err := pp.Handle.SnapshotSharedLister().NodeInfos().List()
		if err != nil {
			klog.Errorf("Failed to list nodes from snapshot: %v", err)
			return nil, err
		}
		return pp.BatchNodeOrder(task, nodeInfoList, state)
	})

	ssn.RegisterBinder(pp.Name(), pp)

	// Add SimulateAddTask function
	ssn.AddSimulateAddTaskFn(pp.Name(), func(ctx context.Context, cycleState fwk.CycleState, taskToSchedule *api.TaskInfo, taskToAdd *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		podInfoToAdd, err := k8sframework.NewPodInfo(taskToAdd.Pod)
		if err != nil {
			return fmt.Errorf("failed to create pod info: %w", err)
		}

		k8sNodeInfo := k8sframework.NewNodeInfo(nodeInfo.Pods()...)
		k8sNodeInfo.SetNode(nodeInfo.Node)

		if pp.enabledPredicates.podAffinityEnable {
			podAffinityFilter := pp.FilterPlugins[interpodaffinity.Name].(*interpodaffinity.InterPodAffinity)
			isSkipInterPodAffinity := handleSkipPredicatePlugin(cycleState, podAffinityFilter.Name())
			if !isSkipInterPodAffinity {
				status := podAffinityFilter.AddPod(ctx, cycleState, taskToSchedule.Pod, podInfoToAdd, k8sNodeInfo)
				if !status.IsSuccess() {
					return fmt.Errorf("failed to add pod to node %s: %w", nodeInfo.Name, status.AsError())
				}
			}
		}

		return nil
	})

	// Add SimulateRemoveTask function
	ssn.AddSimulateRemoveTaskFn(pp.Name(), func(ctx context.Context, cycleState fwk.CycleState, taskToSchedule *api.TaskInfo, taskToRemove *api.TaskInfo, nodeInfo *api.NodeInfo) error {
		podInfoToRemove, err := k8sframework.NewPodInfo(taskToRemove.Pod)
		if err != nil {
			return fmt.Errorf("failed to create pod info: %w", err)
		}

		k8sNodeInfo := k8sframework.NewNodeInfo(nodeInfo.Pods()...)
		k8sNodeInfo.SetNode(nodeInfo.Node)

		if pp.enabledPredicates.podAffinityEnable {
			podAffinityFilter := pp.FilterPlugins[interpodaffinity.Name].(*interpodaffinity.InterPodAffinity)
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
	ssn.AddSimulatePredicateFn(pp.Name(), func(ctx context.Context, cycleState fwk.CycleState, task *api.TaskInfo, node *api.NodeInfo) error {
		k8sNodeInfo := k8sframework.NewNodeInfo(node.Pods()...)
		k8sNodeInfo.SetNode(node.Node)

		if pp.enabledPredicates.podAffinityEnable {
			isSkipInterPodAffinity := handleSkipPredicatePlugin(cycleState, interpodaffinity.Name)
			if !isSkipInterPodAffinity {
				status := pp.FilterPlugins[interpodaffinity.Name].Filter(ctx, cycleState, task.Pod, k8sNodeInfo)
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

// PrePredicate runs all PreFilter plugins for the given task.
// It is safe here to directly use the state to run plugins because we have already initialized the cycle state
// for each pending pod when open session and will not meet nil state.
func (pp *PredicatesPlugin) PrePredicate(task *api.TaskInfo, state *k8sframework.CycleState, nodeInfoList []fwk.NodeInfo) error {
	// Check restartable container
	if !pp.features.EnableSidecarContainers && task.HasRestartableInitContainer {
		// Scheduler will calculate resources usage for a Pod containing
		// restartable init containers that will be equal or more than kubelet will
		// require to run the Pod. So there will be no overbooking. However, to
		// avoid the inconsistency in resource calculation between the scheduler
		// and the older (before v1.28) kubelet, make the Pod unschedulable.
		return fmt.Errorf("pod has a restartable init container and the SidecarContainers feature is disabled")
	}

	// Run all PreFilter plugins
	for name, plugin := range pp.PrefilterPlugins {
		_, status := plugin.PreFilter(context.TODO(), state, task.Pod, nodeInfoList)
		if err := handleSkipPrePredicatePlugin(status, state, task, name); err != nil {
			return err
		}
	}

	return nil
}

func (pp *PredicatesPlugin) InitPlugin() {
	filterPlugins := map[string]k8sframework.FilterPlugin{}
	stableFilterPlugins := map[string]k8sframework.FilterPlugin{} // Subset for cache-stable filters
	prefilterPlugins := map[string]k8sframework.PreFilterPlugin{}
	reservePlugins := map[string]k8sframework.ReservePlugin{}
	scorePlugins := map[string]nodescore.BaseScorePlugin{}
	preBindPlugins := map[string]k8sframework.PreBindPlugin{}
	scoreWeights := map[string]int{} // Weight for each score plugin

	// Initialize k8s plugins
	// TODO: Add more predicates, k8s.io/kubernetes/pkg/scheduler/framework/plugins/legacy_registry.go
	// 1. NodeUnschedulable (stable filter for cache)
	plugin, _ := nodeunschedulable.New(context.TODO(), nil, pp.Handle, pp.features)
	nodeUnscheduleFilter := plugin.(*nodeunschedulable.NodeUnschedulable)
	filterPlugins[nodeunschedulable.Name] = nodeUnscheduleFilter
	stableFilterPlugins[nodeunschedulable.Name] = nodeUnscheduleFilter

	// 2. NodeAffinity (stable filter for cache)
	if pp.enabledPredicates.nodeAffinityEnable {
		nodeAffinityArgs := config.NodeAffinityArgs{
			AddedAffinity: &v1.NodeAffinity{},
		}
		plugin, _ = nodeaffinity.New(context.TODO(), &nodeAffinityArgs, pp.Handle, pp.features)
		nodeAffinityFilter := plugin.(*nodeaffinity.NodeAffinity)
		filterPlugins[nodeaffinity.Name] = nodeAffinityFilter
		stableFilterPlugins[nodeaffinity.Name] = nodeAffinityFilter
	}
	// 3. NodePorts
	if pp.enabledPredicates.nodePortEnable {
		plugin, _ = nodeports.New(context.TODO(), nil, pp.Handle, pp.features)
		nodePortFilter := plugin.(*nodeports.NodePorts)
		filterPlugins[nodeports.Name] = nodePortFilter
		prefilterPlugins[nodeports.Name] = nodePortFilter
	}
	// 4. TaintToleration (stable filter for cache)
	if pp.enabledPredicates.taintTolerationEnable {
		plugin, _ = tainttoleration.New(context.TODO(), nil, pp.Handle, pp.features)
		tolerationFilter := plugin.(*tainttoleration.TaintToleration)
		filterPlugins[tainttoleration.Name] = tolerationFilter
		stableFilterPlugins[tainttoleration.Name] = tolerationFilter
	}
	// 5. InterPodAffinity
	if pp.enabledPredicates.podAffinityEnable {
		plArgs := &config.InterPodAffinityArgs{}
		plugin, _ = interpodaffinity.New(context.TODO(), plArgs, pp.Handle, pp.features)
		podAffinityFilter := plugin.(*interpodaffinity.InterPodAffinity)
		filterPlugins[interpodaffinity.Name] = podAffinityFilter
		prefilterPlugins[interpodaffinity.Name] = podAffinityFilter
	}
	// 6. NodeVolumeLimits
	if pp.enabledPredicates.nodeVolumeLimitsEnable {
		plugin, _ = nodevolumelimits.NewCSI(context.TODO(), nil, pp.Handle, pp.features)
		nodeVolumeLimitsCSIFilter := plugin.(*nodevolumelimits.CSILimits)
		filterPlugins[nodevolumelimits.CSIName] = nodeVolumeLimitsCSIFilter
	}
	// 7. VolumeZone
	if pp.enabledPredicates.volumeZoneEnable {
		plugin, _ = volumezone.New(context.TODO(), nil, pp.Handle, pp.features)
		volumeZoneFilter := plugin.(*volumezone.VolumeZone)
		filterPlugins[volumezone.Name] = volumeZoneFilter
	}
	// 8. PodTopologySpread
	if pp.enabledPredicates.podTopologySpreadEnable {
		// Setting cluster level default constraints is not support for now.
		ptsArgs := &config.PodTopologySpreadArgs{DefaultingType: config.SystemDefaulting}
		plugin, _ = podtopologyspread.New(context.TODO(), ptsArgs, pp.Handle, pp.features)
		podTopologySpreadFilter := plugin.(*podtopologyspread.PodTopologySpread)
		filterPlugins[podtopologyspread.Name] = podTopologySpreadFilter
		prefilterPlugins[podtopologyspread.Name] = podTopologySpreadFilter
	}
	// 9. VolumeBinding
	if pp.enabledPredicates.volumeBindingEnable {
		vbArgs := defaultVolumeBindingArgs()
		// Currently, we support initializing the VolumeBinding plugin once, but do not support hot loading the plugin after modifying the VolumeBinding parameters.
		// This is because VolumeBinding involves AssumeCache, which contains eventHandler. If VolumeBinding is initialized multiple times,
		// eventHandler will be added continuously, causing memory leaks. For details, see: https://github.com/volcano-sh/volcano/issues/2554.
		// Therefore, if users need to modify the VolumeBinding parameters, users need to restart the scheduler.
		volumeBindingPluginOnce.Do(func() {
			setUpVolumeBindingArgs(vbArgs, pp.pluginArguments)

			var err error
			plugin, err = vbcap.New(context.TODO(), vbArgs.VolumeBindingArgs, pp.Handle, pp.features)
			if err != nil {
				klog.Fatalf("failed to create volume binding plugin with args %+v: %v", vbArgs, err)
			}
			volumeBindingPluginInstance = plugin.(*vbcap.VolumeBinding)
		})

		filterPlugins[vbcap.Name] = volumeBindingPluginInstance
		prefilterPlugins[vbcap.Name] = volumeBindingPluginInstance
		reservePlugins[vbcap.Name] = volumeBindingPluginInstance
		preBindPlugins[vbcap.Name] = volumeBindingPluginInstance
		scorePlugins[vbcap.Name] = volumeBindingPluginInstance
		scoreWeights[vbcap.Name] = vbArgs.Weight // Set weight from plugin args
	}
	// 10. DRA
	if pp.enabledPredicates.dynamicResourceAllocationEnable {
		var err error
		draArgs := defaultDynamicResourcesArgs()
		setUpDynamicResourcesArgs(draArgs, pp.pluginArguments)
		plugin, err = dynamicresources.New(context.TODO(), draArgs.DynamicResourcesArgs, pp.Handle, pp.features)
		if err != nil {
			klog.Fatalf("failed to create dra plugin with err: %v", err)
		}
		dynamicResourceAllocationPlugin := plugin.(*dynamicresources.DynamicResources)
		filterPlugins[dynamicresources.Name] = dynamicResourceAllocationPlugin
		prefilterPlugins[dynamicresources.Name] = dynamicResourceAllocationPlugin
		reservePlugins[dynamicresources.Name] = dynamicResourceAllocationPlugin
		preBindPlugins[dynamicresources.Name] = dynamicResourceAllocationPlugin
	}

	pp.FilterPlugins = filterPlugins
	pp.StableFilterPlugins = stableFilterPlugins
	pp.PrefilterPlugins = prefilterPlugins
	pp.ReservePlugins = reservePlugins
	pp.PreBindPlugins = preBindPlugins
	pp.ScorePlugins = scorePlugins
	pp.ScoreWeights = scoreWeights
}

// Predicate runs all Filter plugins for the given task and node.
func (pp *PredicatesPlugin) Predicate(task *api.TaskInfo, node *api.NodeInfo, state *k8sframework.CycleState) error {
	predicateStatus := make([]*api.Status, 0)
	nodeInfo, err := pp.Handle.SnapshotSharedLister().NodeInfos().Get(node.Name)
	if err != nil {
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

	if node.Allocatable.MaxTaskNum <= len(nodeInfo.GetPods()) {
		klog.V(4).Infof("NodePodNumber predicates Task <%s/%s> on Node <%s> failed, allocatable <%d>, existed <%d>",
			task.Namespace, task.Name, node.Name, node.Allocatable.MaxTaskNum, len(nodeInfo.GetPods()))
		podsNumStatus := &api.Status{
			Code:   api.Unschedulable,
			Reason: api.NodePodNumberExceeded,
			Plugin: pp.Name(),
		}
		predicateStatus = append(predicateStatus, podsNumStatus)
	}

	predicateByStablefilter := func(nodeInfo fwk.NodeInfo) ([]*api.Status, bool, error) {
		// Run all stable filter plugins (for cache)
		predicateStatus := make([]*api.Status, 0)

		for name, plugin := range pp.StableFilterPlugins {
			status := plugin.Filter(context.TODO(), state, task.Pod, nodeInfo)
			filterStatus := api.ConvertPredicateStatus(status)
			if filterStatus.Code != api.Success {
				predicateStatus = append(predicateStatus, filterStatus)
				if util.ShouldAbort(filterStatus) {
					return predicateStatus, false, fmt.Errorf("plugin %s predicates failed %s", name, status.Message())
				}
			}
		}

		return predicateStatus, true, nil
	}

	// Check PredicateWithCache
	var fit bool
	predicateCacheStatus := make([]*api.Status, 0)
	if pp.enabledPredicates.cacheEnable {
		fit, err = pp.PredicateCache.PredicateWithCache(node.Name, task.Pod)
		if err != nil {
			predicateCacheStatus, fit, _ = predicateByStablefilter(nodeInfo)
			pp.PredicateCache.UpdateCache(node.Name, task.Pod, fit)
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

	// Run all Filter plugins (except those in StableFilterPlugins)
	for name, plugin := range pp.FilterPlugins {
		// Skip plugins that are already handled in predicateByStablefilter
		if _, isStable := pp.StableFilterPlugins[name]; isStable {
			continue
		}

		// Check if this plugin should be skipped
		if handleSkipPredicatePlugin(state, name) {
			continue
		}

		status := plugin.Filter(context.TODO(), state, task.Pod, nodeInfo)
		filterStatus := api.ConvertPredicateStatus(status)
		if filterStatus.Code != api.Success {
			predicateStatus = append(predicateStatus, filterStatus)
			if util.ShouldAbort(filterStatus) {
				return api.NewFitErrWithStatus(task, node, predicateStatus...)
			}
		}
	}

	if len(predicateStatus) > 0 {
		return api.NewFitErrWithStatus(task, node, predicateStatus...)
	}

	return nil
}

// BatchNodeOrder runs all Score plugins for the given task and nodes.
func (pp *PredicatesPlugin) BatchNodeOrder(task *api.TaskInfo, nodes []fwk.NodeInfo, state *k8sframework.CycleState) (map[string]float64, error) {
	nodeScores := make(map[string]float64, len(nodes))

	// Run all Score plugins
	for name, plugin := range pp.ScorePlugins {
		// Get normalizer (most plugins don't need normalization, use EmptyNormalizer by default)
		normalizer := &nodescore.EmptyNormalizer{}

		// Get weight from ScoreWeights map, default to 1 if not set
		weight := 1
		if w, exists := pp.ScoreWeights[name]; exists {
			weight = w
		}

		// Calculate score using the helper function
		pluginScores, err := nodescore.CalculatePluginScore(name, plugin, normalizer, state, task.Pod, nodes, weight)
		if err != nil {
			return nil, err
		}

		// Accumulate scores
		for _, node := range nodes {
			nodeName := node.Node().Name
			nodeScores[nodeName] += pluginScores[nodeName]
		}
	}

	klog.V(4).Infof("Batch Total Score for task %s/%s is: %v", task.Namespace, task.Name, nodeScores)
	return nodeScores, nil
}

func (pp *PredicatesPlugin) runReservePlugins(ssn *framework.Session, event *framework.Event) {
	state := ssn.GetCycleState(event.Task.UID)

	for name, plugin := range pp.ReservePlugins {
		status := plugin.Reserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			klog.Errorf("Reserve plugin %s failed for pod %s/%s: %v", name, event.Task.Namespace, event.Task.Name, status.AsError())
			event.Err = status.AsError()
			return
		}
	}
}

func (pp *PredicatesPlugin) runUnReservePlugins(ssn *framework.Session, event *framework.Event) {
	state := ssn.GetCycleState(event.Task.UID)

	for _, plugin := range pp.ReservePlugins {
		plugin.Unreserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
	}
}

// needsPreBind judges whether the pod needs set up extension information in bind context
// to execute extra extension points like PreBind.
// Currently, if a pod has any of the following resources, we assume that the pod needs to pass extension information:
// 1. With volumes (for VolumeBinding plugin)
// 2. With resourceClaims (for DRA plugin)
// If a pod doesn't contain any of the above resources, the pod doesn't need to set up extension information in bind context.
func (pp *PredicatesPlugin) needsPreBind(task *api.TaskInfo) bool {
	// Check if pod has volumes that need binding (only when VolumeBinding is enabled)
	if pp.enabledPredicates.volumeBindingEnable {
		for _, vol := range task.Pod.Spec.Volumes {
			if vol.PersistentVolumeClaim != nil || vol.Ephemeral != nil {
				return true
			}
		}
	}

	// Check if pod has resource claims (only when DRA is enabled)
	if pp.enabledPredicates.dynamicResourceAllocationEnable {
		if len(task.Pod.Spec.ResourceClaims) > 0 {
			return true
		}
	}

	return false
}

func (pp *PredicatesPlugin) PreBind(ctx context.Context, bindCtx *cache.BindContext) error {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return nil
	}

	state := bindCtx.Extensions[pp.Name()].(*BindContextExtension).State

	// Run all PreBind plugins
	for name, plugin := range pp.PreBindPlugins {
		status := plugin.PreBind(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			klog.Errorf("PreBind plugin %s failed for pod %s/%s: %v", name, bindCtx.TaskInfo.Namespace, bindCtx.TaskInfo.Name, status.AsError())
			return status.AsError()
		}
	}

	return nil
}

func (pp *PredicatesPlugin) PreBindRollBack(ctx context.Context, bindCtx *cache.BindContext) {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	state := bindCtx.Extensions[pp.Name()].(*BindContextExtension).State

	for _, plugin := range pp.ReservePlugins {
		plugin.Unreserve(ctx, state, bindCtx.TaskInfo.Pod, bindCtx.TaskInfo.Pod.Spec.NodeName)
	}
}

func (pp *PredicatesPlugin) SetupBindContextExtension(state *k8sframework.CycleState, bindCtx *cache.BindContext) {
	if !pp.needsPreBind(bindCtx.TaskInfo) {
		return
	}

	bindCtx.Extensions[pp.Name()] = &BindContextExtension{State: state}
}

func handleSkipPredicatePlugin(state fwk.CycleState, pluginName string) bool {
	return state.GetSkipFilterPlugins().Has(pluginName)
}

func handleSkipPrePredicatePlugin(status *fwk.Status, state *k8sframework.CycleState, task *api.TaskInfo, pluginName string) error {
	if state.GetSkipFilterPlugins() == nil {
		state.SetSkipFilterPlugins(sets.New[string]())
	}

	if status.IsSkip() {
		state.GetSkipFilterPlugins().Insert(pluginName)
		klog.V(5).Infof("The predicate of plugin %s will skip execution for pod <%s/%s>, because the status returned by pre-predicate is skip",
			pluginName, task.Namespace, task.Name)
	} else if !status.IsSuccess() {
		return fmt.Errorf("plugin %s pre-predicates failed %s", pluginName, status.Message())
	}

	return nil
}

func (pp *PredicatesPlugin) OnSessionClose(ssn *framework.Session) {}

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
