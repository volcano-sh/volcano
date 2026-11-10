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
	// namespaceShare carries a "queue" and "resource" label because a
	// namespace's share is only meaningful relative to a specific queue's
	// capacity for a specific tracked resource (e.g. a namespace can have
	// different shares in different queues, or for cpu vs. nvidia.com/gpu).
	namespaceShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "namespace_share",
			Help:      "Share for one namespace in one queue, for one resource",
		}, []string{"namespace_name", "queue", "resource"},
	)

	namespaceWeight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "namespace_weight",
			Help:      "Weight for one namespace",
		}, []string{"namespace_name"},
	)

	namespaceWeightedShare = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "namespace_weighted_share",
			Help:      "Weighted share for one namespace",
		}, []string{"namespace_name"},
	)

	// namespaceDecayedUsage is distinct from namespaceShare: share is a
	// point-in-time entitlement recomputed fresh each cycle from current
	// demand, while decayed usage is a running resource-seconds total that
	// persists and decays across cycles (see the fairshare plugin's decay
	// model). Different units and semantics, so it gets its own gauge
	// rather than reusing namespace_share.
	namespaceDecayedUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "namespace_decayed_usage",
			Help:      "Decayed cumulative resource-seconds usage for one namespace in one queue",
		}, []string{"namespace_name", "queue", "resource"},
	)
)

// UpdateNamespaceShare records share for one namespace in one queue, for one resource
func UpdateNamespaceShare(namespaceName, queue, resource string, share float64) {
	namespaceShare.WithLabelValues(namespaceName, queue, resource).Set(share)
}

// UpdateNamespaceWeight records weight for one namespace
func UpdateNamespaceWeight(namespaceName string, weight int64) {
	namespaceWeight.WithLabelValues(namespaceName).Set(float64(weight))
}

// UpdateNamespaceWeightedShare records weighted share for one namespace
func UpdateNamespaceWeightedShare(namespaceName string, weightedShare float64) {
	namespaceWeightedShare.WithLabelValues(namespaceName).Set(weightedShare)
}

// UpdateNamespaceDecayedUsage records decayed cumulative resource-seconds
// usage for one namespace in one queue, for one resource
func UpdateNamespaceDecayedUsage(namespaceName, queue, resource string, usage float64) {
	namespaceDecayedUsage.WithLabelValues(namespaceName, queue, resource).Set(usage)
}
