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
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestStatPodGroupCountsForQueue(t *testing.T) {
	tests := []struct {
		name    string
		initial PodGroupStatistics
		phase   v1beta1.PodGroupPhase
		want    PodGroupStatistics
	}{
		{
			name:  "inqueue podgroup increments inqueue count",
			phase: v1beta1.PodGroupInqueue,
			want:  PodGroupStatistics{Inqueue: 1},
		},
		{
			name:  "pending podgroup increments pending count",
			phase: v1beta1.PodGroupPending,
			want:  PodGroupStatistics{Pending: 1},
		},
		{
			name:  "running podgroup increments running count",
			phase: v1beta1.PodGroupRunning,
			want:  PodGroupStatistics{Running: 1},
		},
		{
			name:  "unknown podgroup increments unknown count",
			phase: v1beta1.PodGroupUnknown,
			want:  PodGroupStatistics{Unknown: 1},
		},
		{
			name:  "completed podgroup increments completed count",
			phase: v1beta1.PodGroupCompleted,
			want:  PodGroupStatistics{Completed: 1},
		},
		{
			name:    "inqueue podgroup adds to existing counts",
			initial: PodGroupStatistics{Inqueue: 2, Pending: 3, Running: 4, Unknown: 5, Completed: 6},
			phase:   v1beta1.PodGroupInqueue,
			want:    PodGroupStatistics{Inqueue: 3, Pending: 3, Running: 4, Unknown: 5, Completed: 6},
		},
		{
			name:    "unrecognized phase leaves all counts unchanged",
			initial: PodGroupStatistics{Inqueue: 1, Pending: 2, Running: 3, Unknown: 4, Completed: 5},
			phase:   v1beta1.PodGroupPhase("Unrecognized"),
			want:    PodGroupStatistics{Inqueue: 1, Pending: 2, Running: 3, Unknown: 4, Completed: 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := tt.initial
			pg := &v1beta1.PodGroup{
				Status: v1beta1.PodGroupStatus{Phase: tt.phase},
			}

			stats.StatPodGroupCountsForQueue(pg)

			assert.Equal(t, tt.want, stats)
		})
	}
}
