package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	VqaSuccessCntMetric = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xuanwu_op_queue_scale_success_count",
			Help: "Counter of successes in queue auto scaling",
		},
		[]string{"QueueName", "TenantName"},
	)

	VqaFailureCntMetric = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xuanwu_op_queue_scale_failed_count",
			Help: "Counter of failures in queue auto scaling",
		},
		[]string{"QueueName", "TenantName"},
	)
)

func UpdateVqaSuccessMetrics(queueName, tenantName string) {
	println(VqaSuccessCntMetric.WithLabelValues(queueName, tenantName).Desc().String())
	VqaSuccessCntMetric.WithLabelValues(queueName, tenantName).Inc()
}

func UpdateVqaFailMetrics(queueName, tenantName string) {
	VqaFailureCntMetric.WithLabelValues(queueName, tenantName).Inc()
}

func DeleteVqaMetrics(queueName, tenantName string) {
	VqaSuccessCntMetric.DeleteLabelValues(queueName, tenantName)
	VqaFailureCntMetric.DeleteLabelValues(queueName, tenantName)
}

//func init() {
//	prometheus.MustRegister(VqaSuccessCntMetric, VqaFailureCntMetric)
//}
