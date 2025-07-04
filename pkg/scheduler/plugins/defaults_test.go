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

package plugins

import (
	"testing"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestApplyPluginConfDefaults(t *testing.T) {
	type args struct {
		option *conf.PluginOption
	}
	falseValue := false
	tests := []struct {
		name     string
		args     args
		expected bool
	}{
		{
			name: "test with nil fields - should set all to true",
			args: args{
				option: &conf.PluginOption{
					Name:                 "test",
					EnabledJobOrder:      nil,
					EnabledHierarchy:     nil,
					EnabledJobReady:      nil,
					EnabledJobPipelined:  nil,
					EnabledTaskOrder:     nil,
					EnabledPreemptable:   nil,
					EnabledReclaimable:   nil,
					EnablePreemptive:     nil,
					EnabledQueueOrder:    nil,
					EnabledClusterOrder:  nil,
					EnabledPredicate:     nil,
					EnabledBestNode:      nil,
					EnabledNodeOrder:     nil,
					EnabledTargetJob:     nil,
					EnabledReservedNodes: nil,
					EnabledJobEnqueued:   nil,
					EnabledVictim:        nil,
					EnabledJobStarving:   nil,
					EnabledOverused:      nil,
					EnabledAllocatable:   nil,
					Arguments:            nil,
				},
			},
			expected: true,
		},
		{
			name: "test with false fields - should keep false values",
			args: args{
				option: &conf.PluginOption{
					Name:                 "test",
					EnabledJobOrder:      &falseValue,
					EnabledHierarchy:     &falseValue,
					EnabledJobReady:      &falseValue,
					EnabledJobPipelined:  &falseValue,
					EnabledTaskOrder:     &falseValue,
					EnabledPreemptable:   &falseValue,
					EnabledReclaimable:   &falseValue,
					EnablePreemptive:     &falseValue,
					EnabledQueueOrder:    &falseValue,
					EnabledClusterOrder:  &falseValue,
					EnabledPredicate:     &falseValue,
					EnabledBestNode:      &falseValue,
					EnabledNodeOrder:     &falseValue,
					EnabledTargetJob:     &falseValue,
					EnabledReservedNodes: &falseValue,
					EnabledJobEnqueued:   &falseValue,
					EnabledVictim:        &falseValue,
					EnabledJobStarving:   &falseValue,
					EnabledOverused:      &falseValue,
					EnabledAllocatable:   &falseValue,
					Arguments:            nil,
				},
			},
			expected: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ApplyPluginConfDefaults(tt.args.option)
			fields := map[string]*bool{
				"EnabledJobOrder":      tt.args.option.EnabledJobOrder,
				"EnabledJobReady":      tt.args.option.EnabledJobReady,
				"EnabledJobPipelined":  tt.args.option.EnabledJobPipelined,
				"EnabledTaskOrder":     tt.args.option.EnabledTaskOrder,
				"EnabledPreemptable":   tt.args.option.EnabledPreemptable,
				"EnabledReclaimable":   tt.args.option.EnabledReclaimable,
				"EnablePreemptive":     tt.args.option.EnablePreemptive,
				"EnabledQueueOrder":    tt.args.option.EnabledQueueOrder,
				"EnabledPredicate":     tt.args.option.EnabledPredicate,
				"EnabledBestNode":      tt.args.option.EnabledBestNode,
				"EnabledNodeOrder":     tt.args.option.EnabledNodeOrder,
				"EnabledTargetJob":     tt.args.option.EnabledTargetJob,
				"EnabledReservedNodes": tt.args.option.EnabledReservedNodes,
				"EnabledVictim":        tt.args.option.EnabledVictim,
				"EnabledJobStarving":   tt.args.option.EnabledJobStarving,
				"EnabledOverused":      tt.args.option.EnabledOverused,
				"EnabledAllocatable":   tt.args.option.EnabledAllocatable,
				"EnabledJobEnqueued":   tt.args.option.EnabledJobEnqueued,
			}
			for name, field := range fields {
				if field == nil {
					t.Errorf("Field %s is nil", name)
				} else if *field != tt.expected {
					t.Errorf("Field %s has value %v, expected %v", name, *field, tt.expected)
				}
			}
		})
	}
}
