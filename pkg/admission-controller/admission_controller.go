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
	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	v1alpha1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

const (
	AdmitJobPath = "/jobs"
)

type AdmitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

var scheme = runtime.NewScheme()
var Codecs = serializer.NewCodecFactory(scheme)

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

func CheckPolicyDuplicate(policies []v1alpha1.LifecyclePolicy) bool {
	policyEvents := map[v1alpha1.Event]v1alpha1.Event{}
	hasDuplicate := false

	for _, policy := range policies {
		if _, found := policyEvents[v1alpha1.AnyEvent]; found {
			hasDuplicate = true
			break
		}
		// * at the end of policies
		if policy.Event == v1alpha1.AnyEvent && len(policyEvents) > 0 {
			hasDuplicate = true
			break
		}

		if _, found := policyEvents[policy.Event]; found {
			hasDuplicate = true
			break
		} else {
			policyEvents[policy.Event] = policy.Event
		}
	}

	return hasDuplicate
}
