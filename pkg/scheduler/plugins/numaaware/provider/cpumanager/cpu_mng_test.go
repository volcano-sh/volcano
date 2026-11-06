/*
Copyright 2021 The Volcano Authors.

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

package cpumanager

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"
	"k8s.io/utils/cpuset"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/numaaware/policy"
)

var numaInfo = api.NumatopoInfo{
	CPUDetail: topology.CPUDetails{
		0: {NUMANodeID: 0, CoreID: 0, SocketID: 0},
		1: {NUMANodeID: 0, CoreID: 0, SocketID: 0},
		2: {NUMANodeID: 0, CoreID: 1, SocketID: 0},
		3: {NUMANodeID: 0, CoreID: 1, SocketID: 0},
		4: {NUMANodeID: 1, CoreID: 2, SocketID: 1},
		5: {NUMANodeID: 1, CoreID: 2, SocketID: 1},
		6: {NUMANodeID: 1, CoreID: 3, SocketID: 1},
		7: {NUMANodeID: 1, CoreID: 3, SocketID: 1},
	},
}

// reservedFailTopoInfo represents a misconfigured node whose reserved CPU
// count (9) exceeds the total number of CPUs (8).
var reservedFailTopoInfo = api.NumatopoInfo{
	CPUDetail: numaInfo.CPUDetail,
	ResReserved: v1.ResourceList{
		v1.ResourceCPU: *resource.NewQuantity(9, ""),
	},
}

func Test_reserveCPUs(t *testing.T) {
	testCases := []struct {
		name      string
		topoInfo  api.NumatopoInfo
		expectSet cpuset.CPUSet
		expectErr bool
	}{
		{
			name:      "no-reservation",
			topoInfo:  numaInfo,
			expectSet: cpuset.New(),
			expectErr: false,
		},
		{
			name: "normal-reservation",
			topoInfo: api.NumatopoInfo{
				CPUDetail: numaInfo.CPUDetail,
				ResReserved: v1.ResourceList{
					v1.ResourceCPU: *resource.NewQuantity(2, ""),
				},
			},
			expectSet: cpuset.New(0, 1),
			expectErr: false,
		},
		{
			name:      "reservation-exceeds-total-cpus",
			topoInfo:  reservedFailTopoInfo,
			expectSet: cpuset.New(),
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		cputopo := &topology.CPUTopology{
			NumCPUs:    tc.topoInfo.CPUDetail.CPUs().Size(),
			NumCores:   getPhysicalCoresNum(tc.topoInfo.CPUDetail),
			NumSockets: tc.topoInfo.CPUDetail.Sockets().Size(),
			CPUDetails: tc.topoInfo.CPUDetail,
		}

		reserved, err := reserveCPUs(&tc.topoInfo, cputopo)
		if (err != nil) != tc.expectErr {
			t.Errorf("%s: expected error=%v, got %v", tc.name, tc.expectErr, err)
		}
		if !reserved.Equals(tc.expectSet) {
			t.Errorf("%s: expected reserved=%v, got %v", tc.name, tc.expectSet, reserved)
		}
	}
}

func Test_GetTopologyHints(t *testing.T) {
	teseCases := []struct {
		name        string
		container   v1.Container
		resNumaSets api.ResNumaSets
		topoInfo    *api.NumatopoInfo
		expect      []policy.TopologyHint
	}{
		{
			name: "test-1",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(5, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(8, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(9, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			expect: []policy.TopologyHint{},
		},
		{
			name: "test-6",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(2, 3, 4, 5),
			},
			expect: []policy.TopologyHint{
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
			name: "test-7-reservation-exceeds-total-cpus",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			topoInfo: &reservedFailTopoInfo,
			expect:   []policy.TopologyHint{},
		},
	}

	for _, testcase := range teseCases {
		provider := NewProvider()
		topoInfo := testcase.topoInfo
		if topoInfo == nil {
			topoInfo = &numaInfo
		}
		topologyHintmap := provider.GetTopologyHints(&testcase.container, topoInfo, testcase.resNumaSets)
		if !(equality.Semantic.DeepEqual(topologyHintmap["cpu"], testcase.expect) ||
			(len(topologyHintmap["cpu"]) == 0 && len(testcase.expect) == 0)) {
			t.Errorf("%s failed. topologyHintmap = %v\n", testcase.name, topologyHintmap)
		}
	}
}

func Test_Allocate(t *testing.T) {
	teseCases := []struct {
		name        string
		container   v1.Container
		resNumaSets api.ResNumaSets
		bestHit     *policy.TopologyHint
		topoInfo    *api.NumatopoInfo
		expect      cpuset.CPUSet
	}{
		{
			name: "test-1",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(5, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
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
						"cpu": *resource.NewQuantity(5, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(1, 4, 5, 6, 7),
		},
		{
			name: "test-5",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(8, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.New(),
		},
		{
			name: "test-6-reservation-exceeds-total-cpus",
			container: v1.Container{
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.New(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0)
					return mask
				}(),
				Preferred: true,
			},
			topoInfo: &reservedFailTopoInfo,
			expect:   cpuset.New(),
		},
	}

	for _, testcase := range teseCases {
		provider := NewProvider()
		topoInfo := testcase.topoInfo
		if topoInfo == nil {
			topoInfo = &numaInfo
		}
		assignMap := provider.Allocate(&testcase.container, testcase.bestHit, topoInfo, testcase.resNumaSets)
		if !(equality.Semantic.DeepEqual(assignMap["cpu"], testcase.expect)) {
			t.Errorf("%s failed.\n", testcase.name)
		}
	}
}
