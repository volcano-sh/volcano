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
	"reflect"
	"testing"
)

// These tests cover the pure ordering/lookup helpers used by deleteQueues to
// tear down hierarchical queues. They do not exercise any apiserver paths and
// must remain runnable with `go test ./test/e2e/util/...` (no cluster needed).

func TestTopLevelQueues(t *testing.T) {
	tests := []struct {
		name        string
		queues      []string
		queueParent map[string]string
		want        []string
	}{
		{
			name:   "empty context",
			queues: nil,
			want:   []string{},
		},
		{
			name:   "all top-level (no parent declared)",
			queues: []string{"q1", "q2"},
			want:   []string{"q1", "q2"},
		},
		{
			name:        "parent root is top-level",
			queues:      []string{"q1", "q2"},
			queueParent: map[string]string{"q1": "root", "q2": "root"},
			want:        []string{"q1", "q2"},
		},
		{
			name:        "child queues are filtered out",
			queues:      []string{"q1", "q2", "q11", "q21"},
			queueParent: map[string]string{"q1": "root", "q2": "root", "q11": "q1", "q21": "q2"},
			want:        []string{"q1", "q2"},
		},
		{
			name:        "ordering follows ctx.Queues",
			queues:      []string{"q11", "q1", "q2", "q21"},
			queueParent: map[string]string{"q1": "root", "q2": "root", "q11": "q1", "q21": "q2"},
			want:        []string{"q1", "q2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TestContext{Queues: tt.queues, QueueParent: tt.queueParent}
			got := topLevelQueues(ctx)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("topLevelQueues = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueueDepth(t *testing.T) {
	tests := []struct {
		name        string
		queues      []string
		queueParent map[string]string
		queue       string
		want        int
	}{
		{
			name:  "no parent map",
			queue: "q1",
			want:  0,
		},
		{
			name:        "parent empty string",
			queueParent: map[string]string{"q1": ""},
			queue:       "q1",
			want:        0,
		},
		{
			name:        "parent root",
			queueParent: map[string]string{"q1": "root"},
			queue:       "q1",
			want:        0,
		},
		{
			name:        "direct child of root",
			queueParent: map[string]string{"q1": "root", "q11": "q1"},
			queue:       "q11",
			want:        1,
		},
		{
			name:        "three levels deep",
			queueParent: map[string]string{"a": "root", "b": "a", "c": "b"},
			queue:       "c",
			want:        2,
		},
		{
			name:        "untracked parent is treated as root",
			queueParent: map[string]string{"orphan": "ghost-parent"},
			queue:       "orphan",
			want:        1,
		},
		{
			name:        "cycle returns bounded depth without hanging",
			queues:      []string{"a", "b"},
			queueParent: map[string]string{"a": "b", "b": "a"},
			queue:       "a",
			want:        3, // len(queues)+1 = 3 iterations, depth incremented each time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TestContext{Queues: tt.queues, QueueParent: tt.queueParent}
			got := queueDepth(ctx, tt.queue)
			if got != tt.want {
				t.Errorf("queueDepth(%q) = %d, want %d", tt.queue, got, tt.want)
			}
		})
	}
}

func TestQueuesDeepestFirst(t *testing.T) {
	tests := []struct {
		name        string
		queues      []string
		queueParent map[string]string
		want        []string
	}{
		{
			name:   "empty",
			queues: nil,
			want:   []string{},
		},
		{
			name:   "single queue",
			queues: []string{"q1"},
			want:   []string{"q1"},
		},
		{
			name:        "two-level hierarchy",
			queues:      []string{"q1", "q2", "q11"},
			queueParent: map[string]string{"q1": "root", "q2": "root", "q11": "q1"},
			want:        []string{"q11", "q1", "q2"},
		},
		{
			name:        "three-level hierarchy",
			queues:      []string{"a", "b", "c"},
			queueParent: map[string]string{"a": "root", "b": "a", "c": "b"},
			want:        []string{"c", "b", "a"},
		},
		{
			name:        "stable across equal depths",
			queues:      []string{"q1", "q2", "q3"},
			queueParent: map[string]string{"q1": "root", "q2": "root", "q3": "root"},
			want:        []string{"q1", "q2", "q3"}, // declaration order preserved
		},
		{
			name:        "interleaved depths",
			queues:      []string{"q1", "q11", "q2", "q21", "q22"},
			queueParent: map[string]string{"q1": "root", "q2": "root", "q11": "q1", "q21": "q2", "q22": "q2"},
			want:        []string{"q11", "q21", "q22", "q1", "q2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &TestContext{Queues: tt.queues, QueueParent: tt.queueParent}
			got := queuesDeepestFirst(ctx)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("queuesDeepestFirst = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestQueuesDeepestFirstDoesNotMutateContext makes sure the sort produces a
// fresh slice so call sites can iterate the result without affecting future
// reads of ctx.Queues.
func TestQueuesDeepestFirstDoesNotMutateContext(t *testing.T) {
	original := []string{"q1", "q2", "q11"}
	ctx := &TestContext{
		Queues:      original,
		QueueParent: map[string]string{"q1": "root", "q2": "root", "q11": "q1"},
	}

	_ = queuesDeepestFirst(ctx)

	if !reflect.DeepEqual(ctx.Queues, original) {
		t.Errorf("ctx.Queues was mutated by queuesDeepestFirst: got %v, want %v", ctx.Queues, original)
	}
}
