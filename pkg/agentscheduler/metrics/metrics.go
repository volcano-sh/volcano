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

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/component-base/metrics"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	schedulermetrics "volcano.sh/volcano/pkg/scheduler/metrics"
)

var (
	workerSchedulingCycleDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: schedulermetrics.VolcanoSubSystemName,
			Name:      "worker_scheduling_cycle_duration_milliseconds",
			Help:      "Duration of a single scheduling cycle execution in agent worker.",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		},
	)
)

// InitKubeSchedulerRelatedMetrics is used to init metrics global variables in k8s.io/kubernetes/pkg/scheduler/metrics/metrics.go.
// We don't use InitMetrics() to init all global variables because currently only "Goroutines" is required when calling kube-scheduler
// related plugins. And there is no need to export these metrics, therefore currently initialization is enough.
func InitKubeSchedulerRelatedMetrics() {
	k8smetrics.Goroutines = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      schedulermetrics.VolcanoSubSystemName,
			Name:           "goroutines",
			Help:           "Number of running goroutines split by the work they do such as binding.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"operation"})

	//TODO: k8smetrics is inited here to resolve startup NPE because backkoff queue rely on k8s metrics. This line need be removed if metrics depdendency issue is fixed
	k8smetrics.InitMetrics()
}

// UpdateWorkerSchedulingCycleDuration updates worker scheduling cycle duration
func UpdateWorkerSchedulingCycleDuration(duration time.Duration) {
	workerSchedulingCycleDuration.Observe(schedulermetrics.DurationInMilliseconds(duration))
}
