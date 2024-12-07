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

package testing

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/prompb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/agent/utils/cgroup"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect/framework"
	"volcano.sh/volcano/pkg/metriccollect/local"
)

const CollectorName = "FakeLocalCollector"

func init() {
	framework.RegisterMetricCollectFunc(NewFakeLocalCollector)
}

type FakeLocalCollector struct {
	CgroupManager          cgroup.CgroupManager
	InitiatedSubCollectors map[string]local.SubCollector
}

func NewFakeLocalCollector(config *config.Configuration, cgroupManager cgroup.CgroupManager) framework.MetricCollect {
	return &FakeLocalCollector{
		CgroupManager: cgroupManager,
		InitiatedSubCollectors: map[string]local.SubCollector{
			"cpu":    &FakeSubCollectorCPU{},
			"memory": &FakeSubCollectorMemory{},
		},
	}
}
func (f *FakeLocalCollector) Run() error {
	return nil
}

func (f *FakeLocalCollector) MetricCollectorName() string {
	return CollectorName
}

func (f *FakeLocalCollector) CollectMetrics(metricInfo interface{}, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error) {
	metric, ok := metricInfo.(*local.LocalMetricInfo)
	if !ok {
		return nil, fmt.Errorf("metricInfo phase invoked with an invalid data struct %s", metricInfo)
	}

	subCollector, ok := f.InitiatedSubCollectors[metric.ResourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported local sub collector %s", metric.ResourceType)
	}

	return subCollector.CollectLocalMetrics(metric, start, window)
}
