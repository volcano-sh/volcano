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

package util

import (
	"errors"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestValidateNamespaceQueueParentChange(t *testing.T) {
	tests := []struct {
		name      string
		oldQueue  *schedulingv1beta1.NamespaceQueue
		newParent string
		wantErr   bool
	}{
		{
			name:      "unchanged effective parent",
			oldQueue:  &schedulingv1beta1.NamespaceQueue{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"}, Spec: schedulingv1beta1.NamespaceQueueSpec{Parent: ""}},
			newParent: "cluster/default",
		},
		{
			name:      "active queue cannot change parent",
			oldQueue:  &schedulingv1beta1.NamespaceQueue{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"}, Spec: schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"}, Status: schedulingv1beta1.NamespaceQueueStatus{State: schedulingv1beta1.QueueStateOpen}},
			newParent: "cluster/production",
			wantErr:   true,
		},
		{
			name: "closed and drained queue can change parent",
			oldQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
				Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
				Status:     schedulingv1beta1.NamespaceQueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			newParent: "cluster/production",
		},
		{
			name: "closed queue with runtime usage cannot change parent",
			oldQueue: &schedulingv1beta1.NamespaceQueue{
				ObjectMeta: metav1.ObjectMeta{Namespace: "team-a"},
				Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
				Status: schedulingv1beta1.NamespaceQueueStatus{
					State:     schedulingv1beta1.QueueStateClosed,
					Allocated: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
				},
			},
			newParent: "cluster/production",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newQueue := tt.oldQueue.DeepCopy()
			newQueue.Spec.Parent = tt.newParent
			err := ValidateNamespaceQueueParentChange(tt.oldQueue, newQueue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateNamespaceQueueParentChange() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestNamespaceQueueDepth(t *testing.T) {
	parentLookupErr := errors.New("parent lookup failed")
	tests := []struct {
		name      string
		queue     *schedulingv1beta1.NamespaceQueue
		parents   map[string]*schedulingv1beta1.NamespaceQueue
		lookupErr error
		wantDepth int
		wantErr   error
	}{
		{
			name:      "cluster parent is depth one",
			queue:     namespaceQueueForDepthTest("team-a", "department", "cluster/research"),
			wantDepth: 1,
		},
		{
			name:  "local parents increase depth",
			queue: namespaceQueueForDepthTest("team-a", "training", "platform"),
			parents: map[string]*schedulingv1beta1.NamespaceQueue{
				"team-a/platform":   namespaceQueueForDepthTest("team-a", "platform", "department"),
				"team-a/department": namespaceQueueForDepthTest("team-a", "department", "cluster/research"),
			},
			wantDepth: 3,
		},
		{
			name:  "cycle is rejected",
			queue: namespaceQueueForDepthTest("team-a", "a", "b"),
			parents: map[string]*schedulingv1beta1.NamespaceQueue{
				"team-a/b": namespaceQueueForDepthTest("team-a", "b", "a"),
			},
			wantErr: ErrNamespaceQueueHierarchyCycle,
		},
		{
			name:      "parent lookup error is returned",
			queue:     namespaceQueueForDepthTest("team-a", "training", "platform"),
			lookupErr: parentLookupErr,
			wantErr:   parentLookupErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depth, err := NamespaceQueueDepth(
				tt.queue,
				func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error) {
					if tt.lookupErr != nil {
						return nil, tt.lookupErr
					}
					return tt.parents[namespace+"/"+name], nil
				},
			)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NamespaceQueueDepth() error = %v, want %v", err, tt.wantErr)
			}
			if err == nil && depth != tt.wantDepth {
				t.Fatalf("NamespaceQueueDepth() = %d, want %d", depth, tt.wantDepth)
			}
		})
	}
}

func namespaceQueueForDepthTest(namespace, name, parent string) *schedulingv1beta1.NamespaceQueue {
	return &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: parent},
	}
}

func TestEffectiveNamespaceQueueState(t *testing.T) {
	if got := EffectiveNamespaceQueueState(""); got != schedulingv1beta1.QueueStateOpen {
		t.Fatalf("empty state resolved to %q, want Open", got)
	}
	if got := EffectiveNamespaceQueueState(schedulingv1beta1.QueueStateClosed); got != schedulingv1beta1.QueueStateClosed {
		t.Fatalf("Closed state resolved to %q, want Closed", got)
	}
}

func TestIsNamespaceQueueDrained(t *testing.T) {
	tests := []struct {
		name   string
		status schedulingv1beta1.NamespaceQueueStatus
		want   bool
	}{
		{name: "empty", want: true},
		{name: "completed does not block", status: schedulingv1beta1.NamespaceQueueStatus{Completed: 1}, want: true},
		{name: "pending blocks", status: schedulingv1beta1.NamespaceQueueStatus{Pending: 1}},
		{name: "running blocks", status: schedulingv1beta1.NamespaceQueueStatus{Running: 1}},
		{name: "unknown blocks", status: schedulingv1beta1.NamespaceQueueStatus{Unknown: 1}},
		{name: "inqueue blocks", status: schedulingv1beta1.NamespaceQueueStatus{Inqueue: 1}},
		{
			name: "allocated resource blocks",
			status: schedulingv1beta1.NamespaceQueueStatus{
				Allocated: v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
			},
		},
		{
			name: "reservation node blocks",
			status: schedulingv1beta1.NamespaceQueueStatus{
				Reservation: schedulingv1beta1.Reservation{Nodes: []string{"node-1"}},
			},
		},
		{
			name: "reservation resource blocks",
			status: schedulingv1beta1.NamespaceQueueStatus{
				Reservation: schedulingv1beta1.Reservation{
					Resource: v1.ResourceList{v1.ResourceMemory: resource.MustParse("1Gi")},
				},
			},
		},
		{
			name: "zero runtime resources do not block",
			status: schedulingv1beta1.NamespaceQueueStatus{
				Allocated: v1.ResourceList{v1.ResourceCPU: resource.MustParse("0")},
				Reservation: schedulingv1beta1.Reservation{
					Resource: v1.ResourceList{v1.ResourceMemory: resource.MustParse("0")},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNamespaceQueueDrained(tt.status); got != tt.want {
				t.Fatalf("IsNamespaceQueueDrained() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestResolveNamespaceQueueLifecycleState(t *testing.T) {
	tests := []struct {
		name            string
		desired         schedulingv1beta1.QueueState
		workloadDrained bool
		runtimeDrained  bool
		want            schedulingv1beta1.QueueState
	}{
		{name: "open", desired: schedulingv1beta1.QueueStateOpen, want: schedulingv1beta1.QueueStateOpen},
		{
			name:            "closed and drained",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: true,
			runtimeDrained:  true,
			want:            schedulingv1beta1.QueueStateClosed,
		},
		{
			name:            "closed with active workload",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: false,
			runtimeDrained:  true,
			want:            schedulingv1beta1.QueueStateClosing,
		},
		{
			name:            "closed with runtime resource",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: true,
			runtimeDrained:  false,
			want:            schedulingv1beta1.QueueStateClosing,
		},
		{name: "invalid desired state", desired: schedulingv1beta1.QueueStateClosing, want: schedulingv1beta1.QueueStateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveNamespaceQueueLifecycleState(
				tt.desired,
				tt.workloadDrained,
				tt.runtimeDrained,
			)
			if got != tt.want {
				t.Fatalf("ResolveNamespaceQueueLifecycleState() = %q, want %q", got, tt.want)
			}
		})
	}
}
