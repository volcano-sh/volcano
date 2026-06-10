/*
Copyright 2026 The Volcano Authors.

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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/metrics/source"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "usage"
	thresholdSection      = "thresholds"
	estimatorSection      = "estimator"
	MetricsActiveTime     = 5 * time.Minute
	NodeUsageCPUExtend    = "the CPU load of the node exceeds the upper limit."
	NodeUsageMemoryExtend = "the memory load of the node exceeds the upper limit."

	// defaultMetricsInterval is the default interval for metrics collection (used as monitoring delay window)
	defaultMetricsInterval = 30 * time.Second
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
         estimator:
           request_ratio: 0.7
           burst_ratio: 0
           risk_threshold: 0.6
           risk_factor: 1.2
           be_cpu: 250m
           be_memory: 200Mi
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

	// Resource estimator parameters
	requestRatio  float64 // Request contribution ratio, default 0.7
	burstRatio    float64 // Burst contribution ratio, default 0.0
	riskThreshold float64 // Composite load threshold for risk factor, default 0.6
	riskFactor    float64 // Risk multiplier once threshold is reached, default 1.2
	beCPU         float64 // BestEffort CPU estimate in milliCPU, default 250m
	beMemory      float64 // BestEffort memory estimate in bytes, default 200Mi

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
		requestRatio:    0.7,
		burstRatio:      0.0,
		riskThreshold:   0.6,
		riskFactor:      1.2,
		beCPU:           250,
		beMemory:        float64(200 * 1024 * 1024),
		metricsInterval: defaultMetricsInterval,
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

	parseEstimatorArgs(plugin.pluginArguments, plugin)

	return plugin
}

func parseEstimatorArgs(args framework.Arguments, plugin *usagePlugin) {
	argsValue, ok := args[estimatorSection]
	if !ok {
		return
	}

	estimatorArgs, ok := toEstimatorArguments(argsValue)
	if !ok {
		klog.Errorf("Failed to convert the estimator information, estimator args values is %v", argsValue)
		return
	}

	parseBoundedFloatArg(estimatorArgs, "request_ratio", &plugin.requestRatio, 0, 1)
	parseBoundedFloatArg(estimatorArgs, "burst_ratio", &plugin.burstRatio, 0, 1)
	parseBoundedFloatArg(estimatorArgs, "risk_threshold", &plugin.riskThreshold, 0, 1)
	parseMinFloatArg(estimatorArgs, "risk_factor", &plugin.riskFactor, 1)
	parseCPUQuantityArg(estimatorArgs, "be_cpu", &plugin.beCPU)
	parseMemoryQuantityArg(estimatorArgs, "be_memory", &plugin.beMemory)
}

func toEstimatorArguments(value interface{}) (framework.Arguments, bool) {
	switch args := value.(type) {
	case framework.Arguments:
		return args, true
	case map[string]interface{}:
		return framework.Arguments(args), true
	case map[interface{}]interface{}:
		estimatorArgs := framework.Arguments{}
		for key, value := range args {
			keyStr, ok := key.(string)
			if !ok {
				return nil, false
			}
			estimatorArgs[keyStr] = value
		}
		return estimatorArgs, true
	default:
		return nil, false
	}
}

// parseBoundedFloatArg parses a float64 argument and keeps the existing value
// when the input is outside [min, max].
func parseBoundedFloatArg(args framework.Arguments, key string, target *float64, min, max float64) {
	value, ok := getFloatArg(args, key)
	if !ok {
		return
	}
	if value < min || value > max {
		klog.Warningf("Could not parse argument: %v for key %s, expected value in [%v, %v]", value, key, min, max)
		return
	}
	*target = value
}

// parseMinFloatArg parses a float64 argument and keeps the existing value when
// the input is lower than min.
func parseMinFloatArg(args framework.Arguments, key string, target *float64, min float64) {
	value, ok := getFloatArg(args, key)
	if !ok {
		return
	}
	if value < min {
		klog.Warningf("Could not parse argument: %v for key %s, expected value >= %v", value, key, min)
		return
	}
	*target = value
}

func getFloatArg(args framework.Arguments, key string) (float64, bool) {
	if val, ok := args[key]; ok {
		switch v := val.(type) {
		case float64:
			return v, true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		}
		klog.Warningf("Could not parse argument: %v for key %s to float64", val, key)
	}
	return 0, false
}

func parseCPUQuantityArg(args framework.Arguments, key string, target *float64) {
	if val, ok := args[key]; ok {
		switch v := val.(type) {
		case int:
			if v < 0 {
				klog.Warningf("Could not parse argument: %v for key %s, expected non-negative CPU millicores", val, key)
				return
			}
			*target = float64(v)
			return
		case int64:
			if v < 0 {
				klog.Warningf("Could not parse argument: %v for key %s, expected non-negative CPU millicores", val, key)
				return
			}
			*target = float64(v)
			return
		case float64:
			if v < 0 {
				klog.Warningf("Could not parse argument: %v for key %s, expected non-negative CPU millicores", val, key)
				return
			}
			*target = v
			return
		}
	}
	quantity, ok := getQuantityArg(args, key)
	if !ok {
		return
	}
	value := float64(quantity.MilliValue())
	if value < 0 {
		klog.Warningf("Could not parse argument: %v for key %s, expected non-negative CPU quantity", args[key], key)
		return
	}
	*target = value
}

func parseMemoryQuantityArg(args framework.Arguments, key string, target *float64) {
	quantity, ok := getQuantityArg(args, key)
	if !ok {
		return
	}
	value := float64(quantity.Value())
	if value < 0 {
		klog.Warningf("Could not parse argument: %v for key %s, expected non-negative memory quantity", args[key], key)
		return
	}
	*target = value
}

func getQuantityArg(args framework.Arguments, key string) (resource.Quantity, bool) {
	val, ok := args[key]
	if !ok {
		return resource.Quantity{}, false
	}
	switch v := val.(type) {
	case string:
		quantity, err := resource.ParseQuantity(normalizeQuantitySuffix(v))
		if err != nil {
			klog.Warningf("Could not parse argument: %v for key %s to resource quantity", val, key)
			return resource.Quantity{}, false
		}
		return quantity, true
	case int:
		return *resource.NewQuantity(int64(v), resource.DecimalSI), true
	case int64:
		return *resource.NewQuantity(v, resource.DecimalSI), true
	case float64:
		return *resource.NewMilliQuantity(int64(v*1000), resource.DecimalSI), true
	default:
		klog.Warningf("Could not parse argument: %v for key %s to resource quantity", val, key)
		return resource.Quantity{}, false
	}
}

func normalizeQuantitySuffix(value string) string {
	replacements := map[string]string{
		"ki": "Ki",
		"mi": "Mi",
		"gi": "Gi",
		"ti": "Ti",
		"pi": "Pi",
		"ei": "Ei",
	}
	lowerValue := strings.ToLower(value)
	for lowerSuffix, canonicalSuffix := range replacements {
		if strings.HasSuffix(lowerValue, lowerSuffix) {
			return value[:len(value)-len(lowerSuffix)] + canonicalSuffix
		}
	}
	return value
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

	// Step 2: Read metricsInterval from scheduler configmap (metrics.interval)
	metricsConf := ssn.GetMetricsConf()
	if metricsConf != nil {
		if intervalStr, ok := metricsConf["interval"]; ok {
			if interval, err := time.ParseDuration(intervalStr); err == nil && interval > 0 {
				up.metricsInterval = interval
				klog.V(4).Infof("Usage plugin: metricsInterval set to %v from configmap", interval)
			}
		}
	}

	// Step 3: Warm up shadow cache by scanning existing tasks on nodes
	up.warmUpShadowCache(ssn)

	if klog.V(4).Enabled() {
		for node, nodeInfo := range ssn.Nodes {
			cpuEst, memEst := up.shadowCache.GetNodeEst(node)
			klog.V(4).Infof("node:%v, cpu usage:%v, mem usage:%v, metrics time is %v, shadowCPUEst: %v, shadowMemEst: %v",
				node, nodeInfo.ResourceUsage.CPUUsageAvg, nodeInfo.ResourceUsage.MEMUsageAvg,
				nodeInfo.ResourceUsage.MetricsTime, cpuEst, memEst)
		}
	}

	// Step 4: Register EventHandler for Allocate/Deallocate tracking
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			up.handleAllocate(ssn, event)
		},
		DeallocateFunc: func(event *framework.Event) {
			up.handleDeallocate(ssn, event)
		},
	})

	// Step 5: Register PredicateFn - only checks real load against thresholds
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

	// Step 6: Register NodeOrderFn - scores nodes based on composite utilization
	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		if !up.isMetricsAvailable(node) {
			return 0, nil
		}
		return up.calcNodeScore(node), nil
	}

	ssn.AddPredicateFn(up.Name(), predicateFn)
	ssn.AddNodeOrderFn(up.Name(), nodeOrderFn)
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
		for _, task := range nodeInfo.Tasks {
			if !shouldAddToShadowCache(task, up.metricsInterval) {
				continue
			}

			appliedRiskFactor := up.calcAppliedRiskFactor(nodeInfo)
			estCPU, estMem := up.estimateTaskResource(task, appliedRiskFactor)

			up.shadowCache.AddEstimate(nodeInfo.Name, task.UID, estCPU, estMem)

			klog.V(5).Infof("Shadow cache warm-up: task %s/%s on node %s, estCPU=%.2f, estMem=%.2f, riskFactor=%.2f, bestEffort=%v",
				task.Namespace, task.Name, nodeInfo.Name, estCPU, estMem, appliedRiskFactor, task.BestEffort)
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

	// Estimate pod resource consumption
	appliedRiskFactor := up.calcAppliedRiskFactor(node)
	estCPU, estMem := up.estimateTaskResource(task, appliedRiskFactor)

	// Add to shadow cache with snapshot
	up.shadowCache.AddEstimate(nodeName, task.UID, estCPU, estMem)

	klog.V(4).Infof("Usage plugin Allocate: task %s/%s to node %s, estCPU=%.2f, estMem=%.2f, riskFactor=%.2f",
		task.Namespace, task.Name, nodeName, estCPU, estMem, appliedRiskFactor)
}

// handleDeallocate is called when a pod allocation is rolled back.
// It uses the snapshot recorded during allocation to ensure precise subtraction.
func (up *usagePlugin) handleDeallocate(ssn *framework.Session, event *framework.Event) {
	task := event.Task
	// Use snapshot-based subtraction for precise consistency
	up.shadowCache.SubEstimateBySnapshot(task.UID)

	klog.V(4).Infof("Usage plugin Deallocate: task %s/%s, snapshot-based subtraction",
		task.Namespace, task.Name)
}

// calcNodeScore computes the score for a node based on composite utilization.
func (up *usagePlugin) calcNodeScore(node *api.NodeInfo) float64 {
	cpuEst, memEst := up.shadowCache.GetNodeEst(node.Name)
	realCPU := getRealCPUPercent(node, up.period)
	realMem := getRealMemPercent(node, up.period)
	cpuComp := CalcCompositeUtilization(realCPU, cpuEst, node.Capacity.MilliCPU)
	memComp := CalcCompositeUtilization(realMem, memEst, node.Capacity.Memory)
	score := CalcNodeScore(cpuComp, memComp, up.cpuWeight, up.memoryWeight, up.usageWeight)
	klog.V(4).Infof("Node %s score: cpuComp=%.4f, memComp=%.4f, score=%.2f (max=%d)",
		node.Name, cpuComp, memComp, score, fwk.MaxNodeScore)
	return score
}

func (up *usagePlugin) calcAppliedRiskFactor(node *api.NodeInfo) float64 {
	cpuEst, memEst := up.shadowCache.GetNodeEst(node.Name)
	realCPU := getRealCPUPercent(node, up.period)
	realMem := getRealMemPercent(node, up.period)
	cpuComp := CalcCompositeUtilization(realCPU, cpuEst, node.Capacity.MilliCPU)
	memComp := CalcCompositeUtilization(realMem, memEst, node.Capacity.Memory)
	loadCompositePercentage := CalcLoadCompositePercentage(cpuComp, memComp, up.cpuWeight, up.memoryWeight)
	return CalcAppliedRiskFactor(loadCompositePercentage, up.riskThreshold, up.riskFactor)
}

func (up *usagePlugin) estimateTaskResource(task *api.TaskInfo, appliedRiskFactor float64) (estCPU, estMem float64) {
	if task.BestEffort {
		return EstimateBestEffortResource(up.beCPU, appliedRiskFactor),
			EstimateBestEffortResource(up.beMemory, appliedRiskFactor)
	}
	cpuReq, cpuLim, memReq, memLim := getPodResourceRequestLimit(task.Pod)
	return EstimatePodResource(cpuReq, cpuLim, up.requestRatio, up.burstRatio, appliedRiskFactor),
		EstimatePodResource(memReq, memLim, up.requestRatio, up.burstRatio, appliedRiskFactor)
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
// in the monitoring system. BestEffort pods follow the same logic as Guaranteed/Burstable.
func shouldAddToShadowCache(task *api.TaskInfo, metricsDelay time.Duration) bool {
	// Only consider pods that have been assigned to a node
	if task.NodeName == "" {
		return false
	}
	switch task.Status {
	case api.Allocated, api.Binding, api.Bound:
		return true
	case api.Running:
		// Running but started recently - metrics not yet collected
		if task.Pod == nil || task.Pod.Status.StartTime == nil {
			return true
		}
		return time.Since(task.Pod.Status.StartTime.Time) < metricsDelay
	default:
		// Pending, Succeeded, Failed, Unknown, etc. - do not add
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
