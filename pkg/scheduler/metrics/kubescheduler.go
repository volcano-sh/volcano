/*
Copyright 2025 The Volcano Authors.

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

package metrics

import (
	"sync"

	componentbasemetrics "k8s.io/component-base/metrics"
	volumebindingmetrics "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding/metrics"
	schedulermetrics "k8s.io/kubernetes/pkg/scheduler/metrics"
)

var (
	metricsOnce sync.Once
	metricsList []componentbasemetrics.Registerable
)

func initMetrics() {
	// Currently we only initialize FrameworkExtensionPointDuration,PluginExecutionDuration,PluginEvaluationTotal,Goroutines
	// these four metrics, we do not need other metrics introduced in kube-scheduler right now
	schedulermetrics.FrameworkExtensionPointDuration = componentbasemetrics.NewHistogramVec(
		&componentbasemetrics.HistogramOpts{
			Subsystem: VolcanoNamespace,
			Name:      "framework_extension_point_duration_seconds",
			Help:      "Latency for running all plugins of a specific extension point.",
			// Start with 0.1ms with the last bucket being [~200ms, Inf)
			Buckets:        componentbasemetrics.ExponentialBuckets(0.0001, 2, 12),
			StabilityLevel: componentbasemetrics.STABLE,
		},
		[]string{"extension_point", "status", "profile"})

	schedulermetrics.PluginExecutionDuration = componentbasemetrics.NewHistogramVec(
		&componentbasemetrics.HistogramOpts{
			Subsystem: VolcanoNamespace,
			Name:      "plugin_execution_duration_seconds",
			Help:      "Duration for running a plugin at a specific extension point.",
			// Start with 0.01ms with the last bucket being [~22ms, Inf). We use a small factor (1.5)
			// so that we have better granularity since plugin latency is very sensitive.
			Buckets:        componentbasemetrics.ExponentialBuckets(0.00001, 1.5, 20),
			StabilityLevel: componentbasemetrics.ALPHA,
		},
		[]string{"plugin", "extension_point", "status"})

	schedulermetrics.PluginEvaluationTotal = componentbasemetrics.NewCounterVec(
		&componentbasemetrics.CounterOpts{
			Subsystem:      VolcanoNamespace,
			Name:           "plugin_evaluation_total",
			Help:           "Number of attempts to schedule pods by each plugin and the extension point (available only in PreFilter, Filter, PreScore, and Score).",
			StabilityLevel: componentbasemetrics.ALPHA,
		}, []string{"plugin", "extension_point", "profile"})

	schedulermetrics.Goroutines = componentbasemetrics.NewGaugeVec(
		&componentbasemetrics.GaugeOpts{
			Subsystem:      VolcanoNamespace,
			Name:           "goroutines",
			Help:           "Number of running goroutines split by the work they do such as binding.",
			StabilityLevel: componentbasemetrics.ALPHA,
		}, []string{"operation"})

	metricsList = []componentbasemetrics.Registerable{
		schedulermetrics.FrameworkExtensionPointDuration,
		schedulermetrics.PluginExecutionDuration,
		schedulermetrics.PluginEvaluationTotal,
		schedulermetrics.Goroutines,
	}
}

func RegisterMetrics() {
	// Register all metrics
	metricsOnce.Do(func() {
		initMetrics()
		schedulermetrics.RegisterMetrics(metricsList...)
		volumebindingmetrics.RegisterVolumeSchedulingMetrics()
	})
}
