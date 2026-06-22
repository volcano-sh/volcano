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
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUpdateJobCompleted(t *testing.T) {
	jobCompletedPhaseCount.Reset()

	UpdateJobCompleted("ns1/job-a", "q1")
	UpdateJobCompleted("ns1/job-a", "q1")
	UpdateJobCompleted("ns1/job-b", "q2")

	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns1/job-a", "q1")); got != 2 {
		t.Errorf("ns1/job-a@q1 = %v, want 2", got)
	}
	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns1/job-b", "q2")); got != 1 {
		t.Errorf("ns1/job-b@q2 = %v, want 1", got)
	}
	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns1/unknown", "q1")); got != 0 {
		t.Errorf("unknown label combo = %v, want 0", got)
	}
}

func TestUpdateJobFailed(t *testing.T) {
	jobFailedPhaseCount.Reset()

	UpdateJobFailed("ns1/job-a", "q1")
	UpdateJobFailed("ns1/job-a", "q1")
	UpdateJobFailed("ns1/job-a", "q1")

	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns1/job-a", "q1")); got != 3 {
		t.Errorf("ns1/job-a@q1 = %v, want 3", got)
	}
}

func TestDeleteJobMetrics(t *testing.T) {
	jobCompletedPhaseCount.Reset()
	jobFailedPhaseCount.Reset()

	UpdateJobCompleted("ns1/job-a", "q1")
	UpdateJobFailed("ns1/job-a", "q1")
	UpdateJobCompleted("ns1/job-b", "q2")

	DeleteJobMetrics("ns1/job-a", "q1")

	if got := testutil.CollectAndCount(jobCompletedPhaseCount); got != 1 {
		t.Errorf("completed series count = %d, want 1 (only job-b should remain)", got)
	}
	if got := testutil.CollectAndCount(jobFailedPhaseCount); got != 0 {
		t.Errorf("failed series count = %d, want 0", got)
	}
	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns1/job-b", "q2")); got != 1 {
		t.Errorf("ns1/job-b@q2 = %v, want 1 (untouched)", got)
	}
}

func TestMetricsAreIndependentAcrossLabels(t *testing.T) {
	jobCompletedPhaseCount.Reset()
	jobFailedPhaseCount.Reset()

	UpdateJobCompleted("ns/job", "q1")
	UpdateJobFailed("ns/job", "q1")

	if got := testutil.ToFloat64(jobCompletedPhaseCount.WithLabelValues("ns/job", "q1")); got != 1 {
		t.Errorf("completed = %v, want 1", got)
	}
	if got := testutil.ToFloat64(jobFailedPhaseCount.WithLabelValues("ns/job", "q1")); got != 1 {
		t.Errorf("failed = %v, want 1", got)
	}
}
