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
	namespaceShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_share",
			Help:      "Share for one namespace",
		}, []string{"namespace_name"},
	)

	namespaceWeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_weight",
			Help:      "Weight for one namespace",
		}, []string{"namespace_name"},
	)

	namespaceWeightedShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoNamespace,
			Name:      "namespace_weighted_share",
			Help:      "Weighted share for one namespace",
		}, []string{"namespace_name"},
	)
)

// UpdateNamespaceShare records share for one namespace
func UpdateNamespaceShare(namespaceName string, share float64) {
	namespaceShare.WithLabelValues(namespaceName).Set(share)
}

// UpdateNamespaceWeight records weight for one namespace
func UpdateNamespaceWeight(namespaceName string, weight int64) {
	namespaceWeight.WithLabelValues(namespaceName).Set(float64(weight))
}

// UpdateNamespaceWeightedShare records weighted share for one namespace
func UpdateNamespaceWeightedShare(namespaceName string, weightedShare float64) {
	namespaceWeightedShare.WithLabelValues(namespaceName).Set(weightedShare)
}
