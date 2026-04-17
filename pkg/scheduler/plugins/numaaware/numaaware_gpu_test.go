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

package numaaware

import (
	"testing"

	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/utils/cpuset"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/provider/gpumanager"
)

func TestGetNumaMaskForGPUID(t *testing.T) {
	gpuDetails := api.GPUDetails{
		0: {NUMANodeID: 0},
		1: {NUMANodeID: 0},
		2: {NUMANodeID: 1},
		3: {NUMANodeID: 1},
	}

	tests := []struct {
		name   string
		gpus   cpuset.CPUSet
		expect []int
	}{
		{
			name:   "single numa",
			gpus:   cpuset.New(0, 1),
			expect: []int{0},
		},
		{
			name:   "both numas",
			gpus:   cpuset.New(0, 2),
			expect: []int{0, 1},
		},
		{
			name:   "empty",
			gpus:   cpuset.New(),
			expect: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mask := getNumaMaskForGPUID(tt.gpus, gpuDetails)
			got := mask.GetBits()
			if len(got) != len(tt.expect) {
				t.Errorf("mask bits = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestGetNodeNumaNumForTask_GPUScoring(t *testing.T) {
	// node-a: all GPUs on NUMA 0. node-b: GPUs split across NUMA 0 and 1.
	nodeA := &api.NodeInfo{
		Name: "node-a",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
			},
			GPUDetail: api.GPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
			},
		},
	}

	nodeB := &api.NodeInfo{
		Name: "node-b",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 1},
			},
			GPUDetail: api.GPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 1},
			},
		},
	}

	nodes := []*api.NodeInfo{nodeA, nodeB}

	// both nodes get 2 CPUs and 2 GPUs assigned
	resAssignMap := map[string]api.ResNumaSets{
		"node-a": {
			"cpu": cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
		"node-b": {
			"cpu": cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
	}

	scores := getNodeNumaNumForTask(nodes, resAssignMap)

	// node-a should have score 1 (all on NUMA 0)
	// node-b should have score 2 (spans NUMA 0 and 1)
	if scores[0].Score != 1 {
		t.Errorf("node-a score = %d, want 1", scores[0].Score)
	}
	if scores[1].Score != 2 {
		t.Errorf("node-b score = %d, want 2", scores[1].Score)
	}

	if scores[0].Score > scores[1].Score {
		t.Errorf("expected node-a < node-b, got %d vs %d", scores[0].Score, scores[1].Score)
	}
}
