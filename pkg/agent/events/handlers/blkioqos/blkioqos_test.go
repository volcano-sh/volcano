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

package blkioqos

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/agent/apis"
)

// TestParseBlkioWeight runs on all platforms (no _linux). Handle/cgroup tests are in blkioqos_linux_test.go.
func TestParseBlkioWeight(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected int64
	}{
		{name: "no pod", pod: nil, expected: 0},
		{
			name:     "no annotations",
			pod:      &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "ns"}},
			expected: 0,
		},
		{
			name: "valid weight",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p", Namespace: "ns",
					Annotations: map[string]string{apis.BlkioWeightAnnotationKey: "500"},
				},
			},
			expected: 500,
		},
		{
			name: "invalid non-numeric",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p", Namespace: "ns",
					Annotations: map[string]string{apis.BlkioWeightAnnotationKey: "bad"},
				},
			},
			expected: 0,
		},
		{
			name: "invalid zero",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p", Namespace: "ns",
					Annotations: map[string]string{apis.BlkioWeightAnnotationKey: "0"},
				},
			},
			expected: 0,
		},
		{
			name: "invalid negative",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "p", Namespace: "ns",
					Annotations: map[string]string{apis.BlkioWeightAnnotationKey: "-1"},
				},
			},
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ParseBlkioWeight(tt.pod))
		})
	}
}
