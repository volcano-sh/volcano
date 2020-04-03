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
)

// UpdateJobShare records share for one job
func UpdateJobShare(jobNs, jobID string, share float64) {
	jobShare.WithLabelValues(jobNs, jobID).Set(share)
}

// RegisterJobRetries total number of job retries.
func RegisterJobRetries(jobID string) {
	jobRetryCount.WithLabelValues(jobID).Inc()
}
