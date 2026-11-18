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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

func TestValidateBatchDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	valid := validBatchDeviceTopology()
	tests := []struct {
		name         string
		policy       *batchv1alpha1.DeviceTopologySpec
		wantContains []string
	}{
		{
			name:   "valid policy",
			policy: valid,
		},
		{
			name: "reject invalid and ambiguous policy",
			policy: &batchv1alpha1.DeviceTopologySpec{
				Requirements: []batchv1alpha1.DeviceTopologyRequirement{
					{
						Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: corev1.ResourceCPU},
						Mode:     "compact",
					},
					{
						Resource: batchv1alpha1.DeviceResourceSelector{},
						Mode:     batchv1alpha1.HardDeviceTopologyMode,
					},
					{
						Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
						Mode:     batchv1alpha1.SoftDeviceTopologyMode,
					},
					{
						Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
						Mode:     batchv1alpha1.HardDeviceTopologyMode,
					},
				},
				Fabric: &batchv1alpha1.DeviceFabricAffinity{Mode: "spread"},
			},
			wantContains: []string{
				"must be a valid Kubernetes extended resource name",
				"must be hard or soft",
				"extendedResourceName is required",
				"duplicates resource",
				"fabric requires exactly one",
				"fabric.mode must be hard or soft",
			},
		},
		{
			name:   "reject empty requirements",
			policy: &batchv1alpha1.DeviceTopologySpec{},
			wantContains: []string{
				"requirements must contain at least one resource",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(ValidateBatchDeviceTopology(tt.policy), "; ")
			if len(tt.wantContains) == 0 && got != "" {
				t.Fatalf("expected no validation errors, got %q", got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("expected %q to contain %q", got, want)
				}
			}
		})
	}
}

func TestValidateDeviceTopologyRejectsDisabledFeature(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, false)

	got := strings.Join(ValidateBatchDeviceTopology(validBatchDeviceTopology()), "; ")
	if !strings.Contains(got, "DeviceTopologyAwareScheduling feature is disabled") {
		t.Fatalf("expected disabled feature error, got %q", got)
	}
}

func TestValidateBatchJobDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	jobSpec := &batchv1alpha1.JobSpec{
		DeviceTopology: validBatchDeviceTopology(),
		Tasks: []batchv1alpha1.TaskSpec{
			deviceTopologyTask("gpu-worker", "nvidia.com/gpu"),
			deviceTopologyTask("cpu-worker", ""),
		},
	}
	if errs := ValidateBatchJobDeviceTopology(jobSpec); len(errs) != 0 {
		t.Fatalf("expected parent policy to select only the GPU task, got %v", errs)
	}

	jobSpec.Tasks[1].PartitionPolicy = &batchv1alpha1.PartitionPolicySpec{
		DeviceTopology: validBatchDeviceTopology(),
	}
	errs := strings.Join(ValidateBatchJobDeviceTopology(jobSpec), "; ")
	for _, want := range []string{
		"cannot be set together with tasks[].partitionPolicy.deviceTopology",
		"not requested by task \"cpu-worker\"",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("expected %q to contain %q", errs, want)
		}
	}
}

func TestValidateSchedulingPodGroupDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	subGroupSize := int32(0)
	spec := &schedulingv1beta1.PodGroupSpec{
		DeviceTopology: validSchedulingDeviceTopology(),
		SubGroupPolicy: []schedulingv1beta1.SubGroupPolicySpec{{
			Name:           "workers",
			SubGroupSize:   &subGroupSize,
			LabelSelector:  &metav1.LabelSelector{},
			DeviceTopology: validSchedulingDeviceTopology(),
		}},
	}

	errs := strings.Join(ValidateSchedulingPodGroupDeviceTopology(spec), "; ")
	for _, want := range []string{
		"cannot be set together with subGroupPolicy[].deviceTopology",
		"subGroupSize must be greater than 0",
		"labelSelector must be non-empty",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("expected %q to contain %q", errs, want)
		}
	}
}

func TestDeviceTopologyPolicyEquality(t *testing.T) {
	subGroupSize := int32(2)
	schedulingOld := &schedulingv1beta1.PodGroupSpec{
		SubGroupPolicy: []schedulingv1beta1.SubGroupPolicySpec{{
			Name:           "workers",
			SubGroupSize:   &subGroupSize,
			LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
			DeviceTopology: validSchedulingDeviceTopology(),
		}},
	}
	schedulingNew := schedulingOld.DeepCopy()
	if !SchedulingPodGroupDeviceTopologyPoliciesEqual(schedulingOld, schedulingNew) {
		t.Fatal("expected identical PodGroup policies to compare equal")
	}
	schedulingNew.SubGroupPolicy[0].SubGroupSize = int32Ptr(3)
	if SchedulingPodGroupDeviceTopologyPoliciesEqual(schedulingOld, schedulingNew) {
		t.Fatal("expected changed subgroup scope to compare unequal")
	}
}

func validBatchDeviceTopology() *batchv1alpha1.DeviceTopologySpec {
	return &batchv1alpha1.DeviceTopologySpec{
		Requirements: []batchv1alpha1.DeviceTopologyRequirement{{
			Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
			Mode:     batchv1alpha1.HardDeviceTopologyMode,
		}},
	}
}

func validSchedulingDeviceTopology() *schedulingv1beta1.DeviceTopologySpec {
	return &schedulingv1beta1.DeviceTopologySpec{
		Requirements: []schedulingv1beta1.DeviceTopologyRequirement{{
			Resource: schedulingv1beta1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
			Mode:     schedulingv1beta1.HardDeviceTopologyMode,
		}},
	}
}

func deviceTopologyTask(name string, deviceResource corev1.ResourceName) batchv1alpha1.TaskSpec {
	resources := corev1.ResourceList{}
	if deviceResource != "" {
		resources[deviceResource] = resource.MustParse("1")
	}
	return batchv1alpha1.TaskSpec{
		Name: name,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name: "worker",
					Resources: corev1.ResourceRequirements{
						Requests: resources,
					},
				}},
			},
		},
	}
}

func int32Ptr(value int32) *int32 {
	return &value
}
