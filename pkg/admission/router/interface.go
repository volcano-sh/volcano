package router

import (
	"k8s.io/api/admission/v1beta1"
	whv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/client-go/kubernetes"

	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

//The AdmitFunc returns response
type AdmitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

type AdmissionServiceConfig struct {
	SchedulerName string
	KubeClient    kubernetes.Interface
	VolcanoClient versioned.Interface
}

type AdmissionService struct {
	Path    string
	Func    AdmitFunc
	Handler AdmissionHandler

	ValidatingConfig *whv1beta1.ValidatingWebhookConfiguration
	MutatingConfig   *whv1beta1.MutatingWebhookConfiguration

	Config *AdmissionServiceConfig
}
