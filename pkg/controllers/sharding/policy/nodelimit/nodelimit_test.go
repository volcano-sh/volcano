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

package nodelimit

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestNodeLimitInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name:      "valid bounds",
			args:      policy.Arguments{"minNodes": 2, "maxNodes": 10},
			expectErr: false,
		},
		{
			name:      "valid bounds from YAML decoded numbers",
			args:      policy.Arguments{"minNodes": float64(2), "maxNodes": float64(10)},
			expectErr: false,
		},
		{
			name:      "max only",
			args:      policy.Arguments{"maxNodes": 5},
			expectErr: false,
		},
		{
			name:      "no bounds (no-op selector)",
			args:      policy.Arguments{},
			expectErr: false,
		},
		{
			name:      "negative minNodes rejected",
			args:      policy.Arguments{"minNodes": -1, "maxNodes": 5},
			expectErr: true,
		},
		{
			name:      "max < min rejected",
			args:      policy.Arguments{"minNodes": 10, "maxNodes": 5},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			if err := p.Initialize(tt.args); (err != nil) != tt.expectErr {
				t.Errorf("Initialize() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

func node(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func newSelector(t *testing.T, args policy.Arguments) policy.Selector {
	t.Helper()
	p := New()
	if err := p.Initialize(args); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	s, ok := p.(policy.Selector)
	if !ok {
		t.Fatalf("policy does not implement Selector")
	}
	return s
}

func names(nodes []*corev1.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}

func TestNodeLimitSelect(t *testing.T) {
	tests := []struct {
		name       string
		args       policy.Arguments
		input      []*corev1.Node
		expectKeep int
	}{
		{
			name:       "len < max keeps all",
			args:       policy.Arguments{"maxNodes": 5},
			input:      []*corev1.Node{node("a"), node("b"), node("c")},
			expectKeep: 3,
		},
		{
			name:       "len == max keeps all",
			args:       policy.Arguments{"maxNodes": 3},
			input:      []*corev1.Node{node("a"), node("b"), node("c")},
			expectKeep: 3,
		},
		{
			name:       "len > max truncates",
			args:       policy.Arguments{"maxNodes": 2},
			input:      []*corev1.Node{node("a"), node("b"), node("c"), node("d")},
			expectKeep: 2,
		},
		{
			name:       "YAML decoded max truncates",
			args:       policy.Arguments{"maxNodes": float64(2)},
			input:      []*corev1.Node{node("a"), node("b"), node("c"), node("d")},
			expectKeep: 2,
		},
		{
			name:       "max=0 means no upper bound",
			args:       policy.Arguments{},
			input:      []*corev1.Node{node("a"), node("b"), node("c")},
			expectKeep: 3,
		},
		{
			name:       "len < min logs but does not pad",
			args:       policy.Arguments{"minNodes": 5, "maxNodes": 10},
			input:      []*corev1.Node{node("a"), node("b")},
			expectKeep: 2,
		},
		{
			name:       "empty input stays empty",
			args:       policy.Arguments{"maxNodes": 5},
			input:      nil,
			expectKeep: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSelector(t, tt.args)
			got := s.Select(&policy.PolicyContext{SchedulerName: "test"}, tt.input)
			if len(got) != tt.expectKeep {
				t.Errorf("Select kept %d nodes, want %d. Got: %v", len(got), tt.expectKeep, names(got))
			}
		})
	}
}

func TestNodeLimitTruncatesPrefix(t *testing.T) {
	// Confirm Select returns the *prefix* of the input (preserves order),
	// not some random subset.
	s := newSelector(t, policy.Arguments{"maxNodes": 3})
	in := []*corev1.Node{node("a"), node("b"), node("c"), node("d"), node("e")}
	got := s.Select(&policy.PolicyContext{SchedulerName: "test"}, in)

	want := []string{"a", "b", "c"}
	gotNames := names(got)
	if len(gotNames) != len(want) {
		t.Fatalf("expected %d nodes, got %d: %v", len(want), len(gotNames), gotNames)
	}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, gotNames[i], want[i])
		}
	}
}
