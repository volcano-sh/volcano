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

package state

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"volcano.sh/volcano/pkg/controllers/util"
)

var (
	jobCompletedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "job_completed_phase_count",
			Help:      "Number of job completed phase",
		}, []string{"job_name", "queue_name"},
	)

	jobFailedPhaseCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "job_failed_phase_count",
			Help:      "Number of job failed phase",
		}, []string{"job_name", "queue_name"},
	)
)

func UpdateJobCompleted(jobName, queueName string) {
	jobCompletedPhaseCount.WithLabelValues(jobName, queueName).Inc()
}

func UpdateJobFailed(jobName, queueName string) {
	jobFailedPhaseCount.WithLabelValues(jobName, queueName).Inc()
}

func DeleteJobMetrics(jobName, queueName string) {
	jobCompletedPhaseCount.DeleteLabelValues(jobName, queueName)
	jobFailedPhaseCount.DeleteLabelValues(jobName, queueName)
}
