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

package pods

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/api/admission/v1beta1"
	whv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/helpers"
	vcv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/pods/validate",
	Func: AdmitPods,

	Config: config,

	ValidatingConfig: &whv1beta1.ValidatingWebhookConfiguration{
		Webhooks: []whv1beta1.ValidatingWebhook{{
			Name: "validatepod.volcano.sh",
			Rules: []whv1beta1.RuleWithOperations{
				{
					Operations: []whv1beta1.OperationType{whv1beta1.Create},
					Rule: whv1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitPods is to admit pods and return response.
func AdmitPods(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {

	klog.V(3).Infof("admitting pods -- %s", ar.Request.Operation)

	pod, err := schema.DecodePod(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	var msg string
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch ar.Request.Operation {
	case v1beta1.Create:
		msg = validatePod(pod, &reviewResponse)
	default:
		err := fmt.Errorf("expect operation to be 'CREATE'")
		return util.ToAdmissionResponse(err)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

/*
allow pods to create when
1. schedulerName of pod isn't volcano
2. pod has Podgroup whose phase isn't Pending
3. normal pods whose schedulerName is volcano don't have podgroup.
*/
func validatePod(pod *v1.Pod, reviewResponse *v1beta1.AdmissionResponse) string {
	if pod.Spec.SchedulerName != config.SchedulerName {
		return ""
	}

	pgName := ""
	msg := ""

	// vc-job, SN == volcano
	if pod.Annotations != nil {
		pgName = pod.Annotations[vcv1beta1.KubeGroupNameAnnotationKey]
	}
	if pgName != "" {
		if err := checkPGPhase(pod, pgName, true); err != nil {
			msg = err.Error()
			reviewResponse.Allowed = false
		}
		return msg
	}

	// normal pod, SN == volcano
	pgName = helpers.GeneratePodgroupName(pod)
	if err := checkPGPhase(pod, pgName, false); err != nil {
		msg = err.Error()
		reviewResponse.Allowed = false
	}

	return msg
}

func checkPGPhase(pod *v1.Pod, pgName string, isVCJob bool) error {
	pg, err := config.VolcanoClient.SchedulingV1beta1().PodGroups(pod.Namespace).Get(context.TODO(), pgName, metav1.GetOptions{})
	if err != nil {
		if isVCJob || (!isVCJob && !apierrors.IsNotFound(err)) {
			return fmt.Errorf("failed to get PodGroup for pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		}
		return nil
	}
	if pg.Status.Phase != vcv1beta1.PodGroupPending {
		return nil
	}
	return fmt.Errorf("failed to create pod <%s/%s> as the podgroup phase is Pending",
		pod.Namespace, pod.Name)
}
