/*
Copyright 2022 The Volcano Authors.

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

package usage

import (
	"time"

	"volcano.sh/volcano/pkg/scheduler/metrics/source"

	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "usage"
	thresholdSection      = "thresholds"
	MetricsActiveTime     = 5 * time.Minute
	NodeUsageCPUExtend    = "the CPU load of the node exceeds the upper limit."
	NodeUsageMemoryExtend = "the memory load of the node exceeds the upper limit."
)

/*
   actions: "enqueue, allocate, backfill"
   tiers:
   - plugins:
     - name: usage
       enablePredicate: false  # If the value is false, new pod scheduling is not disabled when the node load reaches the threshold. If the value is true or left blank, new pod scheduling is disabled.
       arguments:
         usage.weight: 5
         cpu.weight: 1
         memory.weight: 1
         thresholds:
           cpu: 80
           mem: 80
         # --- Dynamic Sigma parameters ---
         dynamic.sigma_base: 0.15
         dynamic.threshold: 0.5
         dynamic.sensitivity: 12.0
         # --- BestEffort Pod handling ---
         be_default_ratio: 0.1
         be_penalty_factor: 1.2
*/

const AVG string = "average"

type usagePlugin struct {
	pluginArguments framework.Arguments
	usageWeight     int
	cpuWeight       int
	memoryWeight    int
	usageType       string
	cpuThresholds   float64
	memThresholds   float64
	period          string

	// Dynamic sigma estimator configuration
	estimatorConfig *EstimatorConfig
	// Session-level shadow load cache
	shadowCache *ShadowLoadCache
	// Metrics collection interval (used as the monitoring delay window)
	metricsInterval time.Duration
}

// New function returns usagePlugin object
func New(args framework.Arguments) framework.Plugin {
	var plugin = &usagePlugin{
		pluginArguments: args,
		usageWeight:     5,
		cpuWeight:       1,
		memoryWeight:    1,
		usageType:       AVG,
		cpuThresholds:   80,
		memThresholds:   80,
		period:          source.NODE_METRICS_PERIOD,
		estimatorConfig: DefaultEstimatorConfig(),
		metricsInterval: 30 * time.Second,
	}
	args.GetInt(&plugin.usageWeight, "usage.weight")
	args.GetInt(&plugin.cpuWeight, "cpu.weight")
	args.GetInt(&plugin.memoryWeight, "memory.weight")

	// Parse threshold configuration
	argsValue, ok := plugin.pluginArguments[thresholdSection]
	if ok {
		thresholdArgs, ok := argsValue.(map[interface{}]interface{})
		if ok {
			for resourceName, threshold := range thresholdArgs {
				resource, _ := resourceName.(string)
				value, _ := threshold.(int)
				switch resource {
				case "cpu":
					plugin.cpuThresholds = float64(value)
				case "mem":
					plugin.memThresholds = float64(value)
				}
			}
		} else {
			klog.Errorf("Failed to convert the thresholds information, thresholds args values is %v", argsValue)
		}
	}

	// Parse dynamic sigma parameters
	parseFloatArg(plugin.pluginArguments, "dynamic.sigma_base", &plugin.estimatorConfig.SigmaBase)
	parseFloatArg(plugin.pluginArguments, "dynamic.threshold", &plugin.estimatorConfig.Threshold)
	parseFloatArg(plugin.pluginArguments, "dynamic.sensitivity", &plugin.estimatorConfig.Sensitivity)
	parseFloatArg(plugin.pluginArguments, "be_default_ratio", &plugin.estimatorConfig.BERatio)
	parseFloatArg(plugin.pluginArguments, "be_penalty_factor", &plugin.estimatorConfig.BEPenalty)

	return plugin
}

// parseFloatArg parses a float64 argument from the plugin arguments map.
func parseFloatArg(args framework.Arguments, key string, target *float64) {
	if val, ok := args[key]; ok {
		switch v := val.(type) {
		case float64:
			*target = v
		case int:
			*target = float64(v)
		case int64:
			*target = float64(v)
		}
	}
}

func (up *usagePlugin) Name() string {
	return PluginName
}

func (up *usagePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter usage plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving usage plugin ...")
	}()

	// Step 1: Initialize ShadowLoadCache
	if up.shadowCache != nil && !up.shadowCache.IsClean() {
		up.shadowCache.Reset()
	}
	up.shadowCache = NewShadowLoadCache()

	// Step 2: Warm up shadow cache by scanning existing tasks on nodes
	up.warmUpShadowCache(ssn)

	if klog.V(4).Enabled() {
		for node, nodeInfo := range ssn.Nodes {
			cpuEst, memEst := up.shadowCache.GetNodeEst(node)
			klog.V(4).Infof("node:%v, cpu usage:%v, mem usage:%v, metrics time is %v, shadowCPUEst: %v, shadowMemEst: %v",
				node, nodeInfo.ResourceUsage.CPUUsageAvg, nodeInfo.ResourceUsage.MEMUsageAvg,
				nodeInfo.ResourceUsage.MetricsTime, cpuEst, memEst)
		}
	}

	// Step 3: Register EventHandler for Allocate/Deallocate tracking
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			up.handleAllocate(ssn, event)
		},
		DeallocateFunc: func(event *framework.Event) {
			up.handleDeallocate(ssn, event)
		},
	})

	// Step 4: Register PredicateFn - only checks real load against thresholds
	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		predicateStatus := make([]*api.Status, 0)
		usageStatus := &api.Status{Plugin: PluginName}

		now := time.Now()
		if up.period == "" || now.Sub(node.ResourceUsage.MetricsTime) > MetricsActiveTime {
			klog.V(4).Infof("The period(%s) is empty or the usage metrics data is not updated for more than %v minutes, "+
				"Usage plugin filter for task %s/%s on node %s pass, metrics time is %v. ", up.period, MetricsActiveTime, task.Namespace, task.Name, node.Name, node.ResourceUsage.MetricsTime)
			return nil
		}

		klog.V(4).Infof("predicateFn cpuThreshold:%v, memThreshold:%v", up.cpuThresholds, up.memThresholds)
		if node.ResourceUsage.CPUUsageAvg[up.period] > up.cpuThresholds {
			klog.V(3).Infof("Node %s cpu usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.CPUUsageAvg[up.period], up.cpuThresholds)
			usageStatus.Code = api.UnschedulableAndUnresolvable
			usageStatus.Reason = NodeUsageCPUExtend
			predicateStatus = append(predicateStatus, usageStatus)
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}
		if node.ResourceUsage.MEMUsageAvg[up.period] > up.memThresholds {
			klog.V(3).Infof("Node %s mem usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.MEMUsageAvg[up.period], up.memThresholds)
			usageStatus.Code = api.UnschedulableAndUnresolvable
			usageStatus.Reason = NodeUsageMemoryExtend
			predicateStatus = append(predicateStatus, usageStatus)
			return api.NewFitErrWithStatus(task, node, predicateStatus...)
		}

		klog.V(4).Infof("Usage plugin filter for task %s/%s on node %s pass.", task.Namespace, task.Name, node.Name)
		return nil
	}

	// Step 5: Register NodeOrderFn - scores nodes based on composite utilization
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		if !up.isMetricsAvailable(node) {
			return 0, nil
		}
		return up.calcNodeScore(node), nil
	}

	// Step 6: Register BatchNodeOrderFn - batch scores all candidate nodes
	batchNodeOrderFn := func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		scores := make(map[string]float64, len(nodes))
		for _, node := range nodes {
			if !up.isMetricsAvailable(node) {
				scores[node.Name] = 0
				continue
			}
			scores[node.Name] = up.calcNodeScore(node)
		}
		return scores, nil
	}

	ssn.AddPredicateFn(up.Name(), predicateFn)
	ssn.AddNodeOrderFn(up.Name(), nodeOrderFn)
	ssn.AddBatchNodeOrderFn(up.Name(), batchNodeOrderFn)
}

func (up *usagePlugin) OnSessionClose(ssn *framework.Session) {
	if up.shadowCache != nil {
		up.shadowCache.Reset()
	}
}

// warmUpShadowCache scans all nodes' tasks and populates the shadow cache
// with estimated resource consumption for pods that are in the monitoring blind spot.
func (up *usagePlugin) warmUpShadowCache(ssn *framework.Session) {
	for _, nodeInfo := range ssn.Nodes {
		// Compute node real utilization once per node (for Sigmoid)
		realCPU := getRealCPUPercent(nodeInfo, up.period)
		realMem := getRealMemPercent(nodeInfo, up.period)
		nodeRealUtil := CalcNodeRealUtilization(realCPU, realMem, up.cpuWeight, up.memoryWeight)
		sigmaDynamic := CalcDynamicSigma(up.estimatorConfig, nodeRealUtil)

		for _, task := range nodeInfo.Tasks {
			if !shouldAddToShadowCache(task, up.metricsInterval) {
				continue
			}

			bestEffortCount := up.shadowCache.GetBestEffortCount(nodeInfo.Name)
			cpuReq, cpuLim := getPodCPURequestLimit(task.Pod)
			memReq, memLim := getPodMemRequestLimit(task.Pod)

			estCPU := EstimatePodResource(up.estimatorConfig, cpuReq, cpuLim,
				sigmaDynamic, nodeInfo.Capacity.MilliCPU, bestEffortCount, task.BestEffort)
			estMem := EstimatePodResource(up.estimatorConfig, memReq, memLim,
				sigmaDynamic, nodeInfo.Capacity.Memory, bestEffortCount, task.BestEffort)

			up.shadowCache.AddEstimate(nodeInfo.Name, estCPU, estMem, task.BestEffort)

			klog.V(5).Infof("Shadow cache warm-up: task %s/%s on node %s, estCPU=%.2f, estMem=%.2f, bestEffort=%v",
				task.Namespace, task.Name, nodeInfo.Name, estCPU, estMem, task.BestEffort)
		}
	}
}

// handleAllocate is called when a pod is allocated to a node during the current session.
func (up *usagePlugin) handleAllocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	nodeName := task.NodeName
	node, ok := ssn.Nodes[nodeName]
	if !ok {
		return
	}

	// Compute node real utilization → sigmaDynamic
	realCPU := getRealCPUPercent(node, up.period)
	realMem := getRealMemPercent(node, up.period)
	nodeRealUtil := CalcNodeRealUtilization(realCPU, realMem, up.cpuWeight, up.memoryWeight)
	sigmaDynamic := CalcDynamicSigma(up.estimatorConfig, nodeRealUtil)

	// Estimate pod resource consumption
	bestEffortCount := up.shadowCache.GetBestEffortCount(nodeName)
	cpuReq, cpuLim := getPodCPURequestLimit(task.Pod)
	memReq, memLim := getPodMemRequestLimit(task.Pod)
	estCPU := EstimatePodResource(up.estimatorConfig, cpuReq, cpuLim,
		sigmaDynamic, node.Capacity.MilliCPU, bestEffortCount, task.BestEffort)
	estMem := EstimatePodResource(up.estimatorConfig, memReq, memLim,
		sigmaDynamic, node.Capacity.Memory, bestEffortCount, task.BestEffort)

	// Add to shadow cache
	up.shadowCache.AddEstimate(nodeName, estCPU, estMem, task.BestEffort)

	klog.V(4).Infof("Usage plugin Allocate: task %s/%s to node %s, estCPU=%.2f, estMem=%.2f",
		task.Namespace, task.Name, nodeName, estCPU, estMem)
}

// handleDeallocate is called when a pod allocation is rolled back.
func (up *usagePlugin) handleDeallocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	nodeName := task.NodeName
	node, ok := ssn.Nodes[nodeName]
	if !ok {
		return
	}

	// Recompute the same estimate that was added during Allocate
	realCPU := getRealCPUPercent(node, up.period)
	realMem := getRealMemPercent(node, up.period)
	nodeRealUtil := CalcNodeRealUtilization(realCPU, realMem, up.cpuWeight, up.memoryWeight)
	sigmaDynamic := CalcDynamicSigma(up.estimatorConfig, nodeRealUtil)

	bestEffortCount := up.shadowCache.GetBestEffortCount(nodeName)
	cpuReq, cpuLim := getPodCPURequestLimit(task.Pod)
	memReq, memLim := getPodMemRequestLimit(task.Pod)
	estCPU := EstimatePodResource(up.estimatorConfig, cpuReq, cpuLim,
		sigmaDynamic, node.Capacity.MilliCPU, bestEffortCount, task.BestEffort)
	estMem := EstimatePodResource(up.estimatorConfig, memReq, memLim,
		sigmaDynamic, node.Capacity.Memory, bestEffortCount, task.BestEffort)

	// Subtract from shadow cache
	up.shadowCache.SubEstimate(nodeName, estCPU, estMem, task.BestEffort)

	klog.V(4).Infof("Usage plugin Deallocate: task %s/%s from node %s, estCPU=%.2f, estMem=%.2f",
		task.Namespace, task.Name, nodeName, estCPU, estMem)
}

// calcNodeScore computes the score for a node based on composite utilization.
func (up *usagePlugin) calcNodeScore(node *api.NodeInfo) float64 {
	cpuEst, memEst := up.shadowCache.GetNodeEst(node.Name)
	realCPU := getRealCPUPercent(node, up.period)
	realMem := getRealMemPercent(node, up.period)
	cpuComp := CalcCompositeUtilization(realCPU, cpuEst, node.Capacity.MilliCPU)
	memComp := CalcCompositeUtilization(realMem, memEst, node.Capacity.Memory)
	score := CalcNodeScore(cpuComp, memComp, up.cpuWeight, up.memoryWeight)
	klog.V(4).Infof("Node %s score: cpuComp=%.4f, memComp=%.4f, score=%.2f (max=%d)",
		node.Name, cpuComp, memComp, score, fwk.MaxNodeScore)
	return score
}

// isMetricsAvailable checks if the node's metrics data is available and fresh.
// This reuses the same degradation logic from the original usage plugin.
func (up *usagePlugin) isMetricsAvailable(node *api.NodeInfo) bool {
	now := time.Now()
	if up.period == "" || now.Sub(node.ResourceUsage.MetricsTime) > MetricsActiveTime {
		return false
	}
	return true
}

// shouldAddToShadowCache determines whether a task should be tracked in the shadow cache.
// A task is added if it has been assigned to a node but its metrics are not yet visible
// in the monitoring system.
func shouldAddToShadowCache(task *api.TaskInfo, metricsDelay time.Duration) bool {
	// Only consider pods that have been assigned to a node
	if task.NodeName == "" {
		return false
	}
	switch task.Status {
	case api.Pending:
		// Already assigned to a node but still Pending - scheduling decision from previous session
		return true
	case api.Running:
		// Running but started recently - metrics not yet collected
		if task.Pod == nil || task.Pod.Status.StartTime == nil {
			return true
		}
		return time.Since(task.Pod.Status.StartTime.Time) < metricsDelay
	default:
		// Succeeded, Failed, Unknown, Binding, Bound, etc. - do not add
		return false
	}
}

// getRealCPUPercent returns the real CPU usage percentage for a node.
// Returns 0 if the period is not configured or data is unavailable.
func getRealCPUPercent(node *api.NodeInfo, period string) float64 {
	if period == "" {
		return 0
	}
	return node.ResourceUsage.CPUUsageAvg[period]
}

// getRealMemPercent returns the real Memory usage percentage for a node.
// Returns 0 if the period is not configured or data is unavailable.
func getRealMemPercent(node *api.NodeInfo, period string) float64 {
	if period == "" {
		return 0
	}
	return node.ResourceUsage.MEMUsageAvg[period]
}
