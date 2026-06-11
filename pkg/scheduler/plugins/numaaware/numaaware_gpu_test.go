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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/utils/cpuset"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/provider/gpumanager"
)

func Test_getNumaMaskForGPUID(t *testing.T) {
	gpuDetails := api.GPUDetails{
		0: {NUMANodeID: 0},
		1: {NUMANodeID: 0},
		2: {NUMANodeID: 1},
		3: {NUMANodeID: 1},
	}

	testCases := []struct {
		name   string
		gpus   cpuset.CPUSet
		expect []int
	}{
		{
			name:   "test-1-single-numa",
			gpus:   cpuset.New(0, 1),
			expect: []int{0},
		},
		{
			name:   "test-2-cross-numa",
			gpus:   cpuset.New(0, 2),
			expect: []int{0, 1},
		},
		{
			name:   "test-3-empty",
			gpus:   cpuset.New(),
			expect: []int{},
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.name, func(t *testing.T) {
			mask := getNumaMaskForGPUID(testcase.gpus, gpuDetails)
			got := mask.GetBits()
			if len(got) != len(testcase.expect) {
				t.Errorf("%s failed. got = %v, expect = %v\n", testcase.name, got, testcase.expect)
			}
		})
	}
}

func Test_getNodeNumaNumForTask_gpu_scoring(t *testing.T) {
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

	resAssignMap := map[string]api.ResNumaSets{
		"node-a": {
			"cpu":                                cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
		"node-b": {
			"cpu":                                cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
	}

	scores := getNodeNumaNumForTask(nodes, resAssignMap)

	if scores[0].Score != 1 {
		t.Errorf("node-a failed. got = %d, expect = %d\n", scores[0].Score, 1)
	}
	if scores[1].Score != 2 {
		t.Errorf("node-b failed. got = %d, expect = %d\n", scores[1].Score, 2)
	}

	if scores[0].Score > scores[1].Score {
		t.Errorf("scoring order failed. got node-a=%d >= node-b=%d\n", scores[0].Score, scores[1].Score)
	}
}

func Test_getNodeNumaNumForTask_mixed_cpu_gpu(t *testing.T) {
	node := &api.NodeInfo{
		Name: "node-mixed",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
			},
			GPUDetail: api.GPUDetails{
				0: {NUMANodeID: 1},
				1: {NUMANodeID: 1},
			},
		},
	}

	resAssignMap := map[string]api.ResNumaSets{
		"node-mixed": {
			"cpu":                                cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
	}

	scores := getNodeNumaNumForTask([]*api.NodeInfo{node}, resAssignMap)
	if scores[0].Score != 2 {
		t.Errorf("mixed node failed. got = %d, expect = %d\n", scores[0].Score, 2)
	}
}

func Test_getNodeNumaNumForTask_no_gpu_detail(t *testing.T) {
	node := &api.NodeInfo{
		Name: "node-no-gpu",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
				2: {NUMANodeID: 1},
				3: {NUMANodeID: 1},
			},
			GPUDetail: nil,
		},
	}

	resAssignMap := map[string]api.ResNumaSets{
		"node-no-gpu": {
			"cpu":                                cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
	}

	scores := getNodeNumaNumForTask([]*api.NodeInfo{node}, resAssignMap)
	if scores[0].Score != 1 {
		t.Errorf("no-gpu-detail failed. got = %d, expect = %d\n", scores[0].Score, 1)
	}
}

func Test_getNodeNumaNumForTask_gpu_only(t *testing.T) {
	node := &api.NodeInfo{
		Name: "node-gpu-only",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{},
			GPUDetail: api.GPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
				2: {NUMANodeID: 1},
				3: {NUMANodeID: 1},
			},
		},
	}

	resAssignMap := map[string]api.ResNumaSets{
		"node-gpu-only": {
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 2),
		},
	}

	scores := getNodeNumaNumForTask([]*api.NodeInfo{node}, resAssignMap)
	if scores[0].Score != 2 {
		t.Errorf("gpu-only failed. got = %d, expect = %d\n", scores[0].Score, 2)
	}
}

func Test_getNodeNumaNumForTask_four_numa(t *testing.T) {
	node := &api.NodeInfo{
		Name: "dgx-node",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topology.CPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 1},
				2: {NUMANodeID: 2},
				3: {NUMANodeID: 3},
			},
			GPUDetail: api.GPUDetails{
				0: {NUMANodeID: 0},
				1: {NUMANodeID: 0},
				2: {NUMANodeID: 1},
				3: {NUMANodeID: 1},
				4: {NUMANodeID: 2},
				5: {NUMANodeID: 2},
				6: {NUMANodeID: 3},
				7: {NUMANodeID: 3},
			},
		},
	}

	testCases := []struct {
		name   string
		gpus   cpuset.CPUSet
		cpus   cpuset.CPUSet
		expect int64
	}{
		{
			name:   "test-1-single-numa",
			gpus:   cpuset.New(0, 1),
			cpus:   cpuset.New(0),
			expect: 1,
		},
		{
			name:   "test-2-two-numas",
			gpus:   cpuset.New(0, 2),
			cpus:   cpuset.New(0),
			expect: 2,
		},
		{
			name:   "test-3-all-four-numas",
			gpus:   cpuset.New(0, 2, 4, 6),
			cpus:   cpuset.New(0, 1, 2, 3),
			expect: 4,
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.name, func(t *testing.T) {
			resAssignMap := map[string]api.ResNumaSets{
				"dgx-node": {
					"cpu":                                testcase.cpus,
					string(gpumanager.NvidiaGPUResource): testcase.gpus,
				},
			}
			scores := getNodeNumaNumForTask([]*api.NodeInfo{node}, resAssignMap)
			if scores[0].Score != testcase.expect {
				t.Errorf("%s failed. got = %d, expect = %d\n", testcase.name, scores[0].Score, testcase.expect)
			}
		})
	}
}

func Test_gpu_hint_allocate_score(t *testing.T) {
	topoInfo := &api.NumatopoInfo{
		CPUDetail: topology.CPUDetails{
			0: {NUMANodeID: 0, CoreID: 0, SocketID: 0},
			1: {NUMANodeID: 0, CoreID: 1, SocketID: 0},
			2: {NUMANodeID: 1, CoreID: 2, SocketID: 1},
			3: {NUMANodeID: 1, CoreID: 3, SocketID: 1},
		},
		GPUDetail: api.GPUDetails{
			0: {NUMANodeID: 0},
			1: {NUMANodeID: 0},
			2: {NUMANodeID: 1},
			3: {NUMANodeID: 1},
		},
	}

	container := v1.Container{
		Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{
				gpumanager.NvidiaGPUResource: *resource.NewQuantity(2, resource.DecimalSI),
			},
		},
	}

	resNumaSets := api.ResNumaSets{
		string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1, 2, 3),
	}

	provider := gpumanager.NewProvider()
	hintMap := provider.GetTopologyHints(&container, topoInfo, resNumaSets)
	hints := hintMap[string(gpumanager.NvidiaGPUResource)]
	if len(hints) == 0 {
		t.Fatal("no hints returned")
	}

	var bestHit *policy.TopologyHint
	for i := range hints {
		if hints[i].Preferred {
			bestHit = &hints[i]
			break
		}
	}
	if bestHit == nil {
		t.Fatal("no preferred hint")
	}
	if bestHit.NUMANodeAffinity.Count() != 1 {
		t.Errorf("preferred hint NUMA count failed. got = %d, expect = %d\n", bestHit.NUMANodeAffinity.Count(), 1)
	}

	allocMap := provider.Allocate(&container, bestHit, topoInfo, resNumaSets)
	allocated := allocMap[string(gpumanager.NvidiaGPUResource)]
	if allocated.Size() != 2 {
		t.Errorf("allocate failed. got = %d, expect = %d\n", allocated.Size(), 2)
	}

	numaID := bestHit.NUMANodeAffinity.GetBits()[0]
	for _, gpuIdx := range allocated.List() {
		if topoInfo.GPUDetail[gpuIdx].NUMANodeID != numaID {
			t.Errorf("GPU %d NUMA affinity failed. got = %d, expect = %d\n", gpuIdx, topoInfo.GPUDetail[gpuIdx].NUMANodeID, numaID)
		}
	}

	nodeAligned := &api.NodeInfo{
		Name:              "aligned",
		NumaSchedulerInfo: topoInfo,
	}
	nodeSplit := &api.NodeInfo{
		Name: "split",
		NumaSchedulerInfo: &api.NumatopoInfo{
			CPUDetail: topoInfo.CPUDetail,
			GPUDetail: topoInfo.GPUDetail,
		},
	}

	resAssign := map[string]api.ResNumaSets{
		"aligned": {
			"cpu":                                cpuset.New(0, 1),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 1),
		},
		"split": {
			"cpu":                                cpuset.New(0, 2),
			string(gpumanager.NvidiaGPUResource): cpuset.New(0, 2),
		},
	}

	scores := getNodeNumaNumForTask([]*api.NodeInfo{nodeAligned, nodeSplit}, resAssign)
	if scores[0].Score >= scores[1].Score {
		t.Errorf("score ordering failed. aligned=%d >= split=%d\n", scores[0].Score, scores[1].Score)
	}
}
