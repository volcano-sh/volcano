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
		}, []string{"job_id", "queue_name"},
	)

	jobFailedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "job_failed_phase_count",
			Help:      "Number of job failed phase",
		}, []string{"job_id", "queue_name"},
	)
)

func RegisterJobCompleted(jobID, queueName string) {
	jobCompletedPhaseCount.WithLabelValues(jobID, queueName).Inc()
}

func RegisterJobFailed(jobID, queueName string) {
	jobFailedPhaseCount.WithLabelValues(jobID, queueName).Inc()
}

func DeleteJobMetrics(jobID, queueName string) {
	jobCompletedPhaseCount.DeleteLabelValues(jobID, queueName)
	jobFailedPhaseCount.DeleteLabelValues(jobID, queueName)
}
