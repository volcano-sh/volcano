/*
Copyright 2021 The Volcano Authors.

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
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path:   "/podgroups/validate",
	Func:   Validate,
	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatepodgroup.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create},
					Rule: whv1.Rule{
						APIGroups:   []string{schedulingv1beta1.SchemeGroupVersion.Group},
						APIVersions: []string{schedulingv1beta1.SchemeGroupVersion.Version},
						Resources:   []string{"podgroups"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// Validate validates the PodGroup object when creating or updating it
func Validate(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("Validating %s PodGroup %s", ar.Request.Operation, ar.Request.Name)

	podgroup, err := schema.DecodePodGroup(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	switch ar.Request.Operation {
	case admissionv1.Create:
		err = validatePodGroup(podgroup)
	default:
		err = fmt.Errorf("unsupported operation %s", ar.Request.Operation)
	}

	if err != nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result:  &metav1.Status{Message: err.Error()},
		}
	}

	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

// validatePodGroup validates a PodGroup when it's being created
func validatePodGroup(pg *schedulingv1beta1.PodGroup) error {
	return checkQueueState(pg.Spec.Queue)
}

// checkQueueState verifies if the queue exists and is in the open state
func checkQueueState(queueName string) error {
	if queueName == "" {
		return nil
	}

	queue, err := config.QueueLister.Get(queueName)
	if err != nil {
		return fmt.Errorf("unable to find queue: %v", err)
	}

	if queue.Status.State != schedulingv1beta1.QueueStateOpen {
		return fmt.Errorf("can only submit PodGroup to queue with state `Open`, "+
			"queue `%s` status is `%s`", queue.Name, queue.Status.State)
	}

	return nil
}
