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

package router

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

func TestResolveQueueReference(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	tests := []struct {
		name              string
		workloadNamespace string
		reference         string
		defaultQueue      string
		want              ResolvedQueueReference
		wantErr           bool
	}{
		{
			name:         "empty reference uses default cluster queue",
			defaultQueue: "default",
			want: ResolvedQueueReference{
				Scope: ClusterQueueReferenceScope,
				Name:  "default",
			},
		},
		{
			name:      "plain name selects cluster queue",
			reference: "research",
			want: ResolvedQueueReference{
				Scope: ClusterQueueReferenceScope,
				Name:  "research",
			},
		},
		{
			name:              "namespace reference selects local namespace queue",
			workloadNamespace: "team-a",
			reference:         "namespace/training",
			want: ResolvedQueueReference{
				Scope:     NamespaceQueueReferenceScope,
				Namespace: "team-a",
				Name:      "training",
			},
		},
		{
			name:    "empty reference requires default queue",
			wantErr: true,
		},
		{
			name:              "cluster prefix is invalid for workload reference",
			workloadNamespace: "team-a",
			reference:         "cluster/research",
			wantErr:           true,
		},
		{
			name:              "namespace queue name is required",
			workloadNamespace: "team-a",
			reference:         "namespace/",
			wantErr:           true,
		},
		{
			name:              "cross namespace reference is invalid",
			workloadNamespace: "team-a",
			reference:         "team-b/training",
			wantErr:           true,
		},
		{
			name:      "namespace queue requires workload namespace",
			reference: "namespace/training",
			wantErr:   true,
		},
		{
			name:              "nested namespace queue reference is invalid",
			workloadNamespace: "team-a",
			reference:         "namespace/department/training",
			wantErr:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveQueueReference(tt.workloadNamespace, tt.reference, tt.defaultQueue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ResolveQueueReference() error = %v, wantErr = %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("ResolveQueueReference() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveQueueReferenceRejectsNamespaceQueueWhenDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	if _, err := ResolveQueueReference("team-a", "namespace/training", "default"); err == nil {
		t.Fatal("ResolveQueueReference() accepted NamespaceQueue while feature gate is disabled")
	}
}

func TestNamespaceQueueParentIndexFunc(t *testing.T) {
	queue := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "training"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
	}
	keys, err := NamespaceQueueParentIndexFunc(queue)
	if err != nil {
		t.Fatalf("NamespaceQueueParentIndexFunc() error = %v", err)
	}
	if len(keys) != 1 || keys[0] != "cluster/research" {
		t.Fatalf("index keys = %v", keys)
	}
}
