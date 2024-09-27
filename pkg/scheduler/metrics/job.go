/*
Copyright 2020 The Volcano Authors.

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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
)

var (
	jobShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "job_share",
			Help:      "Share for one job",
		}, []string{"job_ns", "job_id"},
	)

	jobRetryCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoNamespace,
			Name:      "job_retry_counts",
			Help:      "Number of retry counts for one job",
		}, []string{"job_id"},
	)

	jobSucceededCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoNamespace,
			Name:      "job_succeeded_counts",
			Help:      "Number of succeeded job counts",
		}, []string{"job_ns"},
	)

	jobFailedCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoNamespace,
			Name:      "job_failed_counts",
			Help:      "Number of failed job counts",
		}, []string{"job_name", "queue", "job_ns"},
	)
)

// UpdateJobShare records share for one job
func UpdateJobShare(jobNs, jobID string, share float64) {
	jobShare.WithLabelValues(jobNs, jobID).Set(share)
}

// RegisterJobRetries total number of job retries.
func RegisterJobRetries(jobID string) {
	jobRetryCount.WithLabelValues(jobID).Inc()
}

// RegisterJobSucceeded total number of job succeeded.
func RegisterJobSucceeded(jobNs string) {
	jobSucceededCount.WithLabelValues(jobNs).Inc()
}

// RegisterJobFailed total number of job failed.
func RegisterJobFailed(jobName, queue, jobNs string) {
	jobFailedCount.WithLabelValues(jobName, queue, jobNs).Inc()
}

// DeleteJobMetrics delete all metrics related to the job
func DeleteJobMetrics(jobName, queue, namespace string) {
	e2eJobSchedulingDuration.DeleteLabelValues(jobName, queue, namespace)
	e2eJobSchedulingStartTime.DeleteLabelValues(jobName, queue, namespace)
	e2eJobSchedulingLastTime.DeleteLabelValues(jobName, queue, namespace)
	unscheduleTaskCount.DeleteLabelValues(jobName)
	jobShare.DeleteLabelValues(namespace, jobName)
	jobRetryCount.DeleteLabelValues(jobName)
	jobSucceededCount.DeleteLabelValues(namespace)
	jobFailedCount.DeleteLabelValues(jobName, queue, namespace)
}
