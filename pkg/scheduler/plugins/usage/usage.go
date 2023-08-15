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

	"k8s.io/klog/v2"
	k8sFramework "k8s.io/kubernetes/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName       = "usage"
	thresholdSection = "thresholds"
)

/*
   actions: "enqueue, allocate, backfill"
   tiers:
   - plugins:
     - name: usage
       arguments:
         usage.weight: 10
         type: average
         thresholds:
           cpu: 70
           mem: 70
         period: 10m
*/

const AVG string = "average"
const COMMON string = "common"
const MAX string = "max"

type usagePlugin struct {
	pluginArguments framework.Arguments
	weight          int
	usageType       string
	cpuThresholds   float64
	memThresholds   float64
	period          string
}

// New function returns usagePlugin object
func New(args framework.Arguments) framework.Plugin {
	var plugin *usagePlugin = &usagePlugin{
		pluginArguments: args,
		weight:          1,
		usageType:       AVG,
		cpuThresholds:   80,
		memThresholds:   80,
		period:          "10m",
	}
	args.GetInt(&plugin.weight, "usage.weight")

	if averageStr, ok := args["type"]; ok {
		if average, success := averageStr.(string); success {
			plugin.usageType = average
		} else {
			klog.Warningf("usage parameter[%v] is wrong", args)
		}
	}

	if periodStr, ok := args["period"]; ok {
		if period, success := periodStr.(string); success {
			plugin.period = period
		} else {
			klog.Warningf("usage parameter[%v] is wrong", args)
		}
	}
	return plugin
}

func (up *usagePlugin) Name() string {
	return PluginName
}

func (up *usagePlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(5).Infof("Enter usage plugin ...")
	defer func() {
		klog.V(5).Infof("Leaving usage plugin ...")
	}()

	if klog.V(4).Enabled() {
		for node := range ssn.Nodes {
			usage := ssn.Nodes[node].ResourceUsage
			klog.V(4).Infof("node:%v, cpu usage:%v, mem usage:%v", node, usage.CPUUsageAvg, usage.MEMUsageAvg)
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
			value, _ := v.(int)
			switch key {
			case "cpu":
				up.cpuThresholds = float64(value)
			case "mem":
				up.memThresholds = float64(value)
			}
		}
	} else {
		klog.V(4).Infof("Threshold arguments :%v", argsValue)
	}

	predicateFn := func(task *api.TaskInfo, node *api.NodeInfo) ([]*api.Status, error) {
		predicateStatus := make([]*api.Status, 0)
		usageStatus := &api.Status{}
		if up.period != "" {
			klog.V(4).Infof("predicateFn cpuUsageAvg:%v,predicateFn memUsageAvg:%v", up.cpuThresholds, up.memThresholds)
			if node.ResourceUsage.CPUUsageAvg[up.period] > up.cpuThresholds {
				msg := fmt.Sprintf("Node %s cpu usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.CPUUsageAvg[up.period], up.cpuThresholds)
				usageStatus.Code = api.Unschedulable
				usageStatus.Reason = msg
				predicateStatus = append(predicateStatus, usageStatus)
				return predicateStatus, fmt.Errorf("plugin %s predicates failed %s", up.Name(), msg)

			}
			if node.ResourceUsage.MEMUsageAvg[up.period] > up.memThresholds {
				msg := fmt.Sprintf("Node %s mem usage %f exceeds the threshold %f", node.Name, node.ResourceUsage.MEMUsageAvg[up.period], up.memThresholds)
				usageStatus.Code = api.Unschedulable
				usageStatus.Reason = msg
				predicateStatus = append(predicateStatus, usageStatus)
				return predicateStatus, fmt.Errorf("plugin %s memory usage predicates failed %s", up.Name(), msg)

			}
		}
		usageStatus.Code = api.Success
		predicateStatus = append(predicateStatus, usageStatus)
		klog.V(4).Infof("Usage plugin filter for task %s/%s on node %s pass.", task.Namespace, task.Name, node.Name)
		return predicateStatus, nil
	}

	nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		score := 0.0
		if up.period == "" {
			return 0, nil
		}
		cpuUsage, exist := node.ResourceUsage.CPUUsageAvg[up.period]
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
