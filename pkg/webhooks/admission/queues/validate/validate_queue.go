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
	"fmt"
	"strconv"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
		if err != nil {
			break
		}
		var oldQueue *schedulingv1beta1.Queue
		if ar.Request.Operation == admissionv1.Update {
			oldQueue, err = schema.DecodeQueue(ar.Request.OldObject, ar.Request.Resource)
			if err != nil {
				break
			}
		}

		if ar.Request.Operation == admissionv1.Create || oldQueue.Spec.Parent != queue.Spec.Parent {
			err = validateHierarchicalQueue(queue)
		}

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
		queueList, err := config.QueueLister.List(labels.Everything())
		if err != nil {
			return append(errs, field.Invalid(fldPath, hierarchy,
				fmt.Sprintf("checking %s, list queues failed: %v",
					schedulingv1beta1.KubeHierarchyAnnotationKey,
					err,
				)))
		}
		for _, queueInTree := range queueList {
			hierarchyInTree := queueInTree.Annotations[schedulingv1beta1.KubeHierarchyAnnotationKey]
			// Add a "/" char to be sure, that we only compare parts that are full nodes.
			// For example if we have in the cluster queue /root/scidev and wants to create a /root/sci
			if hierarchyInTree != "" && queue.Name != queueInTree.Name &&
				strings.HasPrefix(hierarchyInTree, hierarchy+"/") {
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

func validateQueueDeleting(queueName string) error {
	if queueName == "default" {
		return fmt.Errorf("`%s` queue can not be deleted", "default")
	}

	if queueName == "root" {
		return fmt.Errorf("`%s` queue can not be deleted", "root")
	}

	queue, err := config.QueueLister.Get(queueName)
	if err != nil {
		return err
	}

	childQueueNames, err := listQueueChild(queueName)
	if err != nil {
		return fmt.Errorf("failed to list child queues: %v", err)
	}

	if len(childQueueNames) > 0 {
		return fmt.Errorf("queue %s can not be deleted because it has %d child queues: %s",
			queue.Name, len(childQueueNames), strings.Join(childQueueNames, ", "))
	}

	klog.V(3).Infof("Validation passed for deleting hierarchical queue %s", queue.Name)

	return nil
}

func validateHierarchicalQueue(queue *schedulingv1beta1.Queue) error {
	if queue.Spec.Parent == "" || queue.Spec.Parent == "root" {
		return nil
	}
	parentQueue, err := config.QueueLister.Get(queue.Spec.Parent)
	if err != nil {
		return fmt.Errorf("failed to get parent queue of queue %s: %v", queue.Name, err)
	}

	childQueueNames, err := listQueueChild(parentQueue.Name)
	if err != nil {
		return fmt.Errorf("failed to list child queues: %v", err)
	}

	if len(childQueueNames) == 0 {
		if allocated, ok := parentQueue.Status.Allocated[v1.ResourcePods]; ok && !allocated.IsZero() {
			return fmt.Errorf("queue %s cannot be the parent queue of queue %s because it has allocated Pods: %d",
				parentQueue.Name, queue.Name, allocated.Value())
		}
	}

	klog.V(3).Infof("Validation passed for hierarchical queue %s with parent queue %s",
		queue.Name, parentQueue.Name)
	return nil
}

func listQueueChild(parentQueueName string) ([]string, error) {
	queueList, err := config.QueueLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %v", err)
	}

	childQueueNames := make([]string, 0)
	for _, childQueue := range queueList {
		if childQueue.Spec.Parent != parentQueueName {
			continue
		}
		childQueueNames = append(childQueueNames, childQueue.Name)
	}

	return childQueueNames, nil
}
