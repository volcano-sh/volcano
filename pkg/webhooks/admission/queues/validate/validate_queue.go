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
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	k8score "k8s.io/kubernetes/pkg/apis/core"
	k8scorevalid "k8s.io/kubernetes/pkg/apis/core/validation"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
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
			if err != nil {
				break
			}
		}

		if needsValidateHierarchicalQueue(queue, oldQueue, ar.Request.Operation) {
			err = validateHierarchicalQueueResources(queue)
			if err != nil {
				break
			}
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
	// Note: weight >= 1 validation is now enforced by CRD schema (minimum: 1)
	errs = append(errs, validateResourceOfQueue(queue.Spec, resourcePath.Child("spec"))...)
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

func validateResourceOfQueue(resource schedulingv1beta1.QueueSpec, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	// Validate resource quantity values are non-negative
	if len(resource.Capability) != 0 {
		for resourceName, quantity := range resource.Capability {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(k8score.ResourceName(resourceName), quantity, fldPath.Child("capability").Child(string(resourceName)))...)
		}
	}
	if len(resource.Deserved) != 0 {
		for resourceName, quantity := range resource.Deserved {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(k8score.ResourceName(resourceName), quantity, fldPath.Child("deserved").Child(string(resourceName)))...)
		}
	}
	if len(resource.Guarantee.Resource) != 0 {
		for resourceName, quantity := range resource.Guarantee.Resource {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(k8score.ResourceName(resourceName), quantity, fldPath.Child("guarantee").Child("resource").Child(string(resourceName)))...)
		}
	}

	capabilityResource := api.NewResource(resource.Capability)
	deservedResource := api.NewResource(resource.Deserved)
	guaranteeResource := api.NewResource(resource.Guarantee.Resource)

	if len(resource.Capability) != 0 &&
		capabilityResource.LessPartly(deservedResource, api.Zero) {
		return append(errs, field.Invalid(fldPath.Child("deserved"),
			deservedResource.String(), "deserved should less equal than capability"))
	}

	if len(resource.Capability) != 0 &&
		capabilityResource.LessPartly(guaranteeResource, api.Zero) {
		return append(errs, field.Invalid(fldPath.Child("guarantee"),
			guaranteeResource.String(), "guarantee should less equal than capability"))
	}

	// Validate guarantee <= deserved when guarantee is set
	if len(resource.Guarantee.Resource) != 0 &&
		deservedResource.LessPartly(guaranteeResource, api.Zero) {
		return append(errs, field.Invalid(fldPath.Child("guarantee"),
			guaranteeResource.String(), "guarantee should less equal than deserved"))
	}

	return errs
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

// needsValidateHierarchicalQueue determines if hierarchy resource validation is necessary
// Returns true only if:
// - Queue is not the root queue itself AND
// - Either it's a CREATE operation OR resources (capability/deserved/guarantee) changed on UPDATE OR parent changed on UPDATE
func needsValidateHierarchicalQueue(queue, oldQueue *schedulingv1beta1.Queue, operation admissionv1.Operation) bool {
	// Only the root queue itself should skip validation completely
	if queue.Name == "root" {
		return false
	}

	// For CREATE operations, always validate
	if operation == admissionv1.Create {
		return true
	}

	// For UPDATE operations, only validate if resources or parent changed
	if operation == admissionv1.Update && oldQueue != nil {
		// Check if parent changed
		if queue.Spec.Parent != oldQueue.Spec.Parent {
			return true
		}
		// Check if capability changed
		if !equality.Semantic.DeepEqual(queue.Spec.Capability, oldQueue.Spec.Capability) {
			return true
		}
		// Check if deserved changed
		if !equality.Semantic.DeepEqual(queue.Spec.Deserved, oldQueue.Spec.Deserved) {
			return true
		}
		// Check if guarantee changed
		if !equality.Semantic.DeepEqual(queue.Spec.Guarantee.Resource, oldQueue.Spec.Guarantee.Resource) {
			return true
		}
		// No resource or parent changes, skip validation
		return false
	}

	// Default: skip validation
	return false
}

func validateHierarchicalQueue(queue *schedulingv1beta1.Queue) error {
	if queue.Spec.Parent == "" || queue.Spec.Parent == "root" {
		return nil
	}

	// Prevent a queue from using its own name as the parent
	if queue.Spec.Parent == queue.Name {
		return fmt.Errorf("queue %s cannot use itself as parent", queue.Name)
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

// validateHierarchicalQueueResources validates all hierarchy resource constraints for a queue
// This function uses informer index for efficient lookups instead of listing all queues
// This includes:
// - Child's capability <= parent's capability
// - Sum of siblings' guarantee/deserved <= parent's limit
// - Parent's capability >= each child's capability
// - Sum of children's guarantee/deserved <= parent's limit
func validateHierarchicalQueueResources(queue *schedulingv1beta1.Queue) error {
	// If this is a child queue, validate against parent
	if queue.Spec.Parent != "" && queue.Spec.Parent != "root" {
		// Get parent queue directly using lister
		parentQueue, err := config.QueueLister.Get(queue.Spec.Parent)
		if err != nil {
			return fmt.Errorf("parent queue %s not found: %v", queue.Spec.Parent, err)
		}

		// Check child's capability <= parent's capability
		if err := validateChildAgainstParent(queue, parentQueue); err != nil {
			return err
		}

		// Get siblings using index
		siblings, err := config.GetQueuesByParent(queue.Spec.Parent)
		if err != nil {
			return fmt.Errorf("failed to get sibling queues: %v", err)
		}

		// Check sum of all siblings' (including this queue) guarantee/deserved <= parent's limit
		if err := validateSiblingsSum(queue, parentQueue, siblings); err != nil {
			return err
		}
	}

	// Get children using index
	children, err := config.GetQueuesByParent(queue.Name)
	if err != nil {
		return fmt.Errorf("failed to get child queues: %v", err)
	}

	// If this queue has children, validate all children constraints
	if len(children) > 0 {
		if err := validateChildrenConstraints(queue, children); err != nil {
			return err
		}
	}

	return nil
}

// validateChildAgainstParent validates that child queue's resources don't exceed parent's
func validateChildAgainstParent(child, parent *schedulingv1beta1.Queue) error {
	childCapability := api.NewResource(child.Spec.Capability)
	parentCapability := api.NewResource(parent.Spec.Capability)

	// Check if child's capability exceeds parent's capability in any dimension
	if parentCapability.LessPartly(childCapability, api.Zero) {
		return fmt.Errorf("queue %s capability (%s) exceeds parent queue %s capability (%s)",
			child.Name, childCapability, parent.Name, parentCapability)
	}

	return nil
}

// validateSiblingsSum validates that sum of all sibling queues' resources don't exceed parent's limit
// siblings parameter should already be filtered to only include children of the parent queue
func validateSiblingsSum(queue, parent *schedulingv1beta1.Queue, siblings []*schedulingv1beta1.Queue) error {
	// Accumulate resources from all sibling queues (including the queue being validated)
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()

	// siblings are already filtered by parent, just exclude self
	for _, sibling := range siblings {
		if sibling.Name != queue.Name {
			totalGuarantee.Add(api.NewResource(sibling.Spec.Guarantee.Resource))
			totalDeserved.Add(api.NewResource(sibling.Spec.Deserved))
		}
	}

	// Add the current queue's resources
	totalGuarantee.Add(api.NewResource(queue.Spec.Guarantee.Resource))
	totalDeserved.Add(api.NewResource(queue.Spec.Deserved))

	// Validate guarantee sum
	if err := validateResourceLimit(parent, totalGuarantee, "guarantee", parent.Spec.Guarantee.Resource); err != nil {
		return fmt.Errorf("parent queue %s validation failed: %v", parent.Name, err)
	}

	// Validate deserved sum
	if err := validateResourceLimit(parent, totalDeserved, "deserved", parent.Spec.Deserved); err != nil {
		return fmt.Errorf("parent queue %s validation failed: %v", parent.Name, err)
	}

	return nil
}

// validateChildrenConstraints validates that parent's resources are sufficient for all children
func validateChildrenConstraints(parent *schedulingv1beta1.Queue, children []*schedulingv1beta1.Queue) error {
	parentCapability := api.NewResource(parent.Spec.Capability)
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()

	for _, child := range children {
		// Check parent's capability >= each child's capability
		childCapability := api.NewResource(child.Spec.Capability)
		if parentCapability.LessPartly(childCapability, api.Zero) {
			return fmt.Errorf("queue %s capability (%s) is less than child queue %s capability (%s)",
				parent.Name, parentCapability, child.Name, childCapability)
		}

		// Accumulate children's guarantee and deserved
		totalGuarantee.Add(api.NewResource(child.Spec.Guarantee.Resource))
		totalDeserved.Add(api.NewResource(child.Spec.Deserved))
	}

	// Check parent's guarantee >= sum of children's guarantee
	if err := validateResourceLimit(parent, totalGuarantee, "guarantee", parent.Spec.Guarantee.Resource); err != nil {
		return fmt.Errorf("queue %s validation failed: %v", parent.Name, err)
	}

	// Check parent's deserved >= sum of children's deserved
	if err := validateResourceLimit(parent, totalDeserved, "deserved", parent.Spec.Deserved); err != nil {
		return fmt.Errorf("queue %s validation failed: %v", parent.Name, err)
	}

	return nil
}

// validateResourceLimit checks if children's sum exceeds parent's limit
// It checks against parent's explicit limit first, then falls back to capability
func validateResourceLimit(parent *schedulingv1beta1.Queue, childrenSum *api.Resource,
	resourceType string, parentResource v1.ResourceList) error {
	if len(parentResource) > 0 {
		// Parent has explicit resource limit - check against it
		parentLimit := api.NewResource(parentResource)
		if parentLimit.LessPartly(childrenSum, api.Zero) {
			return fmt.Errorf("sum of children's %s (%s) exceeds parent's %s limit (%s)",
				resourceType, childrenSum, resourceType, parentLimit)
		}
	} else if len(parent.Spec.Capability) > 0 {
		// Parent has no explicit limit but has capability - check against capability
		parentCapability := api.NewResource(parent.Spec.Capability)
		if parentCapability.LessPartly(childrenSum, api.Zero) {
			return fmt.Errorf("sum of children's %s (%s) exceeds parent's capability (%s)",
				resourceType, childrenSum, parentCapability)
		}
	}

	return nil
}
