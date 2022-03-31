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

package validate

import (
	"context"
	"fmt"
	admissionv1 "k8s.io/api/admission/v1"
	"strconv"
	"strings"

	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/queues/validate",
	Func: AdmitQueues,

	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatequeue.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update, whv1.Delete},
					Rule: whv1.Rule{
						APIGroups:   []string{schedulingv1beta1.SchemeGroupVersion.Group},
						APIVersions: []string{schedulingv1beta1.SchemeGroupVersion.Version},
						Resources:   []string{"queues"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitQueues is to admit queues and return response.
func AdmitQueues(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("Admitting %s queue %s.", ar.Request.Operation, ar.Request.Name)

	queue, err := schema.DecodeQueue(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	switch ar.Request.Operation {
	case admissionv1.Create, admissionv1.Update:
		err = validateQueue(queue)
	case admissionv1.Delete:
		err = validateQueueDeleting(ar.Request.Name)
	default:
		return util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
			"expect operation to be `CREATE`, `UPDATE` or `DELETE`", ar.Request.Operation))
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

func validateQueue(queue *schedulingv1beta1.Queue) error {
	errs := field.ErrorList{}
	resourcePath := field.NewPath("requestBody")

	errs = append(errs, validateStateOfQueue(queue.Status.State, resourcePath.Child("spec").Child("state"))...)
	errs = append(errs, validateWeightOfQueue(queue.Spec.Weight, resourcePath.Child("spec").Child("weight"))...)
	errs = append(errs, validateHierarchicalAttributes(queue, resourcePath.Child("metadata").Child("annotations"))...)

	if len(errs) > 0 {
		return errs.ToAggregate()
	}

	return nil
}
func validateHierarchicalAttributes(queue *schedulingv1beta1.Queue, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}
	hierarchy := queue.Annotations[schedulingv1beta1.KubeHierarchyAnnotationKey]
	hierarchicalWeights := queue.Annotations[schedulingv1beta1.KubeHierarchyWeightAnnotationKey]
	if hierarchy != "" || hierarchicalWeights != "" {
		paths := strings.Split(hierarchy, "/")
		weights := strings.Split(hierarchicalWeights, "/")
		// path length must be the same with weights length
		if len(paths) != len(weights) {
			return append(errs, field.Invalid(fldPath, hierarchy,
				fmt.Sprintf("%s must have the same length with %s",
					schedulingv1beta1.KubeHierarchyAnnotationKey,
					schedulingv1beta1.KubeHierarchyWeightAnnotationKey,
				)))
		}

		// check weights format
		for _, weight := range weights {
			weightFloat, err := strconv.ParseFloat(weight, 64)
			if err != nil {
				return append(errs, field.Invalid(fldPath, hierarchicalWeights,
					fmt.Sprintf("%s in the %s is invalid number: %v",
						weight, hierarchicalWeights, err,
					)))
			}
			if weightFloat <= 0 {
				return append(errs, field.Invalid(fldPath, hierarchicalWeights,
					fmt.Sprintf("%s in the %s must be larger than 0",
						weight, hierarchicalWeights,
					)))
			}
		}

		// The node is not allowed to be in the sub path of a node.
		// For example, a queue with "root/sci" conflicts with a queue with "root/sci/dev"
		queueList, err := config.VolcanoClient.SchedulingV1beta1().Queues().List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return append(errs, field.Invalid(fldPath, hierarchy,
				fmt.Sprintf("checking %s, list queues failed: %v",
					schedulingv1beta1.KubeHierarchyAnnotationKey,
					err,
				)))
		}
		for _, queueInTree := range queueList.Items {
			hierarchyInTree := queueInTree.Annotations[schedulingv1beta1.KubeHierarchyAnnotationKey]
			if hierarchyInTree != "" && queue.Name != queueInTree.Name &&
				strings.HasPrefix(hierarchyInTree, hierarchy) {
				return append(errs, field.Invalid(fldPath, hierarchy,
					fmt.Sprintf("%s is not allowed to be in the sub path of %s of queue %s",
						hierarchy, hierarchyInTree, queueInTree.Name)))
			}
		}
	}
	return errs
}

func validateStateOfQueue(value schedulingv1beta1.QueueState, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	if len(value) == 0 {
		return errs
	}

	validQueueStates := []schedulingv1beta1.QueueState{
		schedulingv1beta1.QueueStateOpen,
		schedulingv1beta1.QueueStateClosed,
	}

	for _, validQueue := range validQueueStates {
		if value == validQueue {
			return errs
		}
	}

	return append(errs, field.Invalid(fldPath, value, fmt.Sprintf("queue state must be in %v", validQueueStates)))
}

func validateWeightOfQueue(value int32, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}
	if value > 0 {
		return errs
	}
	return append(errs, field.Invalid(fldPath, value, "queue weight must be a positive integer"))
}

func validateQueueDeleting(queue string) error {
	if queue == "default" {
		return fmt.Errorf("`%s` queue can not be deleted", "default")
	}

	_, err := config.VolcanoClient.SchedulingV1beta1().Queues().Get(context.TODO(), queue, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return nil
}
