/*
Copyright 2019 The Volcano Authors.

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

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/scheduling"
)

// ToAdmissionResponse updates the admission response with the input error.
func ToAdmissionResponse(err error) *admissionv1.AdmissionResponse {
	klog.Error(err)
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

// BelongToVcJob Check if pod belongs to vcjob
func BelongToVcJob(pod *v1.Pod) bool {
	for _, ownerReference := range pod.OwnerReferences {
		if *ownerReference.Controller && strings.HasPrefix(ownerReference.APIVersion, scheduling.GroupName) && ownerReference.Kind == "Job" {
			return true
		}
	}
	return false
}
