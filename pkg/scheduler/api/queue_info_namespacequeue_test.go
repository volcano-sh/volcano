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

package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
)

func TestNewNamespaceQueueInfoPreservesQueueFields(t *testing.T) {
	reclaimable := true
	queue := &scheduling.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "compute"},
		Spec: scheduling.NamespaceQueueSpec{
			Parent:     "cluster/default",
			Capability: resourceList("4"),
			Guarantee: scheduling.Guarantee{
				Resource: resourceList("1"),
			},
			Reclaimable: &reclaimable,
		},
		Status: scheduling.NamespaceQueueStatus{
			State:     scheduling.QueueStateOpen,
			Allocated: resourceList("2"),
			Reservation: scheduling.Reservation{
				Nodes:    []string{"node-a"},
				Resource: resourceList("1"),
			},
		},
	}

	info, err := NewNamespaceQueueInfo(queue)
	if err != nil {
		t.Fatalf("NewNamespaceQueueInfo() error = %v", err)
	}
	if info.UID != NamespaceQueueID("team-a", "compute") {
		t.Fatalf("UID = %q, want %q", info.UID, NamespaceQueueID("team-a", "compute"))
	}
	if info.Scope != NamespaceQueueScope || info.Namespace != "team-a" {
		t.Fatalf("scope/namespace = %q/%q", info.Scope, info.Namespace)
	}
	if info.Queue == nil || info.Queue.Spec.Parent != "default" {
		t.Fatalf("internal Queue parent = %q, want default", info.Queue.Spec.Parent)
	}
	capability := info.Queue.Spec.Capability["cpu"]
	guarantee := info.Queue.Spec.Guarantee.Resource["cpu"]
	allocated := info.Queue.Status.Allocated["cpu"]
	if capability.Cmp(resource.MustParse("4")) != 0 ||
		guarantee.Cmp(resource.MustParse("1")) != 0 ||
		allocated.Cmp(resource.MustParse("2")) != 0 {
		t.Fatalf("queue resource fields were not copied")
	}
	if !info.Reclaimable() {
		t.Fatal("expected NamespaceQueue to be reclaimable")
	}
}

func TestNewNamespaceQueueInfoUsesNamespacedParentID(t *testing.T) {
	queue := &scheduling.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child"},
		Spec:       scheduling.NamespaceQueueSpec{Parent: "parent"},
	}

	info, err := NewNamespaceQueueInfo(queue)
	if err != nil {
		t.Fatalf("NewNamespaceQueueInfo() error = %v", err)
	}
	if got, want := info.Queue.Spec.Parent, string(NamespaceQueueID("team-a", "parent")); got != want {
		t.Fatalf("internal Queue parent = %q, want %q", got, want)
	}
}

func resourceList(cpu string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(cpu)}
}
