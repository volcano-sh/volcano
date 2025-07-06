package util

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestNormalizeScore(t *testing.T) {
	tests := []struct {
		name     string
		max      int64
		reverse  bool
		scores   []api.ScoredNode
		expected []api.ScoredNode
	}{
		{
			name:    "basic normalization",
			max:     100,
			reverse: false,
			scores: []api.ScoredNode{
				{Score: 10},
				{Score: 20},
			},
			expected: []api.ScoredNode{
				{Score: 50},
				{Score: 100},
			},
		},
		{
			name:    "reverse normalization",
			max:     10,
			reverse: true,
			scores: []api.ScoredNode{
				{Score: 2},
				{Score: 5},
			},
			expected: []api.ScoredNode{
				{Score: 6},
				{Score: 0},
			},
		},
		{
			name:    "zero scores with reverse",
			max:     100,
			reverse: true,
			scores: []api.ScoredNode{
				{Score: 0},
			},
			expected: []api.ScoredNode{
				{Score: 100},
			},
		},
		{
			name:    "single node",
			max:     100,
			reverse: false,
			scores: []api.ScoredNode{
				{Score: 30},
			},
			expected: []api.ScoredNode{
				{Score: 100},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := make([]api.ScoredNode, len(tt.scores))
			copy(in, tt.scores)
			NormalizeScore(tt.max, tt.reverse, in)
			if !reflect.DeepEqual(in, tt.expected) {
				t.Errorf("got %v, want %v", in, tt.expected)
			}
		})
	}
}

func TestCalculateAllocatedTaskNum(t *testing.T) {
	taskAllocated1 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t1"}})
	taskAllocated1.Status = api.Allocated
	taskAllocated2 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t2"}})
	taskAllocated2.Status = api.Allocated
	taskRunning1 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t3"}})
	taskRunning1.Status = api.Running
	taskRunning2 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t4"}})
	taskRunning2.Status = api.Running
	taskPending1 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t5"}})
	taskPending1.Status = api.Pending
	taskPending2 := api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "t6"}})
	taskPending2.Status = api.Pending

	tests := []struct {
		name     string
		job      *api.JobInfo
		expected int
	}{
		{
			name:     "allocated+running",
			job:      api.NewJobInfo("job1", taskAllocated1, taskAllocated2, taskRunning1, taskRunning2, taskPending1, taskPending2),
			expected: 4,
		},
		{
			name:     "only pending",
			job:      api.NewJobInfo("job2", taskPending1, taskPending2),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateAllocatedTaskNum(tt.job)
			if got != tt.expected {
				t.Errorf("got %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestConstants(t *testing.T) {
	if Permit != 1 || Abstain != 0 || Reject != -1 {
		t.Errorf("Permit=%d, Abstain=%d, Reject=%d; want 1,0,-1",
			Permit, Abstain, Reject)
	}
}
