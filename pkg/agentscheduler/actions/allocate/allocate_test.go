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

package allocate

import (
	"strings"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	api "volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

func TestAllocateTask_PreCheck_NotAllocatable(t *testing.T) {
	// node with small allocatable resources
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		Status: v1.NodeStatus{Allocatable: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse("500m"),
			v1.ResourceMemory: resource.MustParse("256Mi"),
		}},
	}

	vcNode := api.NewNodeInfo(node)

	snapshot := k8sutil.NewEmptySnapshot()
	snapshot.AddOrUpdateNodes([]*api.NodeInfo{vcNode})

	utilFwk := k8sutil.NewFramework(nil, k8sutil.WithSnapshotSharedLister(snapshot))
	fw := &framework.Framework{Framework: utilFwk}

	alloc := &Action{fwk: fw}

	// pod requesting more CPU than any node allocatable
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "p1"},
		Spec: v1.PodSpec{Containers: []v1.Container{{
			Name: "c",
			Resources: v1.ResourceRequirements{Requests: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1000m"),
				v1.ResourceMemory: resource.MustParse("128Mi"),
			}},
		}}},
	}
	task := api.NewTaskInfo(pod)
	schedCtx := &agentapi.SchedulingContext{Task: task}

	err := alloc.allocateTask(schedCtx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if _, ok := err.(*api.FitErrors); !ok {
		t.Fatalf("expected FitErrors, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Insufficient") {
		t.Fatalf("expected insufficient message, got %v", err.Error())
	}
}
