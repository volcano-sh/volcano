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

package source

import (
	"context"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
)

const (
	MetricsTypePrometheus        = "prometheus"
	MetricsTypePrometheusAdaptor = "prometheus_adaptor"
)

type Metrics struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type NodeMetrics struct {
	CPU    float64
	Memory float64
}

type MetricsClient interface {
	NodesMetricsAvg(ctx context.Context, nodesMetrics map[string]*NodeMetrics, period string) error
	PodsMetricsAvg(ctx context.Context, pods []*v1.Pod, period string) (map[types.NamespacedName]map[v1.ResourceName]*resource.Quantity, error)
}

func NewMetricsClient(metricsConf Metrics) (MetricsClient, error) {
	metricsType := metricsConf.Type
	if len(metricsType) == 0 {
		return nil, errors.New("Metrics type is empty, the load-aware rescheduling function does not take effect.")
	}
	if metricsType == MetricsTypePrometheus {
		return NewPrometheusMetricsClient(metricsConf)
	} else if metricsType == MetricsTypePrometheusAdaptor {
		return NewCustomMetricsClient()
	} else {
		return nil, fmt.Errorf("Data cannot be collected from the %s monitoring system. "+
			"The supported monitoring systems are %s and %s.",
			metricsType, MetricsTypePrometheus, MetricsTypePrometheusAdaptor)
	}

}
