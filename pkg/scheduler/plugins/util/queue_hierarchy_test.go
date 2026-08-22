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
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestBuildEffectiveQueueHierarchy(t *testing.T) {
	queues := map[api.QueueID]*api.QueueInfo{
		"root": {
			UID:   "root",
			Name:  "root",
			Scope: api.ClusterQueueScope,
		},
		"research": {
			UID:    "research",
			Name:   "research",
			Scope:  api.ClusterQueueScope,
			Parent: "root",
		},
		"team-a/department": {
			UID:       "team-a/department",
			Name:      "department",
			Scope:     api.NamespaceQueueScope,
			Namespace: "team-a",
			Parent:    "research",
			Weight:    api.DefaultQueueWeight,
		},
		"team-a/training": {
			UID:       "team-a/training",
			Name:      "training",
			Scope:     api.NamespaceQueueScope,
			Namespace: "team-a",
			Parent:    "team-a/department",
			Weight:    api.DefaultQueueWeight,
		},
	}

	if err := BuildEffectiveQueueHierarchy(queues); err != nil {
		t.Fatalf("BuildEffectiveQueueHierarchy() error = %v", err)
	}

	if got := queues["team-a/department"].Hierarchy; got != "root/research/team-a%2Fdepartment" {
		t.Fatalf("department hierarchy = %q", got)
	}
	if got := queues["team-a/training"].Hierarchy; got != "root/research/team-a%2Fdepartment/team-a%2Ftraining" {
		t.Fatalf("training hierarchy = %q", got)
	}
	if got := queues["team-a/training"].Weights; got != "1/1/1/1" {
		t.Fatalf("training weights = %q", got)
	}
}

func TestBuildEffectiveQueueHierarchyPreservesClusterAnnotations(t *testing.T) {
	queues := map[api.QueueID]*api.QueueInfo{
		"research": {
			UID:       "research",
			Name:      "research",
			Scope:     api.ClusterQueueScope,
			Hierarchy: "root/custom",
			Weights:   "1/25",
		},
		"team-a/training": {
			UID:    "team-a/training",
			Name:   "training",
			Scope:  api.NamespaceQueueScope,
			Parent: "research",
		},
	}

	if err := BuildEffectiveQueueHierarchy(queues); err != nil {
		t.Fatalf("BuildEffectiveQueueHierarchy() error = %v", err)
	}
	if queues["research"].Hierarchy != "root/custom" || queues["research"].Weights != "1/25" {
		t.Fatal("cluster Queue hierarchy annotations were modified")
	}
	if queues["team-a/training"].Hierarchy != "root/custom/team-a%2Ftraining" {
		t.Fatalf("unexpected NamespaceQueue hierarchy: %q", queues["team-a/training"].Hierarchy)
	}
}

func TestBuildEffectiveQueueHierarchyRejectsCycle(t *testing.T) {
	queues := map[api.QueueID]*api.QueueInfo{
		"a": {UID: "a", Name: "a", Scope: api.NamespaceQueueScope, Parent: "b"},
		"b": {UID: "b", Name: "b", Scope: api.NamespaceQueueScope, Parent: "a"},
	}

	if err := BuildEffectiveQueueHierarchy(queues); err == nil {
		t.Fatal("BuildEffectiveQueueHierarchy() succeeded for cyclic hierarchy")
	}
}

func TestBuildEffectiveQueueHierarchyRejectsMissingParent(t *testing.T) {
	queues := map[api.QueueID]*api.QueueInfo{
		"team-a/training": {
			UID:    "team-a/training",
			Name:   "training",
			Scope:  api.NamespaceQueueScope,
			Parent: "missing",
		},
	}

	if err := BuildEffectiveQueueHierarchy(queues); err == nil {
		t.Fatal("BuildEffectiveQueueHierarchy() succeeded with missing parent")
	}
}
