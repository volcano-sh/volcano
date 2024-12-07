/*
Copyright 2024 The Volcano Authors.

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
	"github.com/prometheus/client_golang/prometheus/promauto"

	"volcano.sh/volcano/pkg/agent/apis"
)

const (
	subSystem = "volcano_agent"
)

var overSubscriptionResourceQuantity = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Subsystem: subSystem,
		Name:      "oversubscription_resource_quantity",
		Help:      "The reported overSubscription resource quantity",
	},
	[]string{"node", "resource"},
)

// UpdateOverSubscriptionResourceQuantity update node overSubscription resource by resource name.
func UpdateOverSubscriptionResourceQuantity(nodeName string, resources apis.Resource) {
	for resName, quantity := range resources {
		overSubscriptionResourceQuantity.WithLabelValues(nodeName, string(resName)).Set(float64(quantity))
	}
}
