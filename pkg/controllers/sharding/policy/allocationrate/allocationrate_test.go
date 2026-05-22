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

package allocationrate

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestAllocationRatePolicyInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name:      "valid configuration",
			args:      policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 0.6},
			expectErr: false,
		},
		{
			name:      "invalid CPU range - min > max",
			args:      policy.Arguments{ArgMinCPUUtil: 0.7, ArgMaxCPUUtil: 0.5},
			expectErr: true,
		},
		{
			name:      "invalid CPU range - negative min",
			args:      policy.Arguments{ArgMinCPUUtil: -0.1, ArgMaxCPUUtil: 0.5},
			expectErr: true,
		},
		{
			name:      "invalid CPU range - max > 1",
			args:      policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.5},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			err := p.Initialize(tt.args)
			if (err != nil) != tt.expectErr {
				t.Errorf("Initialize() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}

// newPolicy constructs and Initialize-s a policy with the given args.
func newPolicy(t *testing.T, args policy.Arguments) (policy.Filterer, policy.Scorer) {
	t.Helper()
	p := New()
	if err := p.Initialize(args); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	f, ok := p.(policy.Filterer)
	if !ok {
		t.Fatalf("policy does not implement Filterer")
	}
	s, ok := p.(policy.Scorer)
	if !ok {
		t.Fatalf("policy does not implement Scorer")
	}
	return f, s
}

func node(name string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
}

func TestAllocationRatePolicyFilter(t *testing.T) {
	tests := []struct {
		name    string
		args    policy.Arguments
		metrics *policy.NodeMetrics
		want    bool
	}{
		{
			name:    "in-range util passes",
			args:    policy.Arguments{ArgMinCPUUtil: 0.3, ArgMaxCPUUtil: 0.7},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.5},
			want:    true,
		},
		{
			name:    "below-range util rejected",
			args:    policy.Arguments{ArgMinCPUUtil: 0.3, ArgMaxCPUUtil: 0.7},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.2},
			want:    false,
		},
		{
			name:    "above-range util rejected",
			args:    policy.Arguments{ArgMinCPUUtil: 0.3, ArgMaxCPUUtil: 0.7},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.8},
			want:    false,
		},
		{
			name:    "boundary inclusive",
			args:    policy.Arguments{ArgMinCPUUtil: 0.3, ArgMaxCPUUtil: 0.7},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.30},
			want:    true,
		},
		{
			name:    "missing metrics rejected",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: nil,
			want:    false,
		},
		{
			name:    "negative util rejected",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: &policy.NodeMetrics{CPUUtilization: -1.0},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := newPolicy(t, tt.args)
			ctx := &policy.PolicyContext{
				NodeMetrics: map[string]*policy.NodeMetrics{"n": tt.metrics},
			}
			if got := f.Filter(ctx, node("n")); got != tt.want {
				t.Errorf("Filter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAllocationRatePolicyScore(t *testing.T) {
	tests := []struct {
		name    string
		args    policy.Arguments
		metrics *policy.NodeMetrics
		want    float64
	}{
		{
			name:    "full range: util passes through",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.5},
			want:    0.5,
		},
		{
			name:    "full range: max maps to 1.0",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: &policy.NodeMetrics{CPUUtilization: 1.0},
			want:    1.0,
		},
		{
			name:    "narrow range normalizes to [0,1]",
			args:    policy.Arguments{ArgMinCPUUtil: 0.4, ArgMaxCPUUtil: 0.6},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.5},
			want:    0.5,
		},
		{
			name:    "narrow range: min maps to 0",
			args:    policy.Arguments{ArgMinCPUUtil: 0.4, ArgMaxCPUUtil: 0.6},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.4},
			want:    0.0,
		},
		{
			name:    "narrow range: max maps to 1",
			args:    policy.Arguments{ArgMinCPUUtil: 0.4, ArgMaxCPUUtil: 0.6},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.6},
			want:    1.0,
		},
		{
			name:    "collapsed range returns 1",
			args:    policy.Arguments{ArgMinCPUUtil: 0.5, ArgMaxCPUUtil: 0.5},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.5},
			want:    1.0,
		},
		{
			name:    "util above max clamps to 1",
			args:    policy.Arguments{ArgMinCPUUtil: 0.4, ArgMaxCPUUtil: 0.6},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.7},
			want:    1.0,
		},
		{
			name:    "util below min clamps to 0",
			args:    policy.Arguments{ArgMinCPUUtil: 0.4, ArgMaxCPUUtil: 0.6},
			metrics: &policy.NodeMetrics{CPUUtilization: 0.3},
			want:    0.0,
		},
		{
			name:    "missing metrics returns 0",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: nil,
			want:    0.0,
		},
		{
			name:    "negative util returns 0",
			args:    policy.Arguments{ArgMinCPUUtil: 0.0, ArgMaxCPUUtil: 1.0},
			metrics: &policy.NodeMetrics{CPUUtilization: -0.5},
			want:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, s := newPolicy(t, tt.args)
			ctx := &policy.PolicyContext{
				NodeMetrics: map[string]*policy.NodeMetrics{"n": tt.metrics},
			}
			if got := s.Score(ctx, node("n")); got != tt.want {
				t.Errorf("Score = %v, want %v", got, tt.want)
			}
		})
	}
}
