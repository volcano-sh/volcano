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

package validate

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

func TestValidateDeviceTopologyAnnotation(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)
	previousSchedulerNames := config.SchedulerNames
	config.SchedulerNames = []string{"volcano"}
	t.Cleanup(func() { config.SchedulerNames = previousSchedulerNames })

	validPolicy := `{"requirements":[{"resource":{"extendedResourceName":"nvidia.com/gpu"},"mode":"hard"}]}`
	tests := []struct {
		name          string
		schedulerName string
		policy        string
		wantContains  string
	}{
		{
			name:          "valid Volcano Pod policy",
			schedulerName: "volcano",
			policy:        validPolicy,
		},
		{
			name:          "reject non Volcano scheduler",
			schedulerName: "default-scheduler",
			policy:        validPolicy,
			wantContains:  "requires a configured Volcano scheduler",
		},
		{
			name:          "reject unknown policy field",
			schedulerName: "volcano",
			policy:        `{"requirements":[{"resource":{"extendedResourceName":"nvidia.com/gpu"},"mode":"hard"}],"allocationStrategy":"Compact"}`,
			wantContains:  "must contain a valid device topology policy",
		},
		{
			name:          "reject invalid policy",
			schedulerName: "volcano",
			policy:        `{"requirements":[{"resource":{},"mode":"hard"}]}`,
			wantContains:  "extendedResourceName is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
					schedulingv1beta1.DeviceTopologyAnnotationKey: tt.policy,
				}},
				Spec: corev1.PodSpec{SchedulerName: tt.schedulerName},
			}
			err := validateDeviceTopologyAnnotation(pod)
			if tt.wantContains == "" && err != nil {
				t.Fatalf("expected annotation to be valid, got %v", err)
			}
			if tt.wantContains != "" && (err == nil || !strings.Contains(err.Error(), tt.wantContains)) {
				t.Fatalf("expected error containing %q, got %v", tt.wantContains, err)
			}
		})
	}
}

func TestValidateDeviceTopologyAnnotationRejectsDisabledFeature(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, false)
	previousSchedulerNames := config.SchedulerNames
	config.SchedulerNames = []string{"volcano"}
	t.Cleanup(func() { config.SchedulerNames = previousSchedulerNames })

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{
			schedulingv1beta1.DeviceTopologyAnnotationKey: `{"requirements":[{"resource":{"extendedResourceName":"nvidia.com/gpu"},"mode":"hard"}]}`,
		}},
		Spec: corev1.PodSpec{SchedulerName: "volcano"},
	}
	err := validateDeviceTopologyAnnotation(pod)
	if err == nil || !strings.Contains(err.Error(), "DeviceTopologyAwareScheduling feature is disabled") {
		t.Fatalf("expected disabled feature error, got %v", err)
	}
}
