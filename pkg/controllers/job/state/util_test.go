/*
Copyright 2019 The Volcano Authors.

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

package state

import (
	"testing"

	vcbatch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestTotalTasks(t *testing.T) {
	testcases := []struct {
		name     string
		tasks    []vcbatch.TaskSpec
		expected int32
	}{
		{
			name:     "No tasks returns 0",
			tasks:    []vcbatch.TaskSpec{},
			expected: 0,
		},
		{
			name:     "Single task with 3 replicas returns 3",
			tasks:    []vcbatch.TaskSpec{{Name: "worker", Replicas: 3}},
			expected: 3,
		},
		{
			name: "Multiple tasks sum all replicas",
			tasks: []vcbatch.TaskSpec{
				{Name: "master", Replicas: 1},
				{Name: "worker", Replicas: 4},
				{Name: "ps", Replicas: 2},
			},
			expected: 7,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			job := &vcbatch.Job{Spec: vcbatch.JobSpec{Tasks: tc.tasks}}
			got := TotalTasks(job)
			if got != tc.expected {
				t.Errorf("TotalTasks() = %d, want %d", got, tc.expected)
			}
		})
	}
}

func TestTotalTaskMinAvailable(t *testing.T) {
	minAvail := func(n int32) *int32 { return &n }

	testcases := []struct {
		name     string
		tasks    []vcbatch.TaskSpec
		expected int32
	}{
		{
			name:     "No tasks returns 0",
			tasks:    []vcbatch.TaskSpec{},
			expected: 0,
		},
		{
			name: "All tasks have explicit MinAvailable",
			tasks: []vcbatch.TaskSpec{
				{Name: "master", Replicas: 3, MinAvailable: minAvail(1)},
				{Name: "worker", Replicas: 5, MinAvailable: minAvail(3)},
			},
			expected: 4,
		},
		{
			name: "Tasks with nil MinAvailable fall back to Replicas",
			tasks: []vcbatch.TaskSpec{
				{Name: "master", Replicas: 2, MinAvailable: nil},
				{Name: "worker", Replicas: 4, MinAvailable: minAvail(2)},
			},
			expected: 4,
		},
		{
			name: "No tasks have MinAvailable set, all use Replicas",
			tasks: []vcbatch.TaskSpec{
				{Name: "master", Replicas: 1},
				{Name: "worker", Replicas: 3},
			},
			expected: 4,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			job := &vcbatch.Job{Spec: vcbatch.JobSpec{Tasks: tc.tasks}}
			got := TotalTaskMinAvailable(job)
			if got != tc.expected {
				t.Errorf("TotalTaskMinAvailable() = %d, want %d", got, tc.expected)
			}
		})
	}
}
