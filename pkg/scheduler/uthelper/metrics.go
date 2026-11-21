/*
Copyright 2026 The Volcano Authors.

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

package uthelper

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// E2eJobSchedulingMetrics reads the e2e job scheduling metrics from the default registry, as
// they are exposed on /metrics: the number of samples in the latency histogram, which is
// shared by all jobs, and whether the per-job duration gauge has a series for the given job.
func E2eJobSchedulingMetrics(namespace, jobName string) (latencySamples uint64, hasDuration bool, err error) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, false, fmt.Errorf("failed to gather metrics: %v", err)
	}
	for _, family := range families {
		switch family.GetName() {
		case metrics.VolcanoSubSystemName + "_e2e_job_scheduling_latency_milliseconds":
			latencySamples = family.GetMetric()[0].GetHistogram().GetSampleCount()
		case metrics.VolcanoSubSystemName + "_e2e_job_scheduling_duration":
			for _, m := range family.GetMetric() {
				labels := map[string]string{}
				for _, l := range m.GetLabel() {
					labels[l.GetName()] = l.GetValue()
				}
				if labels["job_name"] == jobName && labels["job_namespace"] == namespace {
					hasDuration = true
				}
			}
		}
	}
	return latencySamples, hasDuration, nil
}
