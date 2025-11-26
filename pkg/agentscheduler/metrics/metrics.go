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

	pluginSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "plugin_scheduling_latency_milliseconds",
			Help:      "Plugin scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		}, []string{"plugin", "OnSchedulingSycle"},
	)

	actionSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "action_scheduling_latency_milliseconds",
			Help:      "Action scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		}, []string{"action"},
	)

	taskSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "task_scheduling_latency_milliseconds",
			Help:      "Task scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		},
	)

	scheduleAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "schedule_attempts_total",
			Help:      "Number of attempts to schedule pods, by the result. 'unschedulable' means a pod could not be scheduled, while 'error' means an internal scheduler problem.",
		}, []string{"result"},
	)
)

// UpdateE2eSchedulingDurationByPod updates entire end to end scheduling duration
func UpdateE2eSchedulingDurationByPod(podName string, namespace string, duration time.Duration) {
	e2ePodSchedulingDuration.WithLabelValues(podName, namespace).Set(vmetrics.DurationInMilliseconds(duration))
	e2ePodSchedulingLatency.Observe(vmetrics.DurationInMilliseconds(duration))
}
