package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

var (
	queuePodGroupInqueue = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "queue_pod_group_inqueue_count",
			Help:      "The number of Inqueue PodGroup in this queue",
		}, []string{"queue_name"},
	)

	queuePodGroupPending = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "queue_pod_group_pending_count",
			Help:      "The number of Pending PodGroup in this queue",
		}, []string{"queue_name"},
	)

	queuePodGroupRunning = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "queue_pod_group_running_count",
			Help:      "The number of Running PodGroup in this queue",
		}, []string{"queue_name"},
	)

	queuePodGroupUnknown = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "queue_pod_group_unknown_count",
			Help:      "The number of Unknown PodGroup in this queue",
		}, []string{"queue_name"},
	)

	queuePodGroupCompleted = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: metrics.VolcanoNamespace,
			Name:      "queue_pod_group_completed_count",
			Help:      "The number of Completed PodGroup in this queue",
		}, []string{"queue_name"},
	)
)

// UpdateQueuePodGroupInqueueCount records the number of Inqueue PodGroup in this queue
func UpdateQueuePodGroupInqueueCount(queueName string, count int32) {
	queuePodGroupInqueue.WithLabelValues(queueName).Set(float64(count))
}

// UpdateQueuePodGroupPendingCount records the number of Pending PodGroup in this queue
func UpdateQueuePodGroupPendingCount(queueName string, count int32) {
	queuePodGroupPending.WithLabelValues(queueName).Set(float64(count))
}

// UpdateQueuePodGroupRunningCount records the number of Running PodGroup in this queue
func UpdateQueuePodGroupRunningCount(queueName string, count int32) {
	queuePodGroupRunning.WithLabelValues(queueName).Set(float64(count))
}

// UpdateQueuePodGroupUnknownCount records the number of Unknown PodGroup in this queue
func UpdateQueuePodGroupUnknownCount(queueName string, count int32) {
	queuePodGroupUnknown.WithLabelValues(queueName).Set(float64(count))
}

// UpdateQueuePodGroupCompletedCount records the number of Completed PodGroup in this queue
func UpdateQueuePodGroupCompletedCount(queueName string, count int32) {
	queuePodGroupCompleted.WithLabelValues(queueName).Set(float64(count))
}

// DeleteQueueMetrics delete all metrics related to the queue
func DeleteQueueMetrics(queueName string) {
	queuePodGroupInqueue.DeleteLabelValues(queueName)
	queuePodGroupPending.DeleteLabelValues(queueName)
	queuePodGroupRunning.DeleteLabelValues(queueName)
	queuePodGroupUnknown.DeleteLabelValues(queueName)
	queuePodGroupCompleted.DeleteLabelValues(queueName)
}

func UpdateQueueMetrics(queueName string, queueStatus *v1beta1.QueueStatus) {
	UpdateQueuePodGroupPendingCount(queueName, queueStatus.Pending)
	UpdateQueuePodGroupRunningCount(queueName, queueStatus.Running)
	UpdateQueuePodGroupUnknownCount(queueName, queueStatus.Unknown)
	UpdateQueuePodGroupInqueueCount(queueName, queueStatus.Inqueue)
	UpdateQueuePodGroupCompletedCount(queueName, queueStatus.Completed)
}
