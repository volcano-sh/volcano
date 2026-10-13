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

package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func TestSubJobMinResources(t *testing.T) {
	gpu := v1.ResourceName("nvidia.com/gpu")
	newTask := func(cpu, memory, gpus float64) *TaskInfo {
		resource := &Resource{MilliCPU: cpu, Memory: memory, ScalarResources: map[v1.ResourceName]float64{gpu: gpus}}
		return &TaskInfo{InitResreq: resource, Resreq: resource, BestEffort: resource.IsEmpty()}
	}
	pending := TasksMap{"cpu-heavy": newTask(8000, 1, 0), "memory-heavy": newTask(1000, 8, 2), "small": newTask(2000, 2, 1)}
	tests := []struct {
		name                       string
		min                        int32
		ready, waiting, bestEffort bool
		cpu, memory, gpus          float64
	}{
		{name: "minimum smaller than pending", min: 1, cpu: 1000, memory: 1},
		{name: "heterogeneous lower bound", min: 2, cpu: 3000, memory: 3, gpus: 1},
		{name: "full gang", min: 3, cpu: 11000, memory: 11, gpus: 3},
		{name: "running member", min: 2, ready: true, cpu: 1000, memory: 1},
		{name: "pipelined member", min: 2, waiting: true, cpu: 1000, memory: 1},
		{name: "best effort member", min: 2, bestEffort: true, cpu: 1000, memory: 1},
		{name: "already ready", min: 1, ready: true},
		{name: "zero minimum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &SubJobInfo{MinAvailable: tt.min, TaskStatusIndex: map[TaskStatus]TasksMap{Pending: {}}}
			for name, task := range pending {
				job.TaskStatusIndex[Pending][name] = task
			}
			if tt.ready {
				job.TaskStatusIndex[Running] = TasksMap{"ready": newTask(9000, 9, 9)}
			}
			if tt.waiting {
				job.TaskStatusIndex[Pipelined] = TasksMap{"waiting": newTask(9000, 9, 9)}
			}
			if tt.bestEffort {
				job.TaskStatusIndex[Pending]["best-effort"] = newTask(0, 0, 0)
			}
			got := job.GetMinResources()
			if got.MilliCPU != tt.cpu || got.Memory != tt.memory || got.Get(gpu) != tt.gpus {
				t.Fatalf("got %v, want CPU %v memory %v GPU %v", got, tt.cpu, tt.memory, tt.gpus)
			}
		})
	}
}
