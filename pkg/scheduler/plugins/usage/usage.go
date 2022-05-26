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
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName        = "usage"
	cpuUsageAvgPrefix = "CPUUsageAvg."
	memUsageAvgPrefix = "MEMUsageAvg."
	thresholdSection  = "thresholds"
	cpuUsageAvg5m     = "5m"
)

/*
   actions: "enqueue, allocate, backfill"
   tiers:
   - plugins:
     - name: usage
       arguments:
          thresholds:
            CPUUsageAvg.5m: 80
            MEMUsageAvg.5m: 90
*/

type thresholdConfig struct {
	cpuUsageAvg map[string]float64
	memUsageAvg map[string]float64
}

type usagePlugin struct {
	pluginArguments framework.Arguments
	weight          int
	threshold       thresholdConfig
}

// New function returns usagePlugin object
func New(args framework.Arguments) framework.Plugin {
	usageWeight := 1
	args.GetInt(&usageWeight, "usage.weight")
	config := thresholdConfig{
		cpuUsageAvg: make(map[string]float64),
		memUsageAvg: make(map[string]float64),
	}
	return &usagePlugin{
		pluginArguments: args,
		weight:          usageWeight,
		threshold:       config,
	}
}

func (up *usagePlugin) Name() string {
	return PluginName
}

func (up *usagePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter usage plugin ...")
	if klog.V(4) {
		defer func() {
			klog.V(4).Infof("Leaving usage plugin ...")
		}()
	}

	if klog.V(4) {
		for node := range ssn.Nodes {
			usage := ssn.Nodes[node].ResourceUsage
			klog.V(4).Infof("node:%v, cpu usage:%v, mem usage:%v", node, usage.CPUUsageAvg["5m"], usage.MEMUsageAvg["5m"])
		}
	}

	argsValue, ok := up.pluginArguments[thresholdSection]
	if ok {
		args, ok := argsValue.(map[interface{}]interface{})
		if !ok {
			klog.V(4).Infof("pluginArguments[thresholdsSection]:%v", argsValue)
		}
		for k, v := range args {
			key, _ := k.(string)
			var val float64
			switch a := v.(type) {
			case string:
				val, _ = strconv.ParseFloat(a, 64)
			case int:
				val = float64(a)
			case float64:
				val = a
			default:
				klog.V(4).Infof("The threshold %v is an unknown type", a)
			}
			if strings.Contains(key, cpuUsageAvgPrefix) {
				periodKey := strings.Replace(key, cpuUsageAvgPrefix, "", 1)
				up.threshold.cpuUsageAvg[periodKey] = val
			}
			if strings.Contains(key, memUsageAvgPrefix) {
				periodKey := strings.Replace(key, memUsageAvgPrefix, "", 1)
				up.threshold.memUsageAvg[periodKey] = val
			}
			klog.V(4).Infof("Threshold config key: %s, value: %f", key, val)
		}
	} else {
		klog.V(4).Infof("Threshold arguments :%v", argsValue)
	}

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) error {
		for period, value := range up.threshold.cpuUsageAvg {
			klog.V(4).Infof("predicateFn cpuUsageAvg:%v", up.threshold.cpuUsageAvg)
			if node.ResourceUsage.CPUUsageAvg[period] > value {
				msg := fmt.Sprintf("Node %s cpu usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.CPUUsageAvg[period], value)
				return fmt.Errorf("plugin %s predicates failed %s", up.Name(), msg)
			}
		}

		for period, value := range up.threshold.memUsageAvg {
			klog.V(4).Infof("predicateFn memUsageAvg:%v", up.threshold.memUsageAvg)
			if node.ResourceUsage.MEMUsageAvg[period] > value {
				msg := fmt.Sprintf("Node %s mem usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.MEMUsageAvg[period], value)
				return fmt.Errorf("plugin %s memory usage predicates failed %s", up.Name(), msg)
			}
		}
		klog.V(4).Infof("Usage plugin filter for task %s/%s on node %s pass.", task.Namespace, task.Name, node.Name)
		return nil
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		score := 0.0
		cpuUsage, exist := node.ResourceUsage.CPUUsageAvg[cpuUsageAvg5m]
		klog.V(4).Infof("Node %s cpu usage is %f.", node.Name, cpuUsage)
		if !exist {
			return 0, nil
		}
		score = (100 - cpuUsage) / 100
		score *= float64(k8sFramework.MaxNodeScore * int64(up.weight))
		klog.V(4).Infof("Node %s score for task %s is %f.", node.Name, task.Name, score)
		return score, nil
	}

	ssn.AddPredicateFn(up.Name(), predicateFn)
	ssn.AddNodeOrderFn(up.Name(), nodeOrderFn)
}

func (up *usagePlugin) OnSessionClose(ssn *framework.Session) {}
