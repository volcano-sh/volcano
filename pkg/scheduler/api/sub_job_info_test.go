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
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    4,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
			},
		},
		{
			name: "No policy provided",
			args: args{
				uid:         "test-uid",
				job:         "test-job",
				policy:      nil,
				matchValues: []string{"1"},
			},
			want: &SubJobInfo{
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    1,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				networkTopology: nil,
			},
		},
		{
			name: "No SubGroupSize provided",
			args: args{
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
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    1,
				MatchIndex:      1,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				networkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: ptr.To(1),
				},
			},
		},
		{
			name: "No networkTopology provided",
			args: args{
				uid: "test-uid",
				job: "test-job",
				policy: &scheduling.SubGroupPolicySpec{
					SubGroupSize:    ptr.To(int32(2)),
					NetworkTopology: nil,
				},
				matchValues: []string{},
			},
			want: &SubJobInfo{
				UID:             "test-uid",
				Job:             "test-job",
				MinAvailable:    2,
				Tasks:           make(map[TaskID]*TaskInfo),
				TaskStatusIndex: make(map[TaskStatus]TasksMap),
				taskPriorities:  make(map[int32]sets.Set[TaskID]),
				networkTopology: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewSubJobInfo(tt.args.uid, tt.args.job, tt.args.policy, tt.args.matchValues)
			assert.Equalf(t, tt.want, got, "NewSubJobInfo(%v, %v, %v, %v)", tt.args.uid, tt.args.job, tt.args.policy, tt.args.matchValues)
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

func TestSubJobInfo_getSubJobMatchValues(t *testing.T) {
	type args struct {
		policy scheduling.SubGroupPolicySpec
		pod    *v1.Pod
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "Normale case with matching labels",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{
						{
							LabelKey: "key1",
						},
						{
							LabelKey: "key2",
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					},
				},
			},
			want: []string{"value1", "value2"},
		},
		{
			name: "Policy MatchPolicy is empty",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"key1": "value1",
							"key2": "value2",
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "Pod Labels is nil",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{
						{
							LabelKey: "key1",
						},
						{
							LabelKey: "key2",
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: nil,
					},
				},
			},
			want: nil,
		},
		{
			name: "Pod Labels does not contain all required keys",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{
						{
							LabelKey: "key1",
						},
						{
							LabelKey: "key2",
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"key1": "value1",
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "Pod Labels contains empty value for required key",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{
						{
							LabelKey: "key1",
						},
						{
							LabelKey: "key2",
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"key1": "value1",
							"key2": "",
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "Pod Labels contains all required keys with non-empty values",
			args: args{
				policy: scheduling.SubGroupPolicySpec{
					MatchPolicy: []scheduling.MatchPolicySpec{
						{
							LabelKey: "key1",
						},
						{
							LabelKey: "key2",
						},
					},
				},
				pod: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"key1": "value1",
							"key2": "value2",
							"key3": "value3",
						},
					},
				},
			},
			want: []string{"value1", "value2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, getSubJobMatchValues(tt.args.policy, tt.args.pod), "getSubJobMatchValues(%v, %v)", tt.args.policy, tt.args.pod)
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
		{
			name: "All tasks are pipelined",
			sji: &SubJobInfo{
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
					Pipelined: {"task3": &TaskInfo{}},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are ready, some tasks are pending best-effort and some are pipelined",
			sji: &SubJobInfo{
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
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: true},
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
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: false},
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
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: true},
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
		{
			name: "All tasks are pipelined",
			sji: &SubJobInfo{
				MinAvailable: 3,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {"task1": &TaskInfo{},
						"task2": &TaskInfo{},
						"task3": &TaskInfo{},
					},
				},
			},
			expected: false,
		},
		{
			name: "All tasks are pending bestEffort",
			sji: &SubJobInfo{
				MinAvailable: 2,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: true},
					},
				},
			},
			expected: true,
		},
		{
			name: "Some tasks are pending best-effort and some are waiting",
			sji: &SubJobInfo{
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
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
					},
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
			name: "Some tasks are failed",
			sji: &SubJobInfo{
				MinAvailable: 1,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Failed: {"task1": &TaskInfo{}},
				},
			},
			expected: false,
		},
		{
			name: "Some tasks are pending not best-effort",
			sji: &SubJobInfo{
				MinAvailable: 7,
				Tasks: map[TaskID]*TaskInfo{
					"task1": {},
					"task2": {},
					"task3": {},
					"task4": {},
					"task5": {},
					"task6": {},
					"task7": {},
				},
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Pending: {"task1": &TaskInfo{
						BestEffort: true},
						"task2": &TaskInfo{
							BestEffort: false},
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
