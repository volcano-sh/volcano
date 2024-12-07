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

package local

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/prompb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect/framework"
)

const CollectorName = "LocalCollector"

type LocalMetricInfo struct {
	ResourceType          string
	IncludeGuaranteedPods bool
	IncludeSystemUsed     bool
}

type SubCollector interface {
	// Run runs sub collector
	Run()
	// CollectLocalMetrics returns local metric from sub collector
	CollectLocalMetrics(metricInfo *LocalMetricInfo, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error)
}

func getSubLocalCollectors() map[string]func(cgroupManager cgroup.CgroupManager) (SubCollector, error) {
	initiatedCollectorFuncs := make(map[string]func(cgroupManager cgroup.CgroupManager) (SubCollector, error))
	initiatedCollectorFuncs["cpu"] = NewCPUResourceCollector
	initiatedCollectorFuncs["memory"] = NewMemoryResourceCollector
	return initiatedCollectorFuncs
}

func init() {
	framework.RegisterMetricCollectFunc(NewLocalCollector)
}

type LocalCollector struct {
	CgroupManager          cgroup.CgroupManager
	InitiatedSubCollectors map[string]SubCollector
}

func NewLocalCollector(config *config.Configuration, cgroupManager cgroup.CgroupManager) framework.MetricCollect {
	return &LocalCollector{
		CgroupManager: cgroupManager,
	}
}

func (c *LocalCollector) MetricCollectorName() string {
	return CollectorName
}

func (c *LocalCollector) Run() error {
	if c.InitiatedSubCollectors == nil {
		c.InitiatedSubCollectors = make(map[string]SubCollector)
	}
	for collectorName, collectorFunc := range getSubLocalCollectors() {
		klog.InfoS("Init local sub collector", "collectorName", collectorName)
		if collector, err := collectorFunc(c.CgroupManager); err != nil {
			return err
		} else {
			c.InitiatedSubCollectors[collectorName] = collector
			collector.Run()
		}
	}
	return nil
}

func (c *LocalCollector) CollectMetrics(metricInfo interface{}, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error) {
	metric, ok := metricInfo.(*LocalMetricInfo)
	if !ok {
		return nil, fmt.Errorf("metricInfo phase invoked with an invalid data struct %s", metricInfo)
	}

	subCollector, ok := c.InitiatedSubCollectors[metric.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported local sub collector %s", metric.ResourceType)
	}

	return subCollector.CollectLocalMetrics(metric, start, window)
}
