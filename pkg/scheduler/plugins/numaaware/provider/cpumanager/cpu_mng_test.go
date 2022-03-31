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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/topology"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"

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

func Test_GetTopologyHints(t *testing.T) {
	teseCases := []struct {
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
						"cpu": *resource.NewQuantity(4, ""),
					},
				},
			},
			resNumaSets: api.ResNumaSets{
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(2, 3, 4, 5),
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
	}

	for _, testcase := range teseCases {
		provider := NewProvider()
		topologyHintmap := provider.GetTopologyHints(&testcase.container, &numaInfo, testcase.resNumaSets)
		if !(reflect.DeepEqual(topologyHintmap["cpu"], testcase.expect) ||
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
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.NewCPUSet(0, 1, 2, 3),
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
				"cpu": cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.NewCPUSet(4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(0, 1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.NewCPUSet(0, 1, 2, 3, 4),
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
				"cpu": cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.NewCPUSet(1, 4, 5, 6, 7),
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
				"cpu": cpuset.NewCPUSet(1, 2, 3, 4, 5, 6, 7),
			},
			bestHit: &policy.TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0, 1)
					return mask
				}(),
				Preferred: true,
			},
			expect: cpuset.NewCPUSet(),
		},
	}

	for _, testcase := range teseCases {
		provider := NewProvider()
		assignMap := provider.Allocate(&testcase.container, testcase.bestHit, &numaInfo, testcase.resNumaSets)
		if !(reflect.DeepEqual(assignMap["cpu"], testcase.expect)) {
			t.Errorf("%s failed.\n", testcase.name)
		}
	}
}
