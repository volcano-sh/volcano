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

package util

import "testing"

func TestParseSchedulerCounterValue(t *testing.T) {
	raw := []byte(`# HELP volcano_unschedulable_job_cache_wakeups_total Number of cached Jobs woken.
# TYPE volcano_unschedulable_job_cache_wakeups_total counter
volcano_unschedulable_job_cache_wakeups_total{action="Delete",job_name="job-a",job_namespace="ns",resource="Pod"} 2
volcano_unschedulable_job_cache_wakeups_total{action="Add",job_name="job-a",job_namespace="ns",resource="Node"} 3
`)

	got, err := parseSchedulerCounterValue(raw, "volcano_unschedulable_job_cache_wakeups_total", map[string]string{
		"job_namespace": "ns",
		"job_name":      "job-a",
	})
	if err != nil {
		t.Fatalf("parseSchedulerCounterValue() error = %v", err)
	}
	if got != 5 {
		t.Fatalf("parseSchedulerCounterValue() = %v, want 5", got)
	}
}
