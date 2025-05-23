/*
Copyright 2025 The Volcano Authors.

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

package state

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"volcano.sh/volcano/pkg/controllers/util"
)

var (
	jobflowSucceedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "jobflow_succeed_phase_count",
			Help:      "Number of jobflow succeed phase",
		}, []string{"jobflow_namespace"},
	)

	jobflowFailedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "jobflow_failed_phase_count",
			Help:      "Number of jobflow failed phase",
		}, []string{"jobflow_namespace"},
	)
)

func UpdateJobFlowSucceed(namespace string) {
	jobflowSucceedPhaseCount.WithLabelValues(namespace).Inc()
}

func UpdateJobFlowFailed(namespace string) {
	jobflowFailedPhaseCount.WithLabelValues(namespace).Inc()
}

func DeleteJobFlowMetrics(namespace string) {
	jobflowSucceedPhaseCount.DeleteLabelValues(namespace)
	jobflowFailedPhaseCount.DeleteLabelValues(namespace)
}
