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
	"time"

	vmetrics "volcano.sh/volcano/pkg/scheduler/metrics"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"k8s.io/component-base/metrics"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"
)

const (
	// VolcanoSubSystemName - subsystem name in prometheus used by volcano
	VolcanoSubSystemName = "volcano-agent-scheduler"

	// OnSchedulingSycleOpen label
	OnSchedulingSycleOpen = "OnSessionOpen"

	// OnSchedulingSycleClose label
	OnSchedulingSycleClose = "OnSessionClose"
)

var (
	e2ePodSchedulingDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_pod_scheduling_duration",
			Help:      "E2E pod scheduling duration",
		},
		[]string{"pod_name", "pod_namespace"},
	)

	e2ePodSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_pod_scheduling_latency_milliseconds",
			Help:      "E2e pod scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(32, 2, 10),
		},
	)
)

// InitKubeSchedulerRelatedMetrics is used to init metrics global variables in k8s.io/kubernetes/pkg/scheduler/metrics/metrics.go.
// We don't use InitMetrics() to init all global variables because currently only "Goroutines" is required when calling kube-scheduler
// related plugins. And there is no need to export these metrics, therefore currently initialization is enough.
func InitKubeSchedulerRelatedMetrics() {
	k8smetrics.Goroutines = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      VolcanoSubSystemName,
			Name:           "goroutines",
			Help:           "Number of running goroutines split by the work they do such as binding.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"operation"})
}

// UpdateE2eSchedulingDurationByPod updates entire end to end scheduling duration
func UpdateE2eSchedulingDurationByPod(podName string, namespace string, duration time.Duration) {
	e2ePodSchedulingDuration.WithLabelValues(podName, namespace).Set(vmetrics.DurationInMilliseconds(duration))
	e2ePodSchedulingLatency.Observe(vmetrics.DurationInMilliseconds(duration))
}
