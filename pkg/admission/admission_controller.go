/*
Copyright 2018 The Volcano Authors.

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

package admission

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
)

const (
	AdmitJobPath  = "/jobs"
	MutateJobPath = "/mutating-jobs"
)

type AdmitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

var scheme = runtime.NewScheme()
var Codecs = serializer.NewCodecFactory(scheme)

// policyEventMap defines all policy events and whether to allow external use
var policyEventMap = map[v1alpha1.Event]bool{
	v1alpha1.AnyEvent:           true,
	v1alpha1.PodFailedEvent:     true,
	v1alpha1.PodEvictedEvent:    true,
	v1alpha1.JobUnknownEvent:    true,
	v1alpha1.TaskCompletedEvent: true,
	v1alpha1.OutOfSyncEvent:     false,
	v1alpha1.CommandIssuedEvent: false,
}

// policyActionMap defines all policy actions and whether to allow external use
var policyActionMap = map[v1alpha1.Action]bool{
	v1alpha1.AbortJobAction:     true,
	v1alpha1.RestartJobAction:   true,
	v1alpha1.TerminateJobAction: true,
	v1alpha1.CompleteJobAction:  true,
	v1alpha1.ResumeJobAction:    true,
	v1alpha1.SyncJobAction:      false,
}

func init() {
	addToScheme(scheme)
}

func addToScheme(scheme *runtime.Scheme) {
	corev1.AddToScheme(scheme)
	admissionregistrationv1beta1.AddToScheme(scheme)
}

func ToAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	glog.Error(err)
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func CheckPolicyDuplicate(policies []v1alpha1.LifecyclePolicy) (string, bool) {
	policyEvents := map[v1alpha1.Event]v1alpha1.Event{}
	hasDuplicate := false
	var duplicateInfo string

	for _, policy := range policies {
		if _, found := policyEvents[policy.Event]; found {
			hasDuplicate = true
			duplicateInfo = fmt.Sprintf("%v", policy.Event)
			break
		} else {
			policyEvents[policy.Event] = policy.Event
		}
	}

	if _, found := policyEvents[v1alpha1.AnyEvent]; found && len(policyEvents) > 1 {
		hasDuplicate = true
		duplicateInfo = "if there's * here, no other policy should be here"
	}

	return duplicateInfo, hasDuplicate
}

func DecodeJob(object runtime.RawExtension, resource metav1.GroupVersionResource) (v1alpha1.Job, error) {
	jobResource := metav1.GroupVersionResource{Group: v1alpha1.SchemeGroupVersion.Group, Version: v1alpha1.SchemeGroupVersion.Version, Resource: "jobs"}
	raw := object.Raw
	job := v1alpha1.Job{}

	if resource != jobResource {
		err := fmt.Errorf("expect resource to be %s", jobResource)
		return job, err
	}

	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &job); err != nil {
		return job, err
	}
	glog.V(3).Infof("the job struct is %+v", job)

	return job, nil
}

func validatePolicies(policies []v1alpha1.LifecyclePolicy, fldPath *field.Path) error {
	errs := field.ErrorList{}
	for _, p := range policies {
		if allow, ok := policyEventMap[p.Event]; !ok || !allow {
			errs = append(errs, field.Invalid(fldPath, p.Event, fmt.Sprintf("invalid policy event")))
		}

		if allow, ok := policyActionMap[p.Action]; !ok || !allow {
			errs = append(errs, field.Invalid(fldPath, p.Action, fmt.Sprintf("invalid policy action")))
		}
	}

	return errs.ToAggregate()
}

func getValidEvents() []v1alpha1.Event {
	var events []v1alpha1.Event
	for e, allow := range policyEventMap {
		if allow {
			events = append(events, e)
		}
	}

	return events
}

func getValidActions() []v1alpha1.Action {
	var actions []v1alpha1.Action
	for a, allow := range policyActionMap {
		if allow {
			actions = append(actions, a)
		}
	}

	return actions
}

// validate IO configuration
func ValidateIO(volumes []v1alpha1.VolumeSpec) (string, bool) {
	volumeMap := map[string]bool{}
	for _, volume := range volumes {
		if len(volume.MountPath) == 0 {
			return " mountPath is required;", true
		}
		if _, found := volumeMap[volume.MountPath]; found {
			return fmt.Sprintf(" duplicated mountPath: %s;", volume.MountPath), true
		}
		volumeMap[volume.MountPath] = true
	}
	return "", false
}
