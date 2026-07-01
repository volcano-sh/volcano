/*
Copyright 2025 The Volcano Authors.

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

package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/cpuset"
)

func newNumatopoInfoForTest() *NumatopoInfo {
	return &NumatopoInfo{
		Name: "node1",
		NumaResMap: map[string]*ResourceInfo{
			"cpu": {
				Allocatable:        cpuset.New(0, 1, 2, 3),
				Capacity:           4,
				AllocatablePerNuma: map[int]float64{0: 4},
				UsedPerNuma:        map[int]float64{0: 0},
			},
		},
	}
}

func taskWithTopologyDecision(decision string) *TaskInfo {
	return &TaskInfo{
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{topologyDecisionAnnotation: decision},
			},
		},
	}
}

// A pod can carry an arbitrary volcano.sh/topology-decision annotation. AddTask
// must not index NumaResMap with a resource the node does not track, otherwise
// the nil entry is dereferenced and the scheduler panics.
func TestAddTaskUnknownResource(t *testing.T) {
	info := newNumatopoInfoForTest()
	info.AddTask(taskWithTopologyDecision(`{"numa":{"0":{"nvidia.com/gpu":"1"}}}`))

	if got := info.NumaResMap["cpu"].UsedPerNuma[0]; got != 0 {
		t.Errorf("cpu UsedPerNuma[0] = %v, want 0", got)
	}
}

// A negative usage in the decision would lower recorded usage below the real
// value and make the node look emptier than it is, so it must be ignored.
func TestAddTaskNegativeUsageIgnored(t *testing.T) {
	info := newNumatopoInfoForTest()
	info.AddTask(taskWithTopologyDecision(`{"numa":{"0":{"cpu":"-1000"}}}`))

	if got := info.NumaResMap["cpu"].UsedPerNuma[0]; got != 0 {
		t.Errorf("cpu UsedPerNuma[0] = %v, want 0", got)
	}
}

func TestAddTaskValidUsage(t *testing.T) {
	info := newNumatopoInfoForTest()
	info.AddTask(taskWithTopologyDecision(`{"numa":{"0":{"cpu":"2"}}}`))

	if got := info.NumaResMap["cpu"].UsedPerNuma[0]; got != 2000 {
		t.Errorf("cpu UsedPerNuma[0] = %v, want 2000", got)
	}
}

// RemoveTask must walk the decoded decision, the same source AddTask folds in.
// A task can carry the topology-decision annotation while NumaInfo is unset, so
// reading ti.NumaInfo.ResMap there would dereference a nil and panic.
func TestRemoveTaskNilNumaInfo(t *testing.T) {
	info := newNumatopoInfoForTest()
	info.NumaResMap["cpu"].UsedPerNuma[0] = 2000

	ti := taskWithTopologyDecision(`{"numa":{"0":{"cpu":"2"}}}`)
	ti.NumaInfo = nil
	info.RemoveTask(ti)

	if got := info.NumaResMap["cpu"].UsedPerNuma[0]; got != 0 {
		t.Errorf("cpu UsedPerNuma[0] = %v, want 0", got)
	}
}
