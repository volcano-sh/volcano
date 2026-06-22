/*
Copyright 2024 The Volcano Authors.

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

func int32Ptr(i int32) *int32 { return &i }

func TestTotalTasks(t *testing.T) {
	tests := []struct {
		name string
		job  *vcbatch.Job
		want int32
	}{
		{
			name: "no tasks",
			job:  &vcbatch.Job{Spec: vcbatch.JobSpec{}},
			want: 0,
		},
		{
			name: "single task with replicas",
			job: &vcbatch.Job{Spec: vcbatch.JobSpec{
				Tasks: []vcbatch.TaskSpec{{Name: "t1", Replicas: 3}},
			}},
			want: 3,
		},
		{
			name: "multiple tasks summed",
			job: &vcbatch.Job{Spec: vcbatch.JobSpec{
				Tasks: []vcbatch.TaskSpec{
					{Name: "t1", Replicas: 2},
					{Name: "t2", Replicas: 5},
					{Name: "t3", Replicas: 0},
				},
			}},
			want: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TotalTasks(tt.job); got != tt.want {
				t.Errorf("TotalTasks() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTotalTaskMinAvailable(t *testing.T) {
	tests := []struct {
		name string
		job  *vcbatch.Job
		want int32
	}{
		{
			name: "no tasks",
			job:  &vcbatch.Job{Spec: vcbatch.JobSpec{}},
			want: 0,
		},
		{
			name: "all tasks have MinAvailable set",
			job: &vcbatch.Job{Spec: vcbatch.JobSpec{
				Tasks: []vcbatch.TaskSpec{
					{Name: "t1", Replicas: 5, MinAvailable: int32Ptr(2)},
					{Name: "t2", Replicas: 4, MinAvailable: int32Ptr(3)},
				},
			}},
			want: 5,
		},
		{
			name: "MinAvailable nil falls back to Replicas",
			job: &vcbatch.Job{Spec: vcbatch.JobSpec{
				Tasks: []vcbatch.TaskSpec{
					{Name: "t1", Replicas: 4, MinAvailable: nil},
					{Name: "t2", Replicas: 2, MinAvailable: int32Ptr(1)},
				},
			}},
			want: 5,
		},
		{
			name: "MinAvailable zero is respected (not Replicas fallback)",
			job: &vcbatch.Job{Spec: vcbatch.JobSpec{
				Tasks: []vcbatch.TaskSpec{
					{Name: "t1", Replicas: 4, MinAvailable: int32Ptr(0)},
				},
			}},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TotalTaskMinAvailable(tt.job); got != tt.want {
				t.Errorf("TotalTaskMinAvailable() = %d, want %d", got, tt.want)
			}
		})
	}
}
