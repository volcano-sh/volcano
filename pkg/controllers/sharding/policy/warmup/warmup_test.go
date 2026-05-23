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

package warmup

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

func TestWarmupPolicyInitialize(t *testing.T) {
	tests := []struct {
		name      string
		args      policy.Arguments
		expectErr bool
	}{
		{
			name: "valid configuration",
			args: policy.Arguments{
				"warmupLabel":      "node.volcano.sh/warmup",
				"warmupLabelValue": "true",
			},
			expectErr: false,
		},
		{
			name:      "empty warmup label rejected",
			args:      policy.Arguments{"warmupLabel": ""},
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

func TestWarmupPolicyScore(t *testing.T) {
	tests := []struct {
		name   string
		args   policy.Arguments
		labels map[string]string
		want   float64
	}{
		{
			name:   "default label, present",
			args:   policy.Arguments{},
			labels: map[string]string{"node.volcano.sh/warmup": "true"},
			want:   1.0,
		},
		{
			name:   "default label, absent",
			args:   policy.Arguments{},
			labels: map[string]string{},
			want:   0.0,
		},
		{
			name:   "default label, wrong value",
			args:   policy.Arguments{},
			labels: map[string]string{"node.volcano.sh/warmup": "false"},
			want:   0.0,
		},
		{
			name: "custom label and value, match",
			args: policy.Arguments{
				"warmupLabel":      "node.example.com/pool",
				"warmupLabelValue": "warm",
			},
			labels: map[string]string{"node.example.com/pool": "warm"},
			want:   1.0,
		},
		{
			name: "custom label and value, mismatch",
			args: policy.Arguments{
				"warmupLabel":      "node.example.com/pool",
				"warmupLabelValue": "warm",
			},
			labels: map[string]string{"node.example.com/pool": "cold"},
			want:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New()
			if err := p.Initialize(tt.args); err != nil {
				t.Fatalf("Initialize() error = %v", err)
			}
			s, ok := p.(policy.Scorer)
			if !ok {
				t.Fatalf("policy does not implement Scorer")
			}
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "n", Labels: tt.labels},
			}
			if got := s.Score(&policy.PolicyContext{}, node); got != tt.want {
				t.Errorf("Score = %v, want %v", got, tt.want)
			}
		})
	}
}
