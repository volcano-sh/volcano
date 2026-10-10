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

import "testing"

func TestResolveWorkloadQueueReference(t *testing.T) {
	tests := []struct {
		name              string
		workloadNamespace string
		reference         string
		defaultQueue      string
		want              ResolvedQueueReference
		wantErr           bool
	}{
		{
			name:         "empty uses default cluster queue",
			defaultQueue: "default",
			want:         ResolvedQueueReference{Scope: ClusterQueueReferenceScope, Name: "default"},
		},
		{
			name:      "plain name is cluster queue",
			reference: "research",
			want:      ResolvedQueueReference{Scope: ClusterQueueReferenceScope, Name: "research"},
		},
		{
			name:              "namespace queue",
			workloadNamespace: "team-a",
			reference:         "namespace/training",
			want: ResolvedQueueReference{
				Scope: NamespaceQueueReferenceScope, Namespace: "team-a", Name: "training",
			},
		},
		{
			name:      "empty default is invalid",
			wantErr:   true,
			reference: "",
		},
		{
			name:      "missing namespace",
			reference: "namespace/training",
			wantErr:   true,
		},
		{
			name:              "nested name is invalid",
			workloadNamespace: "team-a",
			reference:         "namespace/department/training",
			wantErr:           true,
		},
		{
			name:              "unsupported scope is invalid",
			workloadNamespace: "team-a",
			reference:         "cluster/research",
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveWorkloadQueueReference(tt.workloadNamespace, tt.reference, tt.defaultQueue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveWorkloadQueueReference() error = %v, wantErr = %t", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ResolveWorkloadQueueReference() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestResolveNamespaceQueueParentReference(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		reference string
		want      ResolvedQueueReference
		wantErr   bool
	}{
		{
			name:      "empty parent is invalid",
			namespace: "team-a",
			wantErr:   true,
		},
		{
			name:      "cluster parent",
			namespace: "team-a",
			reference: "cluster/research",
			want:      ResolvedQueueReference{Scope: ClusterQueueReferenceScope, Name: "research"},
		},
		{
			name:      "local parent",
			namespace: "team-a",
			reference: "department",
			want:      ResolvedQueueReference{Scope: NamespaceQueueReferenceScope, Namespace: "team-a", Name: "department"},
		},
		{
			name:      "cluster root is invalid",
			namespace: "team-a",
			reference: "cluster/root",
			wantErr:   true,
		},
		{
			name:      "missing namespace is invalid for local parent",
			reference: "department",
			wantErr:   true,
		},
		{
			name:      "invalid name",
			namespace: "team-a",
			reference: "Department",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveNamespaceQueueParentReference(tt.namespace, tt.reference)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveNamespaceQueueParentReference() error = %v, wantErr = %t", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ResolveNamespaceQueueParentReference() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
