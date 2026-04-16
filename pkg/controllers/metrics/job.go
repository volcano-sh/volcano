/*
Copyright 2026 The Volcano Authors.

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

	"volcano.sh/volcano/pkg/controllers/util"
)

var (
	// jobToPodCreationLatency is the per-pod latency from VCJob creation to pod created.
	jobToPodCreationLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "controller_job_to_pod_creation_latency_milliseconds",
			Help:      "Latency from VCJob creation to pod created in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		},
	)

	// jobE2EPodCreationDuration is the end-to-end duration from VCJob creation to all pods created.
	// It is a per-job Gauge labeled by job_name, queue, and job_namespace.
	jobE2EPodCreationDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "controller_job_e2e_creation_duration_milliseconds",
			Help:      "End-to-end duration from VCJob creation to all pods created, in milliseconds",
		},
		[]string{"job_name", "queue", "job_namespace"},
	)
)

// DurationInMilliseconds converts a time.Duration to float64 milliseconds.
func DurationInMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

// Duration returns the elapsed time since start.
func Duration(start time.Time) time.Duration {
	return time.Since(start)
}

// ObserveJobToPodCreationLatency observes the latency from job creation to a single pod created.
func ObserveJobToPodCreationLatency(duration time.Duration) {
	jobToPodCreationLatency.Observe(DurationInMilliseconds(duration))
}

// SetJobE2EPodCreationDuration sets the e2e duration from VCJob creation to all pods created.
func SetJobE2EPodCreationDuration(jobName, queue, namespace string, duration time.Duration) {
	jobE2EPodCreationDuration.WithLabelValues(jobName, queue, namespace).Set(DurationInMilliseconds(duration))
}

// DeleteJobE2ECreationMetrics deletes the e2e creation duration metric for a job.
func DeleteJobE2ECreationMetrics(jobName, queue, namespace string) {
	jobE2EPodCreationDuration.DeleteLabelValues(jobName, queue, namespace)
}
