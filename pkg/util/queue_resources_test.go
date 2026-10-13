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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestValidateQueueResourceRelations(t *testing.T) {
	tests := []struct {
		name       string
		capability corev1.ResourceList
		guarantee  corev1.ResourceList
		deserved   corev1.ResourceList
		wantError  bool
	}{
		{
			name:       "valid",
			capability: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
			guarantee:  corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			deserved:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
		},
		{
			name:       "negative quantity",
			capability: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("-1")},
			wantError:  true,
		},
		{
			name:      "guarantee without deserved",
			guarantee: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			wantError: true,
		},
		{
			name:      "guarantee exceeds deserved",
			guarantee: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			deserved:  corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			wantError: true,
		},
		{
			name:       "deserved exceeds capability",
			capability: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
			deserved:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")},
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateQueueResourceRelations(tt.capability, tt.guarantee, tt.deserved)
			if (err != nil) != tt.wantError {
				t.Fatalf("ValidateQueueResourceRelations() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
