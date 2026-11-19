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

package podgroup

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestStatPodGroupCountsForQueue(t *testing.T) {
	tests := []struct {
		name     string
		pg       *v1beta1.PodGroup
		initial  *PodGroupStatistics
		expected *PodGroupStatistics
	}{
		{
			name: "Count Inqueue PodGroup",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-inqueue"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupInqueue,
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{Inqueue: 1},
		},
		{
			name: "Count Pending PodGroup",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-pending"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupPending,
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{Pending: 1},
		},
		{
			name: "Count Running PodGroup",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-running"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupRunning,
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{Running: 1},
		},
		{
			name: "Count Unknown PodGroup",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-unknown"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupUnknown,
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{Unknown: 1},
		},
		{
			name: "Count Completed PodGroup",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-completed"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupCompleted,
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{Completed: 1},
		},
		{
			name: "Unrecognized PodGroup Phase does not increment any count",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-other"},
				Status: v1beta1.PodGroupStatus{
					Phase: "OtherPhase",
				},
			},
			initial:  &PodGroupStatistics{},
			expected: &PodGroupStatistics{},
		},
		{
			name:     "Nil PodGroup does not panic or increment count",
			pg:       nil,
			initial:  &PodGroupStatistics{Running: 2},
			expected: &PodGroupStatistics{Running: 2},
		},
		{
			name: "Nil PodGroupStatistics receiver does not panic",
			pg: &v1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "pg-running"},
				Status: v1beta1.PodGroupStatus{
					Phase: v1beta1.PodGroupRunning,
				},
			},
			initial:  nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.StatPodGroupCountsForQueue(tt.pg)
			if !reflect.DeepEqual(tt.initial, tt.expected) {
				t.Errorf("StatPodGroupCountsForQueue() = %+v, want %+v", tt.initial, tt.expected)
			}
		})
	}
}

func TestAccumulateMultiplePodGroups(t *testing.T) {
	pgs := []*v1beta1.PodGroup{
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupInqueue}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupInqueue}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupPending}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupRunning}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupRunning}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupRunning}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupUnknown}},
		{Status: v1beta1.PodGroupStatus{Phase: v1beta1.PodGroupCompleted}},
	}

	stats := &PodGroupStatistics{}
	for _, pg := range pgs {
		stats.StatPodGroupCountsForQueue(pg)
	}

	expected := &PodGroupStatistics{
		Inqueue:   2,
		Pending:   1,
		Running:   3,
		Unknown:   1,
		Completed: 1,
	}

	if !reflect.DeepEqual(stats, expected) {
		t.Errorf("accumulated stats = %+v, want %+v", stats, expected)
	}
}
