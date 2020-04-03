package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
)

var (
	nsShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_share",
			Help:      "Share for one namespace",
		}, []string{"ns_name"},
	)

	nsWeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_weight",
			Help:      "Weight for one namespace",
		}, []string{"ns_name"},
	)

	nsWeightedShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_weighted_share",
			Help:      "Weighted share for one namespace",
		}, []string{"ns_name"},
	)
)

// UpdateNamespaceShare records share for one namespace
func UpdateNamespaceShare(nsName string, share float64) {
	nsShare.WithLabelValues(nsName).Set(share)
}

// UpdateNamespaceWeight records weight for one namespace
func UpdateNamespaceWeight(nsName string, weight int64) {
	nsWeight.WithLabelValues(nsName).Set(float64(weight))
}

// UpdateNamespaceWeightedShare records weighted share for one namespace
func UpdateNamespaceWeightedShare(nsName string, weightedShare float64) {
	nsWeightedShare.WithLabelValues(nsName).Set(weightedShare)
}
