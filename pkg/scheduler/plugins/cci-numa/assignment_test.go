package ccinuma

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/kubernetes/pkg/kubelet/cm/topologymanager/bitmask"
	"reflect"
	"testing"
	"volcano.sh/volcano/pkg/scheduler/util"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestGetNodeNumaRes(t *testing.T) {
	type args struct {
		topoInfo *api.NumatopoInfo
	}
	tests := []struct {
		name string
		args args
		want ResNumaSetType
	}{
		{
			name: "test-0",
			args: args{
				topoInfo: &api.NumatopoInfo{
					NumaResMap: map[string]*api.ResourceInfo{
						"cpu": {
							AllocatablePerNuma: map[int]float64{
								0: 4000,
								1: 3000,
							},
						},
					},
				},
			},
			want: ResNumaSetType{
				"cpu": {
					0: 4000,
					1: 3000,
				},
			},
		},
		{
			name: "test-1",
			args: args{
				topoInfo: &api.NumatopoInfo{
					NumaResMap: map[string]*api.ResourceInfo{
						"cpu": {
							AllocatablePerNuma: map[int]float64{
								0: 4000,
								1: 4000,
							},
							UsedPerNuma: map[int]float64{
								0: 1000,
								1: 2000,
							},
						},
					},
				},
			},
			want: ResNumaSetType{
				"cpu": {
					0: 3000,
					1: 2000,
				},
			},
		},
		{
			name: "test-2",
			args: args{
				topoInfo: &api.NumatopoInfo{
					NumaResMap: map[string]*api.ResourceInfo{
						"cpu": {
							AllocatablePerNuma: map[int]float64{
								0: 4000,
								1: 4000,
							},
							UsedPerNuma: map[int]float64{
								0: 1000,
							},
						},
					},
				},
			},
			want: ResNumaSetType{
				"cpu": {
					0: 3000,
					1: 4000,
				},
			},
		},
		{
			name: "test-3",
			args: args{
				topoInfo: &api.NumatopoInfo{
					NumaResMap: map[string]*api.ResourceInfo{
						"cpu": {
							AllocatablePerNuma: map[int]float64{
								0: 4000,
								1: 4000,
							},
							UsedPerNuma: map[int]float64{
								0: 1000,
							},
						},
						"gpu": {
							AllocatablePerNuma: map[int]float64{
								0: 4000,
								1: 4000,
							},
							UsedPerNuma: map[int]float64{
								0: 1000,
								1: 3000,
							},
						},
					},
				},
			},
			want: ResNumaSetType{
				"gpu": {
					0: 3000,
					1: 1000,
				},
				"cpu": {
					0: 3000,
					1: 4000,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetNodeNumaRes(tt.args.topoInfo); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNodeNumaRes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateTopologyHints(t *testing.T) {
	type args struct {
		request      float64
		availableRes map[int]float64
	}
	tests := []struct {
		name string
		args args
		want []TopologyHint
	}{
		{
			name: "test-0",
			args: args{
				request: 16,
				availableRes: map[int]float64{
					0: 16,
					1: 16,
				},
			},
			want: []TopologyHint{
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
			name: "test-1",
			args: args{
				request: 16,
				availableRes: map[int]float64{
					0: 16,
					1: 8,
				},
			},
			want: []TopologyHint{
				{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0)
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
			args: args{
				request: 16,
				availableRes: map[int]float64{
					0: 8,
					1: 16,
				},
			},
			want: []TopologyHint{
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
			args: args{
				request: 20,
				availableRes: map[int]float64{
					0: 16,
					1: 8,
				},
			},
			want: []TopologyHint{
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
			args: args{
				request: 30,
				availableRes: map[int]float64{
					0: 16,
					1: 8,
				},
			},
			want: []TopologyHint{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateTopologyHints(tt.args.request, tt.args.availableRes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GenerateTopologyHints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeFilteredHints(t *testing.T) {
	type args struct {
		task            *api.TaskInfo
		nodeNumaIdleMap ResNumaSetType
		numaNodes       []int
		filteredHints   [][]TopologyHint
	}
	tests := []struct {
		name string
		args args
		want TopologyHint
	}{
		{
			name: "test-0",
			args: args{
				task: &api.TaskInfo{
					Resreq: api.NewResource(util.BuildResourceListWithGPU("4", "4G", "0")),
				},
				nodeNumaIdleMap: ResNumaSetType{
					"cpu": map[int]float64{
						0: 3000,
						1: 4000,
					},
					"memory": map[int]float64{
						0: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
						1: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("2G")),
					},
					api.GPUResourceName: map[int]float64{
						0: 2,
						1: 2,
					},
				},
				numaNodes: []int{0, 1},
				filteredHints: [][]TopologyHint{
					{
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
					{
						{
							NUMANodeAffinity: func() bitmask.BitMask {
								mask, _ := bitmask.NewBitMask(0)
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
			},
			want: TopologyHint{
				NUMANodeAffinity: func() bitmask.BitMask {
					mask, _ := bitmask.NewBitMask(0)
					return mask
				}(),
				Preferred: false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MergeFilteredHints(tt.args.task, tt.args.nodeNumaIdleMap, tt.args.numaNodes, tt.args.filteredHints); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MergeFilteredHints() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllocateNumaResForTask(t *testing.T) {
	type args struct {
		task            *api.TaskInfo
		hit             TopologyHint
		nodeNumaIdleMap ResNumaSetType
	}
	tests := []struct {
		name string
		args args
		want ResNumaSetType
	}{
		{
			name: "test-0",
			args: args{
				task: &api.TaskInfo{
					Resreq: api.NewResource(util.BuildResourceListWithGPU("4", "4G", "0")),
				},
				hit: TopologyHint{
					NUMANodeAffinity: func() bitmask.BitMask {
						mask, _ := bitmask.NewBitMask(0)
						return mask
					}(),
					Preferred: false,
				},
				nodeNumaIdleMap: ResNumaSetType{
					"cpu": map[int]float64{
						0: 3000,
						1: 4000,
					},
					"memory": map[int]float64{
						0: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
						1: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("2G")),
					},
				},
			},
			want: ResNumaSetType{
				"cpu": map[int]float64{
					0: 3000,
					1: 1000,
				},
				"memory": map[int]float64{
					0: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AllocateNumaResForTask(tt.args.task, tt.args.hit, tt.args.nodeNumaIdleMap); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AllocateNumaResForTask() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNodeNumaNumForTask(t *testing.T) {
	type args struct {
		nodeInfo     []*api.NodeInfo
		resAssignMap map[string]ResNumaSetType
	}
	tests := []struct {
		name string
		args args
		want map[string]int64
	}{
		{
			name: "test-0",
			args: args{
				nodeInfo: []*api.NodeInfo {
					{
						Name: "node1",
					},
					{
						Name: "node2",
					},
					{
						Name: "node3",
					},
				},
				resAssignMap: map[string]ResNumaSetType {
					"node1" : {
						"cpu": map[int]float64{
							0: 3000,
							1: 1000,
						},
						"memory": map[int]float64{
							0: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
						},
					},
					"node2" : {
						"cpu": map[int]float64{
							0: 3000,
						},
						"memory": map[int]float64{
							0: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
						},
					},
					"node3" : {
						"cpu": map[int]float64{
							0: 3000,
						},
						"memory": map[int]float64{
							1: api.ResQuantity2Float64(v1.ResourceMemory, resource.MustParse("4G")),
						},
					},
				},
			},
			want: map[string]int64 {
				"node1": 2,
				"node2": 1,
				"node3": 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetNodeNumaNumForTask(tt.args.nodeInfo, tt.args.resAssignMap); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetNodeNumaNumForTask() = %v, want %v", got, tt.want)
			}
		})
	}
}
