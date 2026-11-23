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
	"k8s.io/apimachinery/pkg/api/resource"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/features"
)

func TestValidateJobDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	job := validDeviceTopologyJob()
	if got := validateJobDeviceTopology(job); got != "" {
		t.Fatalf("expected valid Job device topology, got %q", got)
	}

	job.Spec.Tasks[0].PartitionPolicy = &batchv1alpha1.PartitionPolicySpec{
		DeviceTopology: deviceTopologyPolicy(),
	}
	got := validateJobDeviceTopology(job)
	if !strings.Contains(got, "cannot be set together with tasks[].partitionPolicy.deviceTopology") {
		t.Fatalf("expected parent/subgroup conflict, got %q", got)
	}
}

func TestValidateJobUpdateRejectsDeviceTopologyChanges(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	oldJob := validDeviceTopologyJob()
	newJob := oldJob.DeepCopy()
	newJob.Spec.DeviceTopology.Requirements[0].Mode = batchv1alpha1.SoftDeviceTopologyMode

	// The policy is part of the Job spec, which a Job update may not change.
	err := validateJobUpdate(oldJob, newJob)
	if err == nil || !strings.Contains(err.Error(), "job updates may not change fields") {
		t.Fatalf("expected Job spec change error, got %v", err)
	}
}

func TestValidateJobUpdateWithDeviceTopologyWhenFeatureDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, false)

	// An existing Job can still be scaled after the feature gate is turned off.
	oldJob := validDeviceTopologyJob()
	newJob := oldJob.DeepCopy()
	newJob.Spec.Tasks[0].Replicas = 6
	if err := validateJobUpdate(oldJob, newJob); err != nil {
		t.Fatalf("expected Job scaling to be allowed, got %v", err)
	}

	// An update still cannot add a policy.
	oldJob = validDeviceTopologyJob()
	oldJob.Spec.DeviceTopology = nil
	newJob = validDeviceTopologyJob()
	err := validateJobUpdate(oldJob, newJob)
	if err == nil || !strings.Contains(err.Error(), "job updates may not change fields") {
		t.Fatalf("expected Job spec change error, got %v", err)
	}
}

func validDeviceTopologyJob() *batchv1alpha1.Job {
	job := newJob()
	job.Spec.DeviceTopology = deviceTopologyPolicy()
	job.Spec.Tasks[0].Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
		"nvidia.com/gpu": resource.MustParse("1"),
	}
	return job
}

func deviceTopologyPolicy() *batchv1alpha1.DeviceTopologySpec {
	return &batchv1alpha1.DeviceTopologySpec{
		Requirements: []batchv1alpha1.DeviceTopologyRequirement{{
			Resource: batchv1alpha1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
			Mode:     batchv1alpha1.HardDeviceTopologyMode,
		}},
	}
}
