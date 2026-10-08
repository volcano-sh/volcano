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

package rescheduling

import (
	"testing"
	"time"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

// A scheduler config that writes interval as a bare YAML number decodes to an
// int, not a string. Before this was fixed the assertion panicked and took the
// scheduler down at config load.
func TestParseArgumentsNonStringInterval(t *testing.T) {
	rc := NewReschedulingConfigs()
	rc.parseArguments(framework.Arguments{"interval": 5})

	if rc.interval != DefaultInterval {
		t.Errorf("interval = %v, want the default %v", rc.interval, DefaultInterval)
	}
}

func TestParseArgumentsNonStringMetricsPeriod(t *testing.T) {
	MetricsPeriod = ""
	rc := NewReschedulingConfigs()
	rc.parseArguments(framework.Arguments{"metricsPeriod": 30})

	if MetricsPeriod != DefaultMetricsPeriod {
		t.Errorf("MetricsPeriod = %q, want the default %q", MetricsPeriod, DefaultMetricsPeriod)
	}
}

func TestParseArgumentsValidValuesStillApply(t *testing.T) {
	MetricsPeriod = ""
	rc := NewReschedulingConfigs()
	rc.parseArguments(framework.Arguments{"interval": "10m", "metricsPeriod": "1m"})

	if rc.interval != 10*time.Minute {
		t.Errorf("interval = %v, want 10m", rc.interval)
	}
	if MetricsPeriod != "1m" {
		t.Errorf("MetricsPeriod = %q, want 1m", MetricsPeriod)
	}
}

// cpu: "20" in the scheduler config decodes to a string, and memory: 20
// decodes to an int. The container assertion already guarded the map itself,
// so only the element types were left unchecked.
func TestLowNodeUtilizationParseMixedThresholdTypes(t *testing.T) {
	conf := &LowNodeUtilizationConf{
		Thresholds:       map[string]float64{},
		TargetThresholds: map[string]float64{},
	}
	conf.parse(map[string]interface{}{
		"thresholds": map[interface{}]interface{}{
			"cpu":    "20",
			"memory": 30,
		},
	})

	if got, ok := conf.Thresholds["memory"]; !ok || got != 30 {
		t.Errorf("Thresholds[memory] = %v (present=%v), want 30", got, ok)
	}
	if _, ok := conf.Thresholds["cpu"]; ok {
		t.Error("Thresholds[cpu] was set from a string value, want it skipped")
	}
}

func TestLowNodeUtilizationParseNonStringKey(t *testing.T) {
	conf := &LowNodeUtilizationConf{
		Thresholds:       map[string]float64{},
		TargetThresholds: map[string]float64{},
	}
	conf.parse(map[string]interface{}{
		"targetThresholds": map[interface{}]interface{}{
			1:     40,
			"cpu": 50,
		},
	})

	if got, ok := conf.TargetThresholds["cpu"]; !ok || got != 50 {
		t.Errorf("TargetThresholds[cpu] = %v (present=%v), want 50", got, ok)
	}
}
