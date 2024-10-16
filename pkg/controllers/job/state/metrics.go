package state

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"volcano.sh/volcano/pkg/scheduler/metrics"
)

var (
	jobCompletedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "job_completed_phase_count",
			Help:      "Number of job completed phase",
		}, []string{"job_name", "queue_name"},
	)

	jobFailedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "job_failed_phase_count",
			Help:      "Number of job failed phase",
		}, []string{"job_name", "queue_name"},
	)
)

func UpdateJobCompleted(jobName, queueName string) {
	jobCompletedPhaseCount.WithLabelValues(jobName, queueName).Inc()
}

func UpdateJobFailed(jobName, queueName string) {
	jobFailedPhaseCount.WithLabelValues(jobName, queueName).Inc()
}

func DeleteJobMetrics(jobName, queueName string) {
	jobCompletedPhaseCount.DeleteLabelValues(jobName, queueName)
	jobFailedPhaseCount.DeleteLabelValues(jobName, queueName)
}
