package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pgPendingPhaseNum = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "podgroup_pending_phase_num",
			Help:      "Number of podgroup at pending phase",
		}, []string{"queue_name"},
	)

	pgRunningPhaseNum = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "podgroup_running_phase_num",
			Help:      "Number of podgroup at running phase",
		}, []string{"queue_name"},
	)

	pgUnknownPhaseNum = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "podgroup_unknown_phase_num",
			Help:      "Number of podgroup at unknown phase",
		}, []string{"queue_name"},
	)

	pgInqueuePhaseNum = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "podgroup_inqueue_phase_num",
			Help:      "Number of podgroup at inqueue phase",
		}, []string{"queue_name"},
	)

	pgCompletedPhaseNum = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "podgroup_completed_phase_num",
			Help:      "Number of podgroup at completed phase",
		}, []string{"queue_name"},
	)
)

// UpdatePgPendingPhaseNum recored the num of podgroups at pending state in queue
func UpdatePgPendingPhaseNum(queueName string, num float64) {
	pgPendingPhaseNum.WithLabelValues(queueName).Set(num)
}

// UpdatePgRunningPhaseNum recored the num of podgroups at running state in queue
func UpdatePgRunningPhaseNum(queueName string, num float64) {
	pgRunningPhaseNum.WithLabelValues(queueName).Set(num)
}

// UpdatePgUnknownPhaseNum recored the num of podgroups at unknown state in queue
func UpdatePgUnknownPhaseNum(queueName string, num float64) {
	pgUnknownPhaseNum.WithLabelValues(queueName).Set(num)
}

// UpdatePgInqueuePhaseNum recored the num of podgroups at inqueue state in queue
func UpdatePgInqueuePhaseNum(queueName string, num float64) {
	pgInqueuePhaseNum.WithLabelValues(queueName).Set(num)
}

// UpdatePgCompletedPhaseNum recored the num of podgroups at completed state in queue
func UpdatePgCompletedPhaseNum(queueName string, num float64) {
	pgCompletedPhaseNum.WithLabelValues(queueName).Set(num)
}
