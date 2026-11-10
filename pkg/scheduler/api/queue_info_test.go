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
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestNewNamespaceQueueInfo(t *testing.T) {
	reclaimable := true
	tests := []struct {
		name     string
		queue    *scheduling.NamespaceQueue
		wantErr  bool
		validate func(t *testing.T, info *QueueInfo)
	}{
		{
			name: "preserves queue fields",
			queue: &scheduling.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "compute", Generation: 1},
				Spec: scheduling.NamespaceQueueSpec{
					Parent:     "cluster/default",
					Capability: resourceList("4"),
					Guarantee: scheduling.Guarantee{
						Resource: resourceList("1"),
					},
					Reclaimable: &reclaimable,
				},
				Status: scheduling.NamespaceQueueStatus{
					State: scheduling.QueueStateOpen,
					Conditions: []metav1.Condition{
						{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
						{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
					},
					Allocated: resourceList("2"),
					Reservation: scheduling.Reservation{
						Nodes:    []string{"node-a"},
						Resource: resourceList("1"),
					},
				},
			},
			validate: func(t *testing.T, info *QueueInfo) {
				if info.UID != NamespaceQueueID("team-a", "compute") {
					t.Fatalf("UID = %q, want %q", info.UID, NamespaceQueueID("team-a", "compute"))
				}
				if info.Scope != NamespaceQueueScope || info.Namespace != "team-a" {
					t.Fatalf("scope/namespace = %q/%q", info.Scope, info.Namespace)
				}
				if info.Queue == nil || info.Queue.Spec.Parent != "default" {
					t.Fatalf("internal Queue parent = %q, want default", info.Queue.Spec.Parent)
				}
				if info.Queue.Status.State != scheduling.QueueStateOpen {
					t.Fatalf("internal Queue state = %q, want Open", info.Queue.Status.State)
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
			},
		},
		{
			name: "uses namespaced parent ID",
			queue: &scheduling.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child"},
				Spec:       scheduling.NamespaceQueueSpec{Parent: "parent"},
			},
			validate: func(t *testing.T, info *QueueInfo) {
				if got, want := info.Queue.Spec.Parent, string(NamespaceQueueID("team-a", "parent")); got != want {
					t.Fatalf("internal Queue parent = %q, want %q", got, want)
				}
			},
		},
		{
			name:    "rejects nil queue",
			queue:   nil,
			wantErr: true,
		},
		{
			name: "rejects empty parent",
			queue: &scheduling.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := NewNamespaceQueueInfo(tt.queue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewNamespaceQueueInfo() error = %v, wantErr = %t", err, tt.wantErr)
			}
			if err == nil && tt.validate != nil {
				tt.validate(t, info)
			}
		})
	}
}

func TestNewNamespaceQueueInfoClosesUnschedulableQueue(t *testing.T) {
	conditions := func(authorized, ready metav1.ConditionStatus, generation int64) []metav1.Condition {
		return []metav1.Condition{
			{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: authorized, ObservedGeneration: generation},
			{Type: commonutil.NamespaceQueueReadyCondition, Status: ready, ObservedGeneration: generation},
		}
	}

	tests := []struct {
		name       string
		state      scheduling.QueueState
		generation int64
		conditions []metav1.Condition
	}{
		{
			name:       "authorization failure",
			state:      scheduling.QueueStateOpen,
			generation: 1,
			conditions: conditions(metav1.ConditionFalse, metav1.ConditionTrue, 1),
		},
		{
			name:       "readiness failure",
			state:      scheduling.QueueStateOpen,
			generation: 1,
			conditions: conditions(metav1.ConditionTrue, metav1.ConditionFalse, 1),
		},
		{
			name:       "stale conditions",
			state:      scheduling.QueueStateOpen,
			generation: 2,
			conditions: conditions(metav1.ConditionTrue, metav1.ConditionTrue, 1),
		},
		{
			name:       "source queue is not open",
			state:      scheduling.QueueStateClosing,
			generation: 1,
			conditions: conditions(metav1.ConditionTrue, metav1.ConditionTrue, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := &scheduling.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "compute", Generation: tt.generation},
				Spec:       scheduling.NamespaceQueueSpec{Parent: "cluster/default"},
				Status: scheduling.NamespaceQueueStatus{
					State:      tt.state,
					Conditions: tt.conditions,
				},
			}
			info, err := NewNamespaceQueueInfo(queue)
			if err != nil {
				t.Fatalf("NewNamespaceQueueInfo() error = %v", err)
			}
			if got := info.Queue.Status.State; got != scheduling.QueueStateClosed {
				t.Fatalf("internal Queue state = %q, want Closed", got)
			}
		})
	}
}

func resourceList(cpu string) v1.ResourceList {
	return v1.ResourceList{v1.ResourceCPU: resource.MustParse(cpu)}
}
