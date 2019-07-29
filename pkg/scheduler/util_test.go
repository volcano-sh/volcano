/*
Copyright 2019 The Kubernetes Authors.

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

package scheduler

import (
	"reflect"
	"strings"
	"testing"

	_ "volcano.sh/volcano/pkg/scheduler/actions"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestLoadSchedulerConf(t *testing.T) {
	configuration := `
actions: "allocate, backfill,reclaim"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
`

	configuration2 := `
version: v2
actions:
- name: enqueue
  arguments:
      idleres-mul: 1.2
- name: allocate
- name: backfill
- name: reclaim
- name: preempt
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
`

	trueValue := true
	expectedTiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                "priority",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
				{
					Name:                "gang",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
				{
					Name:                "conformance",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
			},
		},
		{
			Plugins: []conf.PluginOption{
				{
					Name:                "drf",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
				{
					Name:                "predicates",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
				{
					Name:                "proportion",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
				{
					Name:                "nodeorder",
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledReclaimable:  &trueValue,
					EnabledQueueOrder:   &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledNodeOrder:    &trueValue,
				},
			},
		},
	}

	expectedActs := "allocate, backfill,reclaim"

	expectedActOpts2 := []conf.ActionOption{
		{
			Name:      "enqueue",
			Arguments: map[string]string{"idleres-mul": "1.2"},
		},
		{
			Name: "allocate",
		},
		{
			Name: "backfill",
		},
		{
			Name: "reclaim",
		},
		{
			Name: "preempt",
		},
	}

	cases := []string{
		configuration, configuration2}

	for i := range cases {

		schedStConf, err := loadSchedulerConf(cases[i])
		if err != nil {
			t.Errorf("Failed to load scheduler configuration: %v", err)
		}
		if schedStConf.Version == framework.SchedulerConfigVersion1 {
			if !reflect.DeepEqual(schedStConf.V1Conf.Tiers, expectedTiers) {
				t.Errorf("Failed to set default settings for plugins, expected: %+v, got %+v",
					expectedTiers, schedStConf.V1Conf.Tiers)
			}
			//validate actions
			if strings.Compare(schedStConf.V1Conf.Actions, expectedActs) != 0 {
				t.Errorf("Failed to set default settings for action args, expected: %+v, got %+v",
					expectedActs, schedStConf.V1Conf.Actions)
			}
		} else {
			if !reflect.DeepEqual(schedStConf.V2Conf.Tiers, expectedTiers) {
				t.Errorf("Failed to set default settings for plugins, expected: %+v, got %+v",
					expectedTiers, schedStConf.V2Conf.Tiers)
			}
			//validate actions
			if !reflect.DeepEqual(schedStConf.V2Conf.Actions, expectedActOpts2) {
				t.Errorf("Failed to set default settings for action args, expected: %+v, got %+v",
					expectedActOpts2, schedStConf.V2Conf.Actions)
			}
		}
	}
}
