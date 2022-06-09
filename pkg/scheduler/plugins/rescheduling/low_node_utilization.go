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

package rescheduling

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog"
	"reflect"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// DefaultLowNodeConf defines the default configuration for LNU strategy
var DefaultLowNodeConf = map[string]interface{}{
	"thresholds":                 map[string]float64{"cpu": 100, "memory": 100, "pods": 100},
	"targetThresholds":           map[string]float64{"cpu": 100, "memory": 100, "pods": 100},
	"thresholdPriorityClassName": "system-cluster-critical",
	"nodeFit":                    true,
}

type LowNodeUtilizationConf struct {
	Thresholds                 map[string]float64
	TargetThresholds           map[string]float64
	NumberOfNodes              int
	ThresholdPriority          int
	ThresholdPriorityClassName string
	NodeFit                    bool
}

// NewLowNodeUtilizationConf returns the pointer of LowNodeUtilizationConf object with default value
func NewLowNodeUtilizationConf() *LowNodeUtilizationConf {
	return &LowNodeUtilizationConf{
		Thresholds:                 map[string]float64{"cpu": 100, "memory": 100, "pods": 100},
		TargetThresholds:           map[string]float64{"cpu": 100, "memory": 100, "pods": 100},
		ThresholdPriorityClassName: "system-cluster-critical",
		NodeFit:                    true,
	}
}

// parse converts the config map to struct object
func (lnuc *LowNodeUtilizationConf) parse(configs map[string]interface{}) {
	if len(configs) == 0 {
		return
	}
	lowThresholdsConfigs, ok := configs["thresholds"]
	if ok {
		lowConfigs, ok := lowThresholdsConfigs.(map[interface{}]interface{})
		if !ok {
			klog.Warningln("Assert lowThresholdsConfigs to map error, abort the configuration parse.")
			return
		}
		config := make(map[string]int)
		for k, v := range lowConfigs {
			config[k.(string)] = v.(int)
		}
		parseThreshold(config, lnuc, "Thresholds")
	}
	targetThresholdsConfigs, ok := configs["targetThresholds"]
	if ok {
		targetConfigs, ok := targetThresholdsConfigs.(map[interface{}]interface{})
		if !ok {
			klog.Warningln("Assert targetThresholdsConfigs to map error, abort the configuration parse.")
			return
		}
		config := make(map[string]int)
		for k, v := range targetConfigs {
			config[k.(string)] = v.(int)
		}
		parseThreshold(config, lnuc, "TargetThresholds")
	}
}

func parseThreshold(thresholdsConfig map[string]int, lnuc *LowNodeUtilizationConf, param string) {
	if len(thresholdsConfig) > 0 {
		configValue := reflect.ValueOf(lnuc).Elem().FieldByName(param)
		config := configValue.Interface().(map[string]float64)

		cpuThreshold, ok := thresholdsConfig["cpu"]
		if ok {
			config["cpu"] = float64(cpuThreshold)
		}
		memoryThreshold, ok := thresholdsConfig["memory"]
		if ok {
			config["memory"] = float64(memoryThreshold)
		}
		podThreshold, ok := thresholdsConfig["pod"]
		if ok {
			config["pod"] = float64(podThreshold)
		}
	}
}

var victimsFnForLnu = func(tasks []*api.TaskInfo) []*api.TaskInfo {
	victims := make([]*api.TaskInfo, 0)

	// parse configuration arguments
	utilizationConfig := NewLowNodeUtilizationConf()
	parametersConfig := RegisteredStrategyConfigs["lowNodeUtilization"]
	var config map[string]interface{}
	config, ok := parametersConfig.(map[string]interface{})
	if !ok {
		klog.Errorln("parameters parse error for lowNodeUtilization")
		return victims
	}
	utilizationConfig.parse(config)

	// group the nodes into lowNodes and highNodes
	nodeUtilizationList := getNodeUtilization()
	lowNodes, highNodes := groupNodesByUtilization(nodeUtilizationList, lowThresholdFilter, highThresholdFilter, *utilizationConfig)

	if len(lowNodes) == 0 {
		klog.V(4).Infof("The resource utilization of all nodes is above the threshold")
		return victims
	}
	if len(lowNodes) == len(Session.Nodes) {
		klog.V(4).Infof("The resource utilization of all nodes is below the threshold")
		return victims
	}
	if len(highNodes) == 0 {
		klog.V(4).Infof("The resource utilization of all nodes is below the target threshold")
		return victims
	}

	// select victims from lowNodes
	return evictPodsFromSourceNodes(highNodes, lowNodes, tasks, isContinueEvictPods, *utilizationConfig)
}

// lowThresholdFilter filter nodes which all resource dimensions are under the low utilization threshold
func lowThresholdFilter(usage *NodeUtilization, config interface{}) bool {
	utilizationConfig := parseArgToConfig(config)
	if utilizationConfig == nil {
		klog.V(4).Infoln("lack of LowNodeUtilizationConf pointer parameter")
		return false
	}

	if usage.nodeInfo.Spec.Unschedulable {
		return false
	}
	for rName, usagePercent := range usage.utilization {
		if threshold, ok := utilizationConfig.Thresholds[string(rName)]; ok {
			if usagePercent >= threshold {
				return false
			}
		}
	}
	return true
}

// highThresholdFilter filter nodes which at least one resource dimension above the target utilization threshold
func highThresholdFilter(usage *NodeUtilization, config interface{}) bool {
	utilizationConfig := parseArgToConfig(config)
	if utilizationConfig == nil {
		klog.V(4).Infof("lack of LowNodeUtilizationConf pointer parameter")
		return false
	}

	for rName, usagePercent := range usage.utilization {
		if threshold, ok := utilizationConfig.TargetThresholds[string(rName)]; ok {
			if usagePercent > threshold {
				return true
			}
		}
	}
	return false
}

// isContinueEvictPods judges whether continue to select victim pods
func isContinueEvictPods(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
	var isNodeOverused bool
	utilizationConfig := parseArgToConfig(config)
	for rName, usage := range usage.utilization {
		if threshold, ok := utilizationConfig.TargetThresholds[string(rName)]; ok {
			if usage >= threshold {
				isNodeOverused = true
				break
			}
		}
	}
	if !isNodeOverused {
		return false
	}

	for _, amount := range totalAllocatableResource {
		if amount.CmpInt64(0) == 0 {
			return false
		}
	}
	return true
}
