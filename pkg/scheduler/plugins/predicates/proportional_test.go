package predicates

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildTask(name, cpu, memory, gpu string) *api.TaskInfo {
	return &api.TaskInfo{
		Name:   name,
		Resreq: api.NewResource(util.BuildResourceListWithGPU(cpu, memory, gpu)),
	}
}

func buildNode(name, cpu, memory, gpu string) *api.NodeInfo {
	return &api.NodeInfo{
		Name: name,
		Idle: api.NewResource(util.BuildResourceListWithGPU(cpu, memory, gpu)),
	}
}

func Test_checkNodeResourceIsProportional(t *testing.T) {

	t1 := buildTask("t1", "4", "4G", "0")
	t2 := buildTask("t1", "10", "10G", "0")
	t3 := buildTask("t1", "10", "10G", "1")
	n1 := buildNode("n1", "30", "30G", "6")
	n2 := buildNode("n2", "26", "26G", "2")
	proportional := map[v1.ResourceName]baseResource{
		"nvidia.com/gpu": {
			CPU:    4,
			Memory: 4,
		},
	}

	type args struct {
		task         *api.TaskInfo
		node         *api.NodeInfo
		proportional map[v1.ResourceName]baseResource
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			"cpu_task_less_than_reserved_resource",
			args{
				task:         t1,
				node:         n1,
				proportional: proportional,
			},
			true,
			false,
		},
		{
			"cpu_task_greater_than_reserved_resource",
			args{
				task:         t2,
				node:         n1,
				proportional: proportional,
			},
			false,
			true,
		},
		{
			"gpu_task_no_proportional_check",
			args{
				task:         t3,
				node:         n1,
				proportional: proportional,
			},
			true,
			false,
		},
		{
			"cpu_task_less_than_idle_resource",
			args{
				task:         t2,
				node:         n2,
				proportional: proportional,
			},
			true,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checkNodeResourceIsProportional(tt.args.task, tt.args.node, tt.args.proportional)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkNodeResourceIsProportional() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("checkNodeResourceIsProportional() got = %v, want %v", got, tt.want)
			}
		})
	}
}
