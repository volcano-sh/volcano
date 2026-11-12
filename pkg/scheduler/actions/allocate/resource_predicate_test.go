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

package allocate

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestResourcePredicatePreservesFutureCapacity(t *testing.T) {
	for _, name := range []string{"idle", "release", "pipeline", "zero-scalar-entry", "fractional-negative", "missing-idle-scalar-map"} {
		t.Run(name, func(t *testing.T) {
			node := &api.NodeInfo{Name: "node", Idle: &api.Resource{MilliCPU: 1000, Memory: 4096, ScalarResources: map[v1.ResourceName]float64{api.GPUResourceName: 2}}, Releasing: api.EmptyResource(), Pipelined: api.EmptyResource()}
			switch name {
			case "release":
				node.Releasing.MilliCPU = 1000
				node.Releasing.ScalarResources = map[v1.ResourceName]float64{api.GPUResourceName: 2}
			case "pipeline":
				node.Pipelined.MilliCPU = 500
				node.Pipelined.ScalarResources = map[v1.ResourceName]float64{api.GPUResourceName: 1}
			case "zero-scalar-entry":
				node.Pipelined.ScalarResources = map[v1.ResourceName]float64{api.GPUResourceName: 0}
			case "fractional-negative":
				node.Releasing.MilliCPU = -.09
			case "missing-idle-scalar-map":
				node.Idle.ScalarResources = nil
				node.Pipelined.ScalarResources = map[v1.ResourceName]float64{api.GPUResourceName: 1}
			}
			idle, releasing, pipeline := node.Idle.Clone(), node.Releasing.Clone(), node.Pipelined.Clone()
			action := &Action{session: &framework.Session{}}
			for _, cpu := range []float64{500, 999.99, 1500, 3000} {
				for _, gpu := range []float64{0, 1, 2, 3} {
					request := &api.Resource{MilliCPU: cpu, Memory: 1024, ScalarResources: map[v1.ResourceName]float64{api.GPUResourceName: gpu}}
					want, _ := request.LessEqualWithResourcesName(node.FutureIdle(), api.Zero)
					task := &api.TaskInfo{Namespace: "ns", Name: "pod", InitResreq: request, Resreq: request, Pod: &v1.Pod{}}
					if got := action.predicate(task, node) == nil; got != want {
						t.Fatalf("CPU %v GPU %v: got %v, want %v", cpu, gpu, got, want)
					}
				}
			}
			if !reflect.DeepEqual(node.Idle, idle) || !reflect.DeepEqual(node.Releasing, releasing) || !reflect.DeepEqual(node.Pipelined, pipeline) {
				t.Fatal("resource predicate mutated node capacity")
			}
		})
	}
}
