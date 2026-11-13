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

package probes

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configapi "volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/metriccollect"

	"volcano.sh/volcano/pkg/agent/events/framework"
)

type probeRegistration struct {
	eventName string
	probeName string
}

type fakeProbe struct {
	name string
}

func (p *fakeProbe) ProbeName() string {
	return p.name
}

func (p *fakeProbe) Run(stop <-chan struct{}) {}

func (p *fakeProbe) RefreshCfg(cfg *configapi.ColocationConfig) error {
	return nil
}

func resetProbeFuncs() {
	mutex.Lock()
	defer mutex.Unlock()
	probeFuncs = map[string][]NewEventProbeFunc{}
}

func newProbeFunc(name string) NewEventProbeFunc {
	return func(_ *config.Configuration, _ *metriccollect.MetricCollectorManager, _ *framework.EventQueueFactory) framework.Probe {
		return &fakeProbe{name: name}
	}
}

func TestRegisterEventProbeFunc(t *testing.T) {
	tests := []struct {
		name          string
		registrations []probeRegistration
		want          map[string][]string
	}{
		{
			name: "single probe",
			registrations: []probeRegistration{
				{eventName: "cpu", probeName: "cpu-probe"},
			},
			want: map[string][]string{
				"cpu": {"cpu-probe"},
			},
		},
		{
			name: "multiple probes for one event keep registration order",
			registrations: []probeRegistration{
				{eventName: "cpu", probeName: "cpu-probe-a"},
				{eventName: "cpu", probeName: "cpu-probe-b"},
				{eventName: "cpu", probeName: "cpu-probe-c"},
			},
			want: map[string][]string{
				"cpu": {"cpu-probe-a", "cpu-probe-b", "cpu-probe-c"},
			},
		},
		{
			name: "probes for different events are independent",
			registrations: []probeRegistration{
				{eventName: "cpu", probeName: "cpu-probe-a"},
				{eventName: "memory", probeName: "memory-probe-a"},
				{eventName: "cpu", probeName: "cpu-probe-b"},
			},
			want: map[string][]string{
				"cpu":    {"cpu-probe-a", "cpu-probe-b"},
				"memory": {"memory-probe-a"},
			},
		},
		{
			name: "no registrations leaves the registry empty",
			want: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetProbeFuncs()

			for _, registration := range tt.registrations {
				RegisterEventProbeFunc(registration.eventName, newProbeFunc(registration.probeName))
			}

			got := GetEventProbeFuncs()
			require.Len(t, got, len(tt.want))
			for eventName, wantProbes := range tt.want {
				probeFuncs, ok := got[eventName]
				require.True(t, ok, "event %q is not registered", eventName)
				require.Len(t, probeFuncs, len(wantProbes))
				for i, wantProbe := range wantProbes {
					probe := probeFuncs[i](nil, nil, nil)
					require.NotNil(t, probe)
					assert.Equal(t, wantProbe, probe.ProbeName())
				}
			}
		})
	}
}

func TestRegisterEventProbeFuncConcurrent(t *testing.T) {
	resetProbeFuncs()

	const (
		eventCount      = 5
		probesPerEvent  = 10
		totalProbeFuncs = eventCount * probesPerEvent
	)

	expected := make(map[string]map[string]bool, eventCount)
	for i := 0; i < eventCount; i++ {
		expected[fmt.Sprintf("event-%d", i)] = map[string]bool{}
	}

	var wg sync.WaitGroup
	for i := 0; i < totalProbeFuncs; i++ {
		eventName := fmt.Sprintf("event-%d", i%eventCount)
		probeName := fmt.Sprintf("probe-%d", i)
		expected[eventName][probeName] = true
		wg.Add(1)
		go func(eventName, probeName string) {
			defer wg.Done()
			RegisterEventProbeFunc(eventName, newProbeFunc(probeName))
		}(eventName, probeName)
	}
	wg.Wait()

	got := GetEventProbeFuncs()
	require.Len(t, got, eventCount)
	for eventName, wantProbes := range expected {
		probeFuncs, ok := got[eventName]
		require.True(t, ok, "event %q is not registered", eventName)
		require.Len(t, probeFuncs, len(wantProbes))

		gotProbes := make(map[string]bool, len(probeFuncs))
		for _, probeFunc := range probeFuncs {
			probe := probeFunc(nil, nil, nil)
			require.NotNil(t, probe)
			gotProbes[probe.ProbeName()] = true
		}
		assert.Equal(t, wantProbes, gotProbes)
	}
}
