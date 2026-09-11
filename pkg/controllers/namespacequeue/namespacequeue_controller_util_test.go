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

package namespacequeue

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	controllerutil "volcano.sh/volcano/pkg/util"
)

func TestResolveParent(t *testing.T) {
	tests := []struct {
		name      string
		nq        *schedulingv1beta1.NamespaceQueue
		want      controllerutil.ResolvedQueueReference
		wantError bool
	}{
		{
			name:      "nil namespace queue",
			wantError: true,
		},
		{
			name:      "empty parent is invalid",
			nq:        newNamespaceQueue("team-a", ""),
			wantError: true,
		},
		{
			name: "cluster parent",
			nq:   newNamespaceQueue("team-a", "cluster/research"),
			want: controllerutil.ResolvedQueueReference{
				Scope: controllerutil.ClusterQueueReferenceScope,
				Name:  "research",
			},
		},
		{
			name: "namespace parent",
			nq:   newNamespaceQueue("team-a", "department"),
			want: controllerutil.ResolvedQueueReference{
				Scope:     controllerutil.NamespaceQueueReferenceScope,
				Namespace: "team-a",
				Name:      "department",
			},
		},
		{
			name:      "cluster root is forbidden",
			nq:        newNamespaceQueue("team-a", "cluster/root"),
			wantError: true,
		},
		{
			name:      "cluster parent name is empty",
			nq:        newNamespaceQueue("team-a", "cluster/"),
			wantError: true,
		},
		{
			name:      "cluster parent name is invalid",
			nq:        newNamespaceQueue("team-a", "cluster/Research"),
			wantError: true,
		},
		{
			name:      "namespace parent reference is invalid",
			nq:        newNamespaceQueue("team-a", "department/child"),
			wantError: true,
		},
		{
			name:      "namespace parent requires namespace",
			nq:        newNamespaceQueue("", "department"),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveParent(tt.nq)
			if (err != nil) != tt.wantError {
				t.Fatalf("resolveParent() error = %v, wantError %v", err, tt.wantError)
			}

			if tt.wantError {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("resolveParent() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestQueueReferenceKey(t *testing.T) {
	tests := []struct {
		name      string
		reference controllerutil.ResolvedQueueReference
		want      string
	}{
		{
			name: "cluster queue",
			reference: controllerutil.ResolvedQueueReference{
				Scope: controllerutil.ClusterQueueReferenceScope,
				Name:  "default",
			},
			want: "cluster/default",
		},
		{
			name: "namespace queue",
			reference: controllerutil.ResolvedQueueReference{
				Scope:     controllerutil.NamespaceQueueReferenceScope,
				Namespace: "team-a",
				Name:      "training",
			},
			want: "namespace/team-a/training",
		},
		{name: "empty reference", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queueReferenceKey(tt.reference); got != tt.want {
				t.Fatalf("queueReferenceKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func newNamespaceQueue(namespace, parent string) *schedulingv1beta1.NamespaceQueue {
	return &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace},
		Spec: schedulingv1beta1.NamespaceQueueSpec{
			Parent: parent,
		},
	}
}
