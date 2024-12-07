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

package framework

import (
	"time"

	"github.com/prometheus/prometheus/prompb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MetricCollect interface {
	// MetricCollectorName returns the name of metric collector
	MetricCollectorName() string

	// Run starts/initializes the metric collector
	Run() error

	// CollectMetrics collects metrics from collector.
	// If window == 0, return the latest data, otherwise return the time series data
	CollectMetrics(metricInfo interface{}, start time.Time, window metav1.Duration) ([]*prompb.TimeSeries, error)
}
