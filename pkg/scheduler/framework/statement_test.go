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

package framework

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestStatementMerge(t *testing.T) {
	makeTask := func(name string) *api.TaskInfo {
		return &api.TaskInfo{
			Name: name,
		}
	}

	t.Run("merge transfers operations from source to target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
				{name: Pipeline, task: makeTask("t2")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 2 {
			t.Fatalf("expected 2 operations in target, got %d", len(target.operations))
		}
		if target.operations[0].task.Name != "t1" || target.operations[0].name != Evict {
			t.Errorf("first operation mismatch: got %v", target.operations[0])
		}
		if target.operations[1].task.Name != "t2" || target.operations[1].name != Pipeline {
			t.Errorf("second operation mismatch: got %v", target.operations[1])
		}
	})

	t.Run("source operations cleared after merge", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge(source)

		if source.operations != nil {
			t.Errorf("expected source operations to be nil after merge, got %v", source.operations)
		}
	})

	t.Run("merge multiple sources", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Allocate, task: makeTask("t0")},
			},
		}
		src1 := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		src2 := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t2")},
				{name: Evict, task: makeTask("t3"), reason: "preempt"},
			},
		}

		target.Merge(src1, src2)

		if len(target.operations) != 4 {
			t.Fatalf("expected 4 operations in target, got %d", len(target.operations))
		}
		// Verify order: existing target ops first, then src1, then src2
		expectedNames := []string{"t0", "t1", "t2", "t3"}
		for i, name := range expectedNames {
			if target.operations[i].task.Name != name {
				t.Errorf("operation %d: expected task %s, got %s", i, name, target.operations[i].task.Name)
			}
		}
		if src1.operations != nil {
			t.Errorf("expected src1 operations to be nil after merge")
		}
		if src2.operations != nil {
			t.Errorf("expected src2 operations to be nil after merge")
		}
	})

	t.Run("merge empty source is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}
		empty := &Statement{}

		target.Merge(empty)

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation in target after merging empty, got %d", len(target.operations))
		}
	})

	t.Run("merge into empty target", func(t *testing.T) {
		target := &Statement{}
		source := &Statement{
			operations: []operation{
				{name: Pipeline, task: makeTask("t1")},
			},
		}

		target.Merge(source)

		if len(target.operations) != 1 {
			t.Fatalf("expected 1 operation in target, got %d", len(target.operations))
		}
		if target.operations[0].task.Name != "t1" {
			t.Errorf("expected task t1, got %s", target.operations[0].task.Name)
		}
	})

	t.Run("merge with no arguments is no-op", func(t *testing.T) {
		target := &Statement{
			operations: []operation{
				{name: Evict, task: makeTask("t1"), reason: "preempt"},
			},
		}

		target.Merge()

		if len(target.operations) != 1 {
			t.Errorf("expected 1 operation after no-arg merge, got %d", len(target.operations))
		}
	})
}
