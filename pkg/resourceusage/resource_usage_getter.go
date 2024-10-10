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

package resourceusage

import (
	"time"

	"github.com/prometheus/prometheus/prompb"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/metriccollect"
	"volcano.sh/volcano/pkg/metriccollect/local"
)

// Resource include the overSubscription resource on this node
type Resource map[v1.ResourceName]int64

// Getter is used to get cpu/memory usage of current node.
type Getter interface {
	// UsagesByValue return absolute resource usage of node
	UsagesByValue(includeGuaranteedPods bool) Resource
	// UsagesByPercentage return resource usage percentage of node
	UsagesByPercentage(node *v1.Node) Resource
}

// getter implements Getter.
type getter struct {
	collectorName string
	collector     *metriccollect.MetricCollectorManager
}

// NewUsageGetter create a usage getter
func NewUsageGetter(mgr *metriccollect.MetricCollectorManager, collectorName string) Getter {
	g := &getter{
		collectorName: collectorName,
		collector:     mgr,
	}
	return g
}

// UsagesByValue return absolute resource usage of node
func (g *getter) UsagesByValue(includeGuaranteedPods bool) Resource {
	return g.commonUsage(includeGuaranteedPods)
}

// UsagesByPercentage return resource usage percentage of node
func (g *getter) UsagesByPercentage(node *v1.Node) Resource {
	res := make(Resource)
	usage := g.commonUsage(true)

	cpuAllocatable := node.Status.Allocatable[v1.ResourceCPU]
	memoryAllocatable := node.Status.Allocatable[v1.ResourceMemory]
	cpuTotal := cpuAllocatable.MilliValue()
	memoryTotal := memoryAllocatable.Value()

	if cpuTotal == 0 || memoryTotal == 0 {
		klog.ErrorS(nil, "Failed to get total resource of cpu or memory")
		return res
	}

	res[v1.ResourceCPU] = usage[v1.ResourceCPU] * 100 / cpuTotal
	res[v1.ResourceMemory] = usage[v1.ResourceMemory] * 100 / memoryTotal

	return res
}

func (g *getter) convertMetric(metric []*prompb.TimeSeries) int64 {
	ret := int64(0)
	if len(metric) == 0 {
		return ret
	}
	for _, ts := range metric {
		for _, value := range ts.Samples {
			ret += int64(value.Value)
		}
	}
	return ret
}

func (g *getter) commonUsage(includeGuaranteedPods bool) Resource {
	res := make(Resource)
	c, err := g.collector.GetPluginByName(g.collectorName)
	if err != nil {
		klog.ErrorS(err, "Failed to collector plugin", "name", local.CollectorName)
		return res
	}

	includeSystemUsed := g.collector.Config.GenericConfiguration.IncludeSystemUsage
	metricInfos := map[v1.ResourceName]*local.LocalMetricInfo{
		v1.ResourceCPU:    {ResourceType: "cpu", IncludeGuaranteedPods: includeGuaranteedPods, IncludeSystemUsed: includeSystemUsed},
		v1.ResourceMemory: {ResourceType: "memory", IncludeSystemUsed: includeSystemUsed},
	}

	for resType, metricInfo := range metricInfos {
		metric, err := c.CollectMetrics(metricInfo, time.Time{}, metav1.Duration{})
		if err != nil {
			klog.ErrorS(err, "Failed to collector cpu metric", "resType", resType)
			continue
		}
		sumUsage := g.convertMetric(metric)
		res[resType] = sumUsage
	}

	return res
}
