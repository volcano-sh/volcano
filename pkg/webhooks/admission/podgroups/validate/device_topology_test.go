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
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
)

func TestValidatePodGroupDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	podGroup := validDeviceTopologyPodGroup()
	if got := validatePodGroupDeviceTopology(podGroup); got != "" {
		t.Fatalf("expected valid PodGroup device topology, got %q", got)
	}

	podGroup.Spec.SubGroupPolicy = []schedulingv1beta1.SubGroupPolicySpec{{
		Name:           "workers",
		SubGroupSize:   int32Pointer(1),
		LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "worker"}},
		DeviceTopology: deviceTopologyPodGroupPolicy(),
	}}
	got := validatePodGroupDeviceTopology(podGroup)
	if !strings.Contains(got, "cannot be set together with subGroupPolicy[].deviceTopology") {
		t.Fatalf("expected parent/subgroup conflict, got %q", got)
	}
}

func TestValidatePodGroupRejectsMutableDeviceTopology(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, true)

	oldPodGroup := validDeviceTopologyPodGroup()
	newPodGroup := oldPodGroup.DeepCopy()
	newPodGroup.Spec.DeviceTopology.Requirements[0].Mode = schedulingv1beta1.SoftDeviceTopologyMode

	response := admitPodGroupUpdate(t, oldPodGroup, newPodGroup)
	if response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "deviceTopology policies are immutable") {
		t.Fatalf("expected immutable device topology rejection, got %#v", response)
	}
}

func TestValidatePodGroupUpdateWithDeviceTopologyWhenFeatureDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DeviceTopologyAwareScheduling, false)

	// The scheduler and the Job controller keep updating an existing PodGroup
	// after the feature gate is turned off.
	oldPodGroup := validDeviceTopologyPodGroup()
	newPodGroup := oldPodGroup.DeepCopy()
	newPodGroup.Spec.MinMember = 2
	newPodGroup.Status.Phase = schedulingv1beta1.PodGroupRunning
	if response := admitPodGroupUpdate(t, oldPodGroup, newPodGroup); !response.Allowed {
		t.Fatalf("expected PodGroup update to be allowed, got %#v", response)
	}

	// An update still cannot add a policy.
	oldPodGroup = &schedulingv1beta1.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "device-topology", Namespace: "default"}}
	response := admitPodGroupUpdate(t, oldPodGroup, validDeviceTopologyPodGroup())
	if response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "deviceTopology policies are immutable") {
		t.Fatalf("expected immutable device topology rejection, got %#v", response)
	}
}

func admitPodGroupUpdate(t *testing.T, oldPodGroup, newPodGroup *schedulingv1beta1.PodGroup) *admissionv1.AdmissionResponse {
	t.Helper()
	oldRaw, err := json.Marshal(oldPodGroup)
	if err != nil {
		t.Fatalf("marshal old PodGroup: %v", err)
	}
	newRaw, err := json.Marshal(newPodGroup)
	if err != nil {
		t.Fatalf("marshal new PodGroup: %v", err)
	}
	return Validate(admissionv1.AdmissionReview{Request: &admissionv1.AdmissionRequest{
		Operation: admissionv1.Update,
		Object:    runtime.RawExtension{Raw: newRaw},
		OldObject: runtime.RawExtension{Raw: oldRaw},
		Resource: metav1.GroupVersionResource{
			Group:    schedulingv1beta1.SchemeGroupVersion.Group,
			Version:  schedulingv1beta1.SchemeGroupVersion.Version,
			Resource: "podgroups",
		},
	}})
}

func validDeviceTopologyPodGroup() *schedulingv1beta1.PodGroup {
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "device-topology", Namespace: "default"},
		Spec: schedulingv1beta1.PodGroupSpec{
			DeviceTopology: deviceTopologyPodGroupPolicy(),
		},
	}
}

func deviceTopologyPodGroupPolicy() *schedulingv1beta1.DeviceTopologySpec {
	return &schedulingv1beta1.DeviceTopologySpec{
		Requirements: []schedulingv1beta1.DeviceTopologyRequirement{{
			Resource: schedulingv1beta1.DeviceResourceSelector{ExtendedResourceName: "nvidia.com/gpu"},
			Mode:     schedulingv1beta1.HardDeviceTopologyMode,
		}},
	}
}

func int32Pointer(value int32) *int32 {
	return &value
}
