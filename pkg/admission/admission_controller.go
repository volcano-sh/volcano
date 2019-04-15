/*
Copyright 2019 The Kubernetes Authors.

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
	"github.com/hashicorp/go-multierror"

	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	v1alpha1 "github.com/kubernetes-sigs/volcano/pkg/apis/batch/v1alpha1"
)

const (
	// AdmitJobPath gives admit job API path
	AdmitJobPath = "/jobs"
	// MutateJobPath gives mutate job API path
	MutateJobPath = "/mutating-jobs"
	// PVCInputName gives input persistent volume claim name
	PVCInputName = "volcano.sh/job-input"
	// PVCOutputName gives output persistent volume claim name
	PVCOutputName = "volcano.sh/job-output"
)

// AdmitFunc is a function type which received admissionReview and returns admissionResponse
type AdmitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

var scheme = runtime.NewScheme()

// Codecs is a new codec factory for the scheme
var Codecs = serializer.NewCodecFactory(scheme)

func init() {
	addToScheme(scheme)
}

func addToScheme(scheme *runtime.Scheme) {
	corev1.AddToScheme(scheme)
	admissionregistrationv1beta1.AddToScheme(scheme)
}

// ToAdmissionResponse returns error in AdmissionResponse
func ToAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	glog.Error(err)
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// ValidatePolicies validates lifecyclePolicies
func ValidatePolicies(policies []v1alpha1.LifecyclePolicy) error {
	var err error
	policyEvents := map[v1alpha1.Event]struct{}{}
	exitCodes := map[int32]struct{}{}

	for _, policy := range policies {
		if policy.Event != "" && policy.ExitCode != nil {
			err = multierror.Append(err, fmt.Errorf("must not specify event and exitCode simultaneously"))
			break
		}

		if policy.Event == "" && policy.ExitCode == nil {
			err = multierror.Append(err, fmt.Errorf("either event and exitCode should be specified"))
			break
		}

		if policy.Event != "" {
			// TODO: check event is in supported Event
			if _, found := policyEvents[policy.Event]; found {
				err = multierror.Append(err, fmt.Errorf("duplicate event %v", policy.Event))
				break
			} else {
				policyEvents[policy.Event] = struct{}{}
			}
		} else {
			if *policy.ExitCode == 0 {
				err = multierror.Append(err, fmt.Errorf("0 is not a valid error code"))
				break
			}
			if _, found := exitCodes[*policy.ExitCode]; found {
				err = multierror.Append(err, fmt.Errorf("duplicate exitCode %v", *policy.ExitCode))
				break
			} else {
				exitCodes[*policy.ExitCode] = struct{}{}
			}
		}
	}

	if _, found := policyEvents[v1alpha1.AnyEvent]; found && len(policyEvents) > 1 {
		err = multierror.Append(err, fmt.Errorf("if there's * here, no other policy should be here"))
	}

	return err
}

// DecodeJob deserialize the job object
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
