package util

import "k8s.io/api/admission/v1beta1"
import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
import "k8s.io/klog"

//ToAdmissionResponse updates the admission response with the input error
func ToAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	klog.Error(err)
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}
