/*
Copyright 2024 The Volcano Authors.

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

package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/agent/apis"
)

func TestShouldUpdateNodeOverSubscription(t *testing.T) {
	tests := []struct {
		name    string
		current apis.Resource
		new     apis.Resource
		want    bool
	}{
		{
			name:    "current is zero and new is zero: no update, first publish handled without NaN",
			current: apis.Resource{corev1.ResourceCPU: 0, corev1.ResourceMemory: 0},
			new:     apis.Resource{corev1.ResourceCPU: 0, corev1.ResourceMemory: 0},
			want:    false,
		},
		{
			name:    "current is zero and new is non-zero: update once, without dividing by zero",
			current: apis.Resource{corev1.ResourceCPU: 0, corev1.ResourceMemory: 0},
			new:     apis.Resource{corev1.ResourceCPU: 1000, corev1.ResourceMemory: 0},
			want:    true,
		},
		{
			name:    "current non-zero, delta below the change step: no update",
			current: apis.Resource{corev1.ResourceCPU: 1000, corev1.ResourceMemory: 1000},
			new:     apis.Resource{corev1.ResourceCPU: 1050, corev1.ResourceMemory: 1000},
			want:    false,
		},
		{
			name:    "current non-zero, delta above the change step: update",
			current: apis.Resource{corev1.ResourceCPU: 1000, corev1.ResourceMemory: 1000},
			new:     apis.Resource{corev1.ResourceCPU: 1200, corev1.ResourceMemory: 1000},
			want:    true,
		},
		{
			name:    "one resource zero and unchanged, other resource changed above step: update",
			current: apis.Resource{corev1.ResourceCPU: 0, corev1.ResourceMemory: 1000},
			new:     apis.Resource{corev1.ResourceCPU: 0, corev1.ResourceMemory: 1200},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ShouldUpdateNodeOverSubscription(tt.current, tt.new))
		})
	}
}
