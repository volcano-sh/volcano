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

package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// e2eLatencySampleCount returns the number of samples observed in e2eJobSchedulingLatency.
func e2eLatencySampleCount(t *testing.T) uint64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() == VolcanoSubSystemName+"_e2e_job_scheduling_latency_milliseconds" {
			return family.GetMetric()[0].GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func TestUpdateE2eSchedulingByJob(t *testing.T) {
	const (
		jobName   = "e2e-job"
		queue     = "q1"
		namespace = "ns1"
	)
	creationTime := time.Now().Add(-time.Minute)
	t.Cleanup(func() { DeleteJobMetrics(jobName, queue, namespace) })

	before := e2eLatencySampleCount(t)
	UpdateE2eSchedulingByJob(jobName, queue, namespace, creationTime)
	if got := e2eLatencySampleCount(t); got != before+1 {
		t.Fatalf("latency samples after first commit = %d, want %d", got, before+1)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingDuration.WithLabelValues(jobName, queue, namespace)); got < float64(time.Minute.Milliseconds()) {
		t.Fatalf("job scheduling duration = %vms, want at least %vms", got, time.Minute.Milliseconds())
	}
	firstLastTime := testutil.ToFloat64(e2eJobSchedulingLastTime.WithLabelValues(jobName, queue, namespace))

	// A later commit of the same job, e.g. replicas above minAvailable, moves the gauges
	// forward but is not another sample of the job's scheduling latency.
	UpdateE2eSchedulingByJob(jobName, queue, namespace, creationTime)
	if got := e2eLatencySampleCount(t); got != before+1 {
		t.Fatalf("latency samples after second commit of the same job = %d, want %d", got, before+1)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingLastTime.WithLabelValues(jobName, queue, namespace)); got < firstLastTime {
		t.Fatalf("job scheduling last time went back from %v to %v", firstLastTime, got)
	}

	// Another job with the same name in a different namespace is a different job.
	UpdateE2eSchedulingByJob(jobName, queue, "ns2", creationTime)
	t.Cleanup(func() { DeleteJobMetrics(jobName, queue, "ns2") })
	if got := e2eLatencySampleCount(t); got != before+2 {
		t.Fatalf("latency samples after commit of another job = %d, want %d", got, before+2)
	}

	// A job recreated with the same name before the old one's metrics are deleted is a
	// different job.
	UpdateE2eSchedulingByJob(jobName, queue, namespace, creationTime.Add(time.Second))
	if got := e2eLatencySampleCount(t); got != before+3 {
		t.Fatalf("latency samples after commit of recreated job = %d, want %d", got, before+3)
	}

	// Once the job is deleted, its series is gone and a job recreated with the same name is
	// observed again.
	series := testutil.CollectAndCount(e2eJobSchedulingDuration)
	DeleteJobMetrics(jobName, queue, namespace)
	if got := testutil.CollectAndCount(e2eJobSchedulingDuration); got != series-1 {
		t.Fatalf("job scheduling duration series after delete = %d, want %d", got, series-1)
	}
	UpdateE2eSchedulingByJob(jobName, queue, namespace, creationTime.Add(time.Second))
	if got := e2eLatencySampleCount(t); got != before+4 {
		t.Fatalf("latency samples after commit of job recreated after delete = %d, want %d", got, before+4)
	}
}

func TestRegisterEvictionTransaction(t *testing.T) {
	actions := []string{
		evictionActionPreempt,
		evictionActionReclaim,
		evictionActionGangPreempt,
		evictionActionGangReclaim,
	}

	for _, action := range actions {
		action := action
		t.Run(action, func(t *testing.T) {
			counter := evictionTransactions.WithLabelValues(action)
			before := testutil.ToFloat64(counter)

			RegisterEvictionTransaction(action)

			if got := testutil.ToFloat64(counter); got != before+1 {
				t.Fatalf("eviction transaction counter = %v, want %v", got, before+1)
			}
		})
	}

	before := testutil.CollectAndCount(evictionTransactions)
	RegisterEvictionTransaction("unsupported-action")
	if got := testutil.CollectAndCount(evictionTransactions); got != before {
		t.Fatalf("metric count after unsupported action = %d, want %d", got, before)
	}
}
