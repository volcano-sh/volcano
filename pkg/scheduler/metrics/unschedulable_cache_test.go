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

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestUnschedulableJobCacheMetrics(t *testing.T) {
	tests := []struct {
		name     string
		register func()
		value    func() float64
	}{
		{
			name: "records scheduling stage skip",
			register: func() {
				RegisterUnschedulableJobCacheSkip("namespace", "job", "allocate")
			},
			value: func() float64 {
				return testutil.ToFloat64(unschedulableJobCacheSkips.WithLabelValues("namespace", "job", "allocate"))
			},
		},
		{
			name: "records event wakeup",
			register: func() {
				RegisterUnschedulableJobCacheWakeup("namespace", "job", "Node", "UpdateNodeLabel")
			},
			value: func() float64 {
				return testutil.ToFloat64(unschedulableJobCacheWakeups.WithLabelValues("namespace", "job", "Node", "UpdateNodeLabel"))
			},
		},
		{
			name: "records watchdog expiration",
			register: func() {
				RegisterUnschedulableJobCacheWatchdogExpiration("namespace", "job")
			},
			value: func() float64 {
				return testutil.ToFloat64(unschedulableJobCacheWatchdogExpirations.WithLabelValues("namespace", "job"))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetUnschedulableJobCacheDebugMetricsEnabled(false)
			t.Cleanup(func() { SetUnschedulableJobCacheDebugMetricsEnabled(false) })

			before := test.value()
			test.register()
			if got := test.value(); got != before {
				t.Fatalf("disabled metric value = %v, want %v", got, before)
			}

			SetUnschedulableJobCacheDebugMetricsEnabled(true)
			before = test.value()
			test.register()
			if got := test.value(); got != before+1 {
				t.Fatalf("metric value = %v, want %v", got, before+1)
			}
		})
	}
}
