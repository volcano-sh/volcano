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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
)

func TestNewSubJobInfo(t *testing.T) {
	type args struct {
		gid         SubJobGID
		uid         SubJobID
		job         JobID
		policy      *scheduling.SubGroupPolicySpec
		matchValues []string
	}
	tests := []struct {
		name string
		args args
		want *SubJobInfo
	}{
		{
			name: "All field provided",
			args: args{
				gid: "test-gid",
				uid: "test-uid",
				job: "test-job",
				policy: &scheduling.SubGroupPolicySpec{
					SubGroupSize: ptr.To(int32(4)),
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode:               scheduling.HardNetworkTopologyMode,
						HighestTierAllowed: ptr.To(1),
					},
				},
				matchValues: []string{"1"},
			},
			want: &SubJobInfo{
				GID:             "test-gid",
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    4,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				Children:        make(map[SubJobID]*SubJobInfo),
				IsLeaf:          true,
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
			},
		},
		{
			name: "No policy provided",
			args: args{
				gid:         "test-gid",
				uid:         "test-uid",
				job:         "test-job",
				policy:      nil,
				matchValues: []string{"1"},
			},
			want: &SubJobInfo{
				GID:             "test-gid",
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    1,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				Children:        make(map[SubJobID]*SubJobInfo),
				IsLeaf:          true,
				networkTopology: nil,
			},
		},
		{
			name: "No SubGroupSize provided",
			args: args{
				gid: "test-gid",
				uid: "test-uid",
				job: "test-job",
				policy: &scheduling.SubGroupPolicySpec{
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode:               scheduling.HardNetworkTopologyMode,
						HighestTierAllowed: ptr.To(1),
					},
				},
				matchValues: []string{"1"},
			},
			want: &SubJobInfo{
				GID:             "test-gid",
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    1,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				Children:        make(map[SubJobID]*SubJobInfo),
				IsLeaf:          true,
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
			},
		},
		{
			name: "No networkTopology provided",
			args: args{
				gid: "test-gid",
				uid: "test-uid",
				job: "test-job",
				policy: &scheduling.SubGroupPolicySpec{
					SubGroupSize:    ptr.To(int32(2)),
					NetworkTopology: nil,
				},
				matchValues: []string{},
			},
			want: &SubJobInfo{
				GID:             "test-gid",
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    2,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				Children:        make(map[SubJobID]*SubJobInfo),
				IsLeaf:          true,
				networkTopology: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewSubJobInfo(tt.args.gid, tt.args.uid, tt.args.job, tt.args.policy, tt.args.matchValues)
			assert.Equalf(t, tt.want, got, "NewSubJobInfo(%v, %v, %v, %v, %v)", tt.args.gid, tt.args.uid, tt.args.job, tt.args.policy, tt.args.matchValues)
		})
	}
}

func TestSubJobInfo_IsHardTopologyMode(t *testing.T) {
	type fields struct {
		UID               SubJobID
		Job               JobID
		Priority          int32
		MatchIndex        int
		Tasks             map[TaskID]*TaskInfo
		TaskStatusIndex   map[TaskStatus]TasksMap
		taskPriorities    map[int32]sets.Set[TaskID]
		AllocateHyperNode string
		networkTopology   *scheduling.NetworkTopologySpec
	}
	tests := []struct {
		name         string
		fields       fields
		expectedHard bool
		expectedTier int
	}{
		{
			name: "networkTopology is nil",
			fields: fields{
				networkTopology: nil,
			},
			expectedHard: false,
			expectedTier: 0,
		},
		{
			name: "HighestTierAllowed is nil",
			fields: fields{
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode: scheduling.HardNetworkTopologyMode,
				},
			},
			expectedHard: false,
			expectedTier: 0,
		},
		{
			name: "Mode is HardNetworkTopologyMode and HighestTierAllowed is not nil",
			fields: fields{
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
			},
			expectedHard: true,
			expectedTier: 1,
		},
		{
			name: "Mode is not HardNetworkTopologyMode and HighestTierAllowed is valid",
			fields: fields{
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.SoftNetworkTopologyMode,
					HighestTierAllowed: ptr.To(2),
				},
			},
			expectedHard: false,
			expectedTier: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sji := &SubJobInfo{
				UID:                tt.fields.UID,
				Job:                tt.fields.Job,
				Priority:           tt.fields.Priority,
				MatchIndex:         tt.fields.MatchIndex,
				Tasks:              tt.fields.Tasks,
				TaskStatusIndex:    tt.fields.TaskStatusIndex,
				taskPriorities:     tt.fields.taskPriorities,
				AllocatedHyperNode: tt.fields.AllocateHyperNode,
				networkTopology:    tt.fields.networkTopology,
			}
			gotIsHard, gotTier := sji.IsHardTopologyMode()
			assert.Equal(t, tt.expectedHard, gotIsHard, "IsHardTopologyMode()")
			assert.Equal(t, tt.expectedTier, gotTier, "IsHardTopologyMode()")
		})
	}
}

func TestSubJobInfo_IsSoftTopologyMode(t *testing.T) {
	type fields struct {
		UID               SubJobID
		Job               JobID
		Priority          int32
		MatchIndex        int
		Tasks             map[TaskID]*TaskInfo
		TaskStatusIndex   map[TaskStatus]TasksMap
		taskPriorities    map[int32]sets.Set[TaskID]
		AllocateHyperNode string
		networkTopology   *scheduling.NetworkTopologySpec
	}
	tests := []struct {
		name         string
		fields       fields
		expectedHard bool
	}{
		{
			name: "networkTopology is nil",
			fields: fields{
				networkTopology: nil,
			},
			expectedHard: false,
		},
		{
			name: "Mode is HardNetworkTopologyMode",
			fields: fields{
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode: scheduling.HardNetworkTopologyMode,
				},
			},
			expectedHard: false,
		},
		{
			name: "Mode is SoftNetworkTopologyMode",
			fields: fields{
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode: scheduling.SoftNetworkTopologyMode,
				},
			},
			expectedHard: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sji := &SubJobInfo{
				networkTopology: tt.fields.networkTopology,
			}
			assert.Equalf(t, tt.expectedHard, sji.IsSoftTopologyMode(), "IsSoftTopologyMode()")
		})
	}
}

func TestGetSubJobMatchValues_Basic(t *testing.T) {
	tests := []struct {
		name     string
		policy   scheduling.SubGroupPolicySpec
		pod      *v1.Pod
		expected []string
	}{
		{
			name: "match label selector and collect match values",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				MatchLabelKeys: []string{"version", "env"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test", "version": "v1", "env": "dev"},
				},
			},
			expected: []string{"v1", "dev"},
		},
		{
			name: "no label selector with match keys",
			policy: scheduling.SubGroupPolicySpec{
				MatchLabelKeys: []string{"zone"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"zone": "zone-1"},
				},
			},
			expected: []string{"zone-1"},
		},
		{
			name: "no match keys returns empty slice",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubJobMatchValues(tt.policy, tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubJobMatchValues_Boundary(t *testing.T) {
	tests := []struct {
		name     string
		policy   scheduling.SubGroupPolicySpec
		pod      *v1.Pod
		expected []string
	}{
		{
			name: "empty label selector matches all",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector:  &metav1.LabelSelector{},
				MatchLabelKeys: []string{"key"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"key": "value"},
				},
			},
			expected: []string{"value"},
		},
		{
			name: "match expression selector",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"test", "demo"},
						},
					},
				},
				MatchLabelKeys: []string{"app"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "demo"},
				},
			},
			expected: []string{"demo"},
		},
		{
			name: "multiple match keys with empty values",
			policy: scheduling.SubGroupPolicySpec{
				MatchLabelKeys: []string{"a", "b"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"a": "", "b": "val"},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubJobMatchValues(tt.policy, tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetSubJobMatchValues_Error(t *testing.T) {
	tests := []struct {
		name     string
		policy   scheduling.SubGroupPolicySpec
		pod      *v1.Pod
		expected []string
	}{
		{
			name: "invalid label selector",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: "invalid",
						},
					},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
			},
			expected: nil,
		},
		{
			name: "pod labels do not match selector",
			policy: scheduling.SubGroupPolicySpec{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				MatchLabelKeys: []string{"version"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "prod"},
				},
			},
			expected: nil,
		},
		{
			name: "missing label key in pod",
			policy: scheduling.SubGroupPolicySpec{
				MatchLabelKeys: []string{"missing"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"existing": "value"},
				},
			},
			expected: nil,
		},
		{
			name: "pod has no labels",
			policy: scheduling.SubGroupPolicySpec{
				MatchLabelKeys: []string{"any"},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSubJobMatchValues(tt.policy, tt.pod)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubJobInfo_getPodSubJobID(t *testing.T) {
	type args struct {
		job         JobID
		policy      string
		matchValues []string
	}
	tests := []struct {
		name string
		args args
		want SubJobID
	}{
		{
			name: "Normale case with non-empty policy and matchValues",
			args: args{
				job:         JobID("job1"),
				policy:      "policy1",
				matchValues: []string{"value1", "value2"},
			},
			want: SubJobID("job1/policy1-value1-value2"),
		},
		{
			name: "Single matchValue",
			args: args{
				job:         JobID("job1"),
				policy:      "policy1",
				matchValues: []string{"value1"},
			},
			want: SubJobID("job1/policy1-value1"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSubJobID(tt.args.job, tt.args.policy, tt.args.matchValues)
			if got != tt.want {
				t.Errorf("getPodSubJobID(%v, %v, %v) got = %v, want %v", tt.args.job, tt.args.policy, tt.args.matchValues, got, tt.want)
			}
		})
	}
}

func TestSubJobInfo_IsPipelined(t *testing.T) {
	tests := []struct {
		name     string
		sji      *SubJobInfo
		expected bool
	}{
		// Leaf node test cases
		{
			name: "All tasks are pipelined",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 3,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pipelined: {
						"task1": &TaskInfo{},
						"task2": &TaskInfo{},
						"task3": &TaskInfo{},
					},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are ready and some are pipelined",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 6,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
					"task4": {},
					"task5": {},
					"task6": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Allocated: {"task1": &TaskInfo{}},
					Pipelined: {"task2": &TaskInfo{}},
					Binding:   {"task3": &TaskInfo{}},
					Bound:     {"task4": &TaskInfo{}},
					Running:   {"task5": &TaskInfo{}},
					Succeeded: {"task6": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are pending best-effort and some are pipelined",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 3,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: true},
					},
					Pipelined: {"task3": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are ready, some tasks are pending best-effort and some are pipelined",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 8,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
					"task4": {},
					"task5": {},
					"task6": {},
					"task7": {},
					"task8": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: true},
					},
					Allocated: {"task3": &TaskInfo{}},
					Pipelined: {"task4": &TaskInfo{}},
					Binding:   {"task5": &TaskInfo{}},
					Bound:     {"task6": &TaskInfo{}},
					Running:   {"task7": &TaskInfo{}},
					Succeeded: {"task8": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are pending not best-effort",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 8,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
					"task4": {},
					"task5": {},
					"task6": {},
					"task7": {},
					"task8": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: false},
					},
					Allocated: {"task3": &TaskInfo{}},
					Pipelined: {"task4": &TaskInfo{}},
					Binding:   {"task5": &TaskInfo{}},
					Bound:     {"task6": &TaskInfo{}},
					Running:   {"task7": &TaskInfo{}},
					Succeeded: {"task8": &TaskInfo{}},
				},
			},
			expected: false,
		},
		{
			name: "Some tasks are failed",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 8,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
					"task4": {},
					"task5": {},
					"task6": {},
					"task7": {},
					"task8": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: true},
					},
					Allocated: {"task3": &TaskInfo{}},
					Pipelined: {"task4": &TaskInfo{}},
					Binding:   {"task5": &TaskInfo{}},
					Bound:     {"task6": &TaskInfo{}},
					Running:   {"task7": &TaskInfo{}},
					Failed:    {"task8": &TaskInfo{}},
				},
			},
			expected: false,
		},
		{
			name: "Some tasks are unknown",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 3,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: true},
					},
					Unknown: {"task3": &TaskInfo{}},
				},
			},
			expected: false,
		},
		// Multi-level test cases
		{
			name: "Non-leaf: 2 children with pipelined tasks meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 7,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task1": &TaskInfo{}},
							Bound:     {"task2": &TaskInfo{}},
							Running:   {"task3": &TaskInfo{}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task4": &TaskInfo{}, "task5": &TaskInfo{}},
							Allocated: {"task6": &TaskInfo{}},
							Succeeded: {"task7": &TaskInfo{}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-leaf: 2 children with all task types meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 9,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task1": &TaskInfo{}},
							Bound:     {"task2": &TaskInfo{}},
							Pending:   {"task3": &TaskInfo{BestEffort: true}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task4": &TaskInfo{}, "task5": &TaskInfo{}},
							Running:   {"task6": &TaskInfo{}},
							Succeeded: {"task7": &TaskInfo{}},
							Pending:   {"task8": &TaskInfo{BestEffort: true}, "task9": &TaskInfo{BestEffort: true}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-leaf: 2 children with non-best-effort pending do not meet",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 6,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task1": &TaskInfo{}},
							Bound:     {"task2": &TaskInfo{}},
							Pending:   {"task3": &TaskInfo{BestEffort: false}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Pipelined: {"task4": &TaskInfo{}},
							Running:   {"task5": &TaskInfo{}},
							Failed:    {"task6": &TaskInfo{}},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Three-level: Deep nesting with pipelined tasks meets MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 9,
				Children: map[SubJobID]*SubJobInfo{
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition1": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task1": &TaskInfo{}, "task2": &TaskInfo{}},
									Bound:     {"task3": &TaskInfo{}},
								},
							},
							"partition2": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task4": &TaskInfo{}},
									Running:   {"task5": &TaskInfo{}},
								},
							},
						},
					},
					"group2": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition3": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task6": &TaskInfo{}, "task7": &TaskInfo{}},
									Succeeded: {"task8": &TaskInfo{}},
									Running:   {"task9": &TaskInfo{}},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Three-level: Deep nesting with all task types meets MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 12,
				Children: map[SubJobID]*SubJobInfo{
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition1": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task1": &TaskInfo{}, "task2": &TaskInfo{}},
									Bound:     {"task3": &TaskInfo{}},
									Pending:   {"task4": &TaskInfo{BestEffort: true}},
								},
							},
							"partition2": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task5": &TaskInfo{}},
									Running:   {"task6": &TaskInfo{}},
									Pending:   {"task7": &TaskInfo{BestEffort: true}},
								},
							},
						},
					},
					"group2": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition3": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Pipelined: {"task8": &TaskInfo{}, "task9": &TaskInfo{}},
									Succeeded: {"task10": &TaskInfo{}},
									Running:   {"task11": &TaskInfo{}},
									Pending:   {"task12": &TaskInfo{BestEffort: true}},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Three-level: Deep nesting does not meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 10,
				Children: map[SubJobID]*SubJobInfo{
					"level1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"level2": {
								IsLeaf: false,
								Children: map[SubJobID]*SubJobInfo{
									"level3": {
										IsLeaf: true,
										TaskStatusIndex: map[TaskStatus]TasksMap{
											Pipelined: {"task1": &TaskInfo{}},
											Bound:     {"task2": &TaskInfo{}},
											Running:   {"task3": &TaskInfo{}},
											Pending:   {"task4": &TaskInfo{BestEffort: false}},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sji.IsPipelined()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubJobInfo_IsReady(t *testing.T) {
	tests := []struct {
		name     string
		sji      *SubJobInfo
		expected bool
	}{
		// Leaf node test cases
		{
			name: "Leaf: All tasks are pending (not best-effort)",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 3,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{},
						"task2": &TaskInfo{},
						"task3": &TaskInfo{},
					},
				},
			},
			expected: false,
		},
		{
			name: "Leaf: All tasks are pending best-effort",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 2,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: true},
					},
				},
			},
			expected: true,
		},
		{
			name: "Leaf: Ready tasks meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 5,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Allocated: {"task1": &TaskInfo{}},
					Binding:   {"task2": &TaskInfo{}},
					Bound:     {"task3": &TaskInfo{}},
					Running:   {"task4": &TaskInfo{}},
					Succeeded: {"task5": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Leaf: Ready + best-effort tasks meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 6,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending:   {"task1": &TaskInfo{BestEffort: true}},
					Allocated: {"task2": &TaskInfo{}},
					Binding:   {"task3": &TaskInfo{}},
					Bound:     {"task4": &TaskInfo{}},
					Running:   {"task5": &TaskInfo{}},
					Succeeded: {"task6": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Leaf: Ready + best-effort tasks do not meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 7,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {
						"task1": &TaskInfo{BestEffort: true},
						"task2": &TaskInfo{BestEffort: false},
					},
					Allocated: {"task3": &TaskInfo{}},
					Binding:   {"task4": &TaskInfo{}},
					Bound:     {"task5": &TaskInfo{}},
					Running:   {"task6": &TaskInfo{}},
					Succeeded: {"task7": &TaskInfo{}},
				},
			},
			expected: false,
		},
		{
			name: "Leaf: Some tasks failed, not ready",
			sji: &SubJobInfo{
				IsLeaf:       true,
				MinAvailable: 1,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Failed: {"task1": &TaskInfo{}},
				},
			},
			expected: false,
		},

		// Multi-level test cases
		{
			name: "Non-leaf: 2 children meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 5,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Bound:   {"task1": &TaskInfo{}},
							Running: {"task2": &TaskInfo{}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Allocated: {"task3": &TaskInfo{}},
							Binding:   {"task4": &TaskInfo{}},
							Succeeded: {"task5": &TaskInfo{}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-leaf: 2 children with best-effort tasks meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 6,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Bound:   {"task1": &TaskInfo{}},
							Running: {"task2": &TaskInfo{}},
							Pending: {"task3": &TaskInfo{BestEffort: true}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Allocated: {"task4": &TaskInfo{}},
							Succeeded: {"task5": &TaskInfo{}},
							Pending:   {"task6": &TaskInfo{BestEffort: true}},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-leaf: 2 children do not meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 6,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Bound:   {"task1": &TaskInfo{}},
							Pending: {"task2": &TaskInfo{BestEffort: false}},
						},
					},
					"child2": {
						IsLeaf: true,
						TaskStatusIndex: map[TaskStatus]TasksMap{
							Running: {"task3": &TaskInfo{}},
							Failed:  {"task4": &TaskInfo{}},
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "Three-level: Deep nesting meets MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 6,
				Children: map[SubJobID]*SubJobInfo{
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition1": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Bound:   {"task1": &TaskInfo{}},
									Running: {"task2": &TaskInfo{}},
								},
							},
							"partition2": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Allocated: {"task3": &TaskInfo{}},
									Binding:   {"task4": &TaskInfo{}},
								},
							},
						},
					},
					"group2": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition3": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Succeeded: {"task5": &TaskInfo{}},
									Running:   {"task6": &TaskInfo{}},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Three-level: Deep nesting with best-effort tasks",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 8,
				Children: map[SubJobID]*SubJobInfo{
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition1": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Bound:   {"task1": &TaskInfo{}},
									Running: {"task2": &TaskInfo{}},
									Pending: {"task3": &TaskInfo{BestEffort: true}},
								},
							},
							"partition2": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Allocated: {"task4": &TaskInfo{}},
									Binding:   {"task5": &TaskInfo{}},
								},
							},
						},
					},
					"group2": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition3": {
								IsLeaf: true,
								TaskStatusIndex: map[TaskStatus]TasksMap{
									Succeeded: {"task6": &TaskInfo{}},
									Running:   {"task7": &TaskInfo{}},
									Pending:   {"task8": &TaskInfo{BestEffort: true}},
								},
							},
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Three-level: Deep nesting does not meet MinAvailable",
			sji: &SubJobInfo{
				IsLeaf:       false,
				MinAvailable: 10,
				Children: map[SubJobID]*SubJobInfo{
					"level1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"level2": {
								IsLeaf: false,
								Children: map[SubJobID]*SubJobInfo{
									"level3": {
										IsLeaf: true,
										TaskStatusIndex: map[TaskStatus]TasksMap{
											Bound:     {"task1": &TaskInfo{}},
											Running:   {"task2": &TaskInfo{}},
											Allocated: {"task3": &TaskInfo{}},
											Pending:   {"task4": &TaskInfo{BestEffort: false}},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sji.IsReady()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSubJobInfo_getTaskHighestPriority(t *testing.T) {
	// Test case 1: When taskPriorities is empty, it should return 0
	sji := &SubJobInfo{
		taskPriorities: map[int32]sets.Set[TaskID]{},
	}
	assert.Equal(t, int32(0), sji.getTaskHighestPriority(), "Expected 0 when taskPriorities is empty")

	// Test case 2: When taskPriorities has only one element, it should return that element
	sji = &SubJobInfo{
		taskPriorities: map[int32]sets.Set[TaskID]{
			1: sets.New(TaskID("task1")),
		},
	}
	assert.Equal(t, int32(1), sji.getTaskHighestPriority(), "Expected 1 when taskPriorities has one element")

	// Test case 3: When taskPriorities has multiple elements, it should return the highest element
	sji = &SubJobInfo{
		taskPriorities: map[int32]sets.Set[TaskID]{
			1: sets.New(TaskID("task1")),
			2: sets.New(TaskID("task2")),
			3: sets.New(TaskID("task3")),
		},
	}
	assert.Equal(t, int32(3), sji.getTaskHighestPriority(), "Expected 3 when taskPriorities has multiple elements")

	// Test case 4: When taskPriorities has multiple elements and the highest element is negative, it should return the highest element
	sji = &SubJobInfo{
		taskPriorities: map[int32]sets.Set[TaskID]{
			-1: sets.New(TaskID("task1")),
			-2: sets.New(TaskID("task2")),
			-3: sets.New(TaskID("task3")),
		},
	}
	assert.Equal(t, int32(0), sji.getTaskHighestPriority(), "Expected -1 when taskPriorities has multiple negative elements")
}

func TestSubJobInfo_LeafSubJobCount(t *testing.T) {
	tests := []struct {
		name     string
		subJob   *SubJobInfo
		expected int32
	}{
		{
			name: "Leaf node returns 1",
			subJob: &SubJobInfo{
				IsLeaf:   true,
				Children: map[SubJobID]*SubJobInfo{},
			},
			expected: 1,
		},
		{
			name: "Non-leaf node with 2 leaf children",
			subJob: &SubJobInfo{
				IsLeaf: false,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
					"child2": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
				},
			},
			expected: 2,
		},
		{
			name: "Non-leaf node with 3 leaf children",
			subJob: &SubJobInfo{
				IsLeaf: false,
				Children: map[SubJobID]*SubJobInfo{
					"child1": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
					"child2": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
					"child3": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
				},
			},
			expected: 3,
		},
		{
			name: "Multi-level structure: 2 groups, each with 2 partitions",
			subJob: &SubJobInfo{
				IsLeaf: false,
				Children: map[SubJobID]*SubJobInfo{
					"group0": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition0": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
							"partition1": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
						},
					},
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition2": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
							"partition3": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
						},
					},
				},
			},
			expected: 4,
		},
		{
			name: "Multi-level structure: 2 groups with different partition counts",
			subJob: &SubJobInfo{
				IsLeaf: false,
				Children: map[SubJobID]*SubJobInfo{
					"group0": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition0": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
						},
					},
					"group1": {
						IsLeaf: false,
						Children: map[SubJobID]*SubJobInfo{
							"partition1": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
							"partition2": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
							"partition3": {IsLeaf: true, Children: map[SubJobID]*SubJobInfo{}},
						},
					},
				},
			},
			expected: 4,
		},
		{
			name: "Empty non-leaf node returns 0",
			subJob: &SubJobInfo{
				IsLeaf:   false,
				Children: map[SubJobID]*SubJobInfo{},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.subJob.LeafSubJobCount()
			assert.Equal(t, tt.expected, result)
		})
	}
}
