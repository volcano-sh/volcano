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

package gpumanager

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"
	"k8s.io/utils/cpuset"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
)

var gpuNumaInfo = api.NumatopoInfo{
	GPUDetail: api.GPUDetails{
		0: {NUMANodeID: 0},
		1: {NUMANodeID: 0},
		2: {NUMANodeID: 0},
		3: {NUMANodeID: 0},
		4: {NUMANodeID: 1},
		5: {NUMANodeID: 1},
		6: {NUMANodeID: 1},
		7: {NUMANodeID: 1},
	},
}

func Test_GetTopologyHints(t *testing.T) {
	testCases := []struct {
		name        string
		container   v1.Container
		resNumaSets api.ResNumaSets
		expect      []policy.TopologyHint
	}{
		{
			name: "test-1",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0)
						return mask
					}(),
					Preferred: true,
				},
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(1)
						return mask
					}(),
					Preferred: true,
				},
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0, 1)
						return mask
					}(),
					Preferred: false,
				},
			},
		},
		{
			name: "test-2",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(1, 2, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(1)
						return mask
					}(),
					Preferred: true,
				},
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0, 1)
						return mask
					}(),
					Preferred: false,
				},
			},
		},
		{
			name: "test-3",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(5, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0, 1)
						return mask
					}(),
					Preferred: true,
				},
			},
		},
		{
			name: "test-4",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(8, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0, 1)
						return mask
					}(),
					Preferred: true,
				},
			},
		},
		{
			name: "test-5",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(9, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{},
		},
		{
			name: "test-6",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: nil,
		},
	}

	for _, testcase := range testCases {
		provider := NewProvider()
		topologyHintmap := provider.GetTopologyHints(&testcase.container, &gpuNumaInfo, testcase.resNumaSets)
		got := topologyHintmap[string(NvidiaGPUResource)]
		if !(equality.Semantic.DeepEqual(got, testcase.expect) ||
			(len(got) == 0 && len(testcase.expect) == 0)) {
			t.Errorf("%s failed. got = %v, expect = %v\n", testcase.name, got, testcase.expect)
		}
	}
}

func Test_Allocate(t *testing.T) {
	testCases := []struct {
		name        string
		container   v1.Container
		resNumaSets api.ResNumaSets
		bestHit     *policy.TopologyHint
		expect      cpuset.CPUSet
	}{
		{
			name: "test-1",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(0, 1, 2, 3),
		},
		{
			name: "test-2",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(4, 5, 6, 7),
		},
		{
			name: "test-3",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(5, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(0, 1, 2, 3, 4),
		},
		{
			name: "test-4",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						NvidiaGPUResource: *resource.NewQuantity(4, resource.DecimalSI),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				string(NvidiaGPUResource): cpuset.New(1, 2, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(4, 5, 6, 7),
		},
	}

	for _, testcase := range testCases {
		provider := NewProvider()
		assignMap := provider.Allocate(&testcase.container, testcase.bestHit, &gpuNumaInfo, testcase.resNumaSets)
		if !(equality.Semantic.DeepEqual(assignMap[string(NvidiaGPUResource)], testcase.expect)) {
			t.Errorf("%s failed. got = %v, expect = %v\n",
				testcase.name, assignMap[string(NvidiaGPUResource)], testcase.expect)
		}
	}
}
