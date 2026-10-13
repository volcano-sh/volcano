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
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
)

func newTestHyperNode(name string, tier int, tierName, parent string) *HyperNodeInfo {
	return &HyperNodeInfo{
		Name:     name,
		tier:     tier,
		tierName: tierName,
		Parent:   parent,
		Children: sets.New[string](),
	}
}

func TestHyperNodeIndexPlacement(t *testing.T) {
	hyperNodes := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"a":    newTestHyperNode("a", 2, "rack", "root"),
		"b":    newTestHyperNode("b", 2, "rack", "root"),
		"c":    newTestHyperNode("c", 2, "rack", "root"),
		"leaf": newTestHyperNode("leaf", 1, "leaf", "a"),
	}
	nodes := map[string]sets.Set[string]{
		"root": sets.New("n1", "n2", "n3"),
		"a":    sets.New("n1"), "b": sets.New("n2"), "c": sets.New("n3"), "leaf": sets.New("n1"),
		"deleted": sets.New("n1"),
	}
	index := NewHyperNodeIndex(hyperNodes, nodes)
	if index.NodeToHyperNode["n1"] != "leaf" {
		t.Fatalf("expected finest domain, got %v", index.NodeToHyperNode)
	}
	first := &TaskInfo{UID: "first", TransactionContext: TransactionContext{Status: Running, NodeName: "n1"}}
	second := &TaskInfo{UID: "second", TransactionContext: TransactionContext{Status: Running, NodeName: "n2"}}
	job := &JobInfo{AllocatedHyperNode: "root", TaskStatusIndex: map[TaskStatus]TasksMap{Running: {first.UID: first, second.UID: second}}}
	if got := index.JobAllocatedHyperNode(job); got != "root" {
		t.Fatalf("expected common ancestor, got %q", got)
	}
	if got := index.OccupiedHyperNodes(job, 2); !got.Equal(sets.New("a", "b")) {
		t.Fatalf("must not expand LCA to unoccupied sibling c: %v", got)
	}
	// Same index must observe placement changes and rollback, without caching occupancy.
	second.NodeName = "n1"
	if got := index.OccupiedHyperNodes(job, 2); !got.Equal(sets.New("a")) {
		t.Fatalf("stale occupancy after move: %v", got)
	}
	second.NodeName = "n2"
	if got := index.OccupiedHyperNodes(job, 2); !got.Equal(sets.New("a", "b")) {
		t.Fatalf("stale occupancy after rollback: %v", got)
	}
	second.NodeName = "missing"
	if got := index.JobAllocatedHyperNode(job); got != "" {
		t.Fatalf("incomplete placement must not narrow to mapped tasks: %q", got)
	}
}

func TestHyperNodeIndexOccupancyFallback(t *testing.T) {
	hyperNodes := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 2, "cluster", ""),
		"a":    newTestHyperNode("a", 1, "rack", "root"),
		"b":    newTestHyperNode("b", 1, "rack", "root"),
	}
	hyperNodes["root"].Children = sets.New("a", "b")
	for _, tc := range []struct {
		name string
		job  *JobInfo
		want sets.Set[string]
	}{
		{name: "nil job", want: sets.New[string]()},
		{name: "unplaced job", job: &JobInfo{}, want: sets.New[string]()},
		{name: "recorded job", job: &JobInfo{AllocatedHyperNode: "a"}, want: sets.New("a")},
		{name: "coarse recorded job", job: &JobInfo{AllocatedHyperNode: "root"}, want: sets.New("a", "b")},
		{name: "recorded subjob", job: &JobInfo{SubJobs: map[SubJobID]*SubJobInfo{"sub": {AllocatedHyperNode: "b"}}}, want: sets.New("b")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewHyperNodeIndex(hyperNodes, nil).OccupiedHyperNodes(tc.job, 1); !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func BenchmarkHyperNodeIndexedOccupancy(b *testing.B) {
	hyperNodes := HyperNodeInfoMap{"root": newTestHyperNode("root", 2, "cluster", "")}
	nodes := map[string]sets.Set[string]{"root": sets.New[string]()}
	job := &JobInfo{TaskStatusIndex: map[TaskStatus]TasksMap{Running: {}}}
	for i := 0; i < 1000; i++ {
		name, node := fmt.Sprintf("rack-%d", i), fmt.Sprintf("node-%d", i)
		hyperNodes[name] = newTestHyperNode(name, 1, "rack", "root")
		nodes[name] = sets.New(node)
		nodes["root"].Insert(node)
		task := &TaskInfo{UID: TaskID(node), TransactionContext: TransactionContext{Status: Running, NodeName: node}}
		job.TaskStatusIndex[Running][task.UID] = task
	}
	index := NewHyperNodeIndex(hyperNodes, nodes)
	b.Run("session-index", func(b *testing.B) {
		for b.Loop() {
			index.OccupiedHyperNodes(job, 1)
		}
	})
}

func TestHyperNodeIndexPreservesOverlappingMembership(t *testing.T) {
	hyperNodes := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 2, "cluster", ""),
		"a":    newTestHyperNode("a", 1, "rack", "root"),
		"b":    newTestHyperNode("b", 1, "rack", "root"),
	}
	nodes := map[string]sets.Set[string]{
		"root": sets.New("n1", "n2"), "a": sets.New("n1"), "b": sets.New("n1", "n2"),
	}
	index := NewHyperNodeIndex(hyperNodes, nodes)
	job := &JobInfo{TaskStatusIndex: map[TaskStatus]TasksMap{Running: {
		"first":  {UID: "first", TransactionContext: TransactionContext{Status: Running, NodeName: "n1"}},
		"second": {UID: "second", TransactionContext: TransactionContext{Status: Running, NodeName: "n2"}},
	}}}
	if got := index.JobAllocatedHyperNode(job); got != "b" {
		t.Fatalf("common membership must not be widened to root: %s", got)
	}
	for _, tt := range []struct {
		nodes sets.Set[string]
		want  sets.Set[string]
	}{
		{sets.New("n1"), sets.New("a", "b", "root")},
		{sets.New("n1", "n2"), sets.New("b", "root")},
		{sets.New("missing"), sets.New[string]()},
	} {
		got := sets.New[string]()
		for _, hn := range index.DomainsContaining(tt.nodes) {
			got.Insert(hn.Name)
		}
		if !got.Equal(tt.want) {
			t.Fatalf("domains containing %v: got %v, want %v", tt.nodes, got, tt.want)
		}
	}
}
