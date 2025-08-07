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

package predicates

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func buildTask(name, cpu, memory, gpu string) *api.TaskInfo {
	return &api.TaskInfo{
		Name:   name,
		Resreq: api.NewResource(api.BuildResourceListWithGPU(cpu, memory, gpu)),
	}
}

func buildNode(name, cpu, memory, gpu string) *api.NodeInfo {
	return &api.NodeInfo{
		Name: name,
		Idle: api.NewResource(api.BuildResourceListWithGPU(cpu, memory, gpu)),
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
		want    int
		wantErr bool
	}{
		{
			"cpu_task_less_than_reserved_resource",
			args{
				task:         t1,
				node:         n1,
				proportional: proportional,
			},
			api.Success,
			false,
		},
		{
			"cpu_task_greater_than_reserved_resource",
			args{
				task:         t2,
				node:         n1,
				proportional: proportional,
			},
			api.UnschedulableAndUnresolvable,
			true,
		},
		{
			"gpu_task_no_proportional_check",
			args{
				task:         t3,
				node:         n1,
				proportional: proportional,
			},
			api.Success,
			false,
		},
		{
			"cpu_task_less_than_idle_resource",
			args{
				task:         t2,
				node:         n2,
				proportional: proportional,
			},
			api.Success,
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
			if got.Code != tt.want {
				t.Errorf("checkNodeResourceIsProportional() got = %v, want %v", got, tt.want)
			}
		})
	}
}
