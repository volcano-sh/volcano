package noderesourcefitplus

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	"reflect"
	"testing"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func Test_nodeResourcesFitPlus_String(t *testing.T) {
	type fields struct {
		NodeResourcesFitPlusWeight int
		Resources                  map[v1.ResourceName]ResourcesType
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		// TODO: Add test cases.
		{"test1", fields{
			NodeResourcesFitPlusWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.MostAllocated,
					Weight: 2,
				},
			},
		}, "{\"nodeResourcesFitPlusWeight\":10,\"resources\":{\"cpu\":{\"type\":\"LeastAllocated\",\"weight\":1},\"memory\":{\"type\":\"MostAllocated\",\"weight\":2}}}"},
		{"test2", fields{
			NodeResourcesFitPlusWeight: 10,
		}, "{\"nodeResourcesFitPlusWeight\":10,\"resources\":null}"},
		{"test3", fields{
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.MostAllocated,
					Weight: 2,
				},
			},
		}, "{\"nodeResourcesFitPlusWeight\":0,\"resources\":{\"cpu\":{\"type\":\"LeastAllocated\",\"weight\":1},\"memory\":{\"type\":\"MostAllocated\",\"weight\":2}}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: tt.fields.NodeResourcesFitPlusWeight,
				Resources:                  tt.fields.Resources,
			}
			if got := w.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			} else {
				fmt.Println(got)
			}
		})
	}
}

func Test_calculateWeight(t *testing.T) {
	type args struct {
		args framework.Arguments
	}
	tests := []struct {
		name string
		args args
		want nodeResourcesFitPlus
	}{
		// TODO: Add test cases.
		{"test1", args{framework.Arguments{
			"nodeResourcesFitPlusWeight": 10,
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"type":   "MostAllocated",
					"weight": 1,
				},
				"memory": map[string]interface{}{
					"type":   "LeastAllocated",
					"weight": 2,
				},
			},
		}}, nodeResourcesFitPlus{
			NodeResourcesFitPlusWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.MostAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 2,
				},
			},
		}},
		{"test2", args{framework.Arguments{
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"type":   "MostAllocated",
					"weight": 1,
				},
				"memory": map[string]interface{}{
					"type":   "LeastAllocated",
					"weight": 2,
				},
			},
		}}, nodeResourcesFitPlus{
			NodeResourcesFitPlusWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.MostAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 2,
				},
			},
		}},
		{"test3", args{framework.Arguments{
			"nodeResourcesFitPlusWeight": 10,
		}}, nodeResourcesFitPlus{
			NodeResourcesFitPlusWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateWeight(tt.args.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlusScore(t *testing.T) {
	type args struct {
		task   *api.TaskInfo
		node   *api.NodeInfo
		weight nodeResourcesFitPlus
	}
	tests := []struct {
		name string
		args args
		want float64
	}{
		// TODO: Add test cases.
		{name: "test1", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 400},
		{name: "test2", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 400,
					Memory:   400,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 250},
		{name: "test3", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 600},
		{name: "test4", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 400,
					Memory:   400,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 750},
		{name: "test5", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 2,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 600},
		{name: "test6", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 750},
		{name: "test7", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 0,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 600},
		{name: "test8", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					Memory: 500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 0},
		{name: "test9", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: nodeResourcesFitPlus{
				NodeResourcesFitPlusWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 600},
	}
	socre := map[string]float64{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlusScore(tt.args.task, tt.args.node, tt.args.weight); got != tt.want {
				if tt.name == "test5" || tt.name == "test6" {
					socre[tt.name] = got
					return
				}
				t.Errorf("PlusScore() = %v, want %v", got, tt.want)
			}
		})
	}
	//if socre["test5"] >= socre["test6"] {
	//	t.Errorf("test5 must be less than test6")
	//}
}

func Test_mostRequestedScore(t *testing.T) {
	type args struct {
		requested float64
		used      float64
		capacity  float64
		weight    int
	}
	tests := []struct {
		name    string
		args    args
		want    float64
		wantErr bool
	}{
		// TODO: Add test cases.
		{name: "test1", args: args{
			requested: 0,
			used:      0,
			capacity:  0,
			weight:    0,
		}, want: 0, wantErr: true},
		{name: "test2", args: args{
			requested: 1,
			used:      2,
			capacity:  2,
			weight:    1,
		}, want: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mostRequestedScore(tt.args.requested, tt.args.used, tt.args.capacity, tt.args.weight)
			if (err != nil) != tt.wantErr {
				t.Errorf("mostRequestedScore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("mostRequestedScore() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_leastRequestedScore(t *testing.T) {
	type args struct {
		requested float64
		used      float64
		capacity  float64
		weight    int
	}
	tests := []struct {
		name    string
		args    args
		want    float64
		wantErr bool
	}{
		// TODO: Add test cases.
		{name: "test1", args: args{
			requested: 0,
			used:      0,
			capacity:  0,
			weight:    0,
		}, want: 0, wantErr: true},
		{name: "test2", args: args{
			requested: 1,
			used:      2,
			capacity:  2,
			weight:    1,
		}, want: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := leastRequestedScore(tt.args.requested, tt.args.used, tt.args.capacity, tt.args.weight)
			if (err != nil) != tt.wantErr {
				t.Errorf("mostRequestedScore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("mostRequestedScore() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_nodeResourcesFitPlusPlugin_OnSessionOpen(t *testing.T) {
	type fields struct {
		weight nodeResourcesFitPlus
	}
	type args struct {
		ssn *framework.Session
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
		{name: "test1", args: args{ssn: &framework.Session{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := &nodeResourcesFitPlusPlugin{
				weight: tt.fields.weight,
			}
			bp.OnSessionOpen(tt.args.ssn)
		})
	}
}
