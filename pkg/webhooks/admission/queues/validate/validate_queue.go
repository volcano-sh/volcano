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
	"k8s.io/apimachinery/pkg/api/resource"
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
			return util.ToAdmissionResponse(err)
		}
		var oldQueue *schedulingv1beta1.Queue
		if ar.Request.Operation == admissionv1.Update {
			oldQueue, err = schema.DecodeQueue(ar.Request.OldObject, ar.Request.Resource)
			if err != nil {
				return util.ToAdmissionResponse(err)
			}
		}

		if ar.Request.Operation == admissionv1.Create || oldQueue.Spec.Parent != queue.Spec.Parent {
			err = validateHierarchicalQueue(queue)
			if err != nil {
				return util.ToAdmissionResponse(err)
			}
		}

		// Block attribute modifications to root queue on UPDATE
		if config.EnableRootQueueProtection && ar.Request.Operation == admissionv1.Update && queue.Name == "root" && oldQueue != nil {
			if !equality.Semantic.DeepEqual(queue.Spec.Capability, oldQueue.Spec.Capability) ||
				!equality.Semantic.DeepEqual(queue.Spec.Deserved, oldQueue.Spec.Deserved) ||
				!equality.Semantic.DeepEqual(queue.Spec.Guarantee.Resource, oldQueue.Spec.Guarantee.Resource) {
				return util.ToAdmissionResponse(fmt.Errorf("root queue's resource attributes (capability, deserved, guarantee) cannot be modified"))
			}
		}

		if needsValidateHierarchicalQueue(queue, oldQueue, ar.Request.Operation) {
			err = validateHierarchicalQueueResources(queue)
			if err != nil {
				return util.ToAdmissionResponse(err)
			}
		}
	case admissionv1.Delete:
		err = validateQueueDeleting(ar.Request.Name)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}
	default:
		return util.ToAdmissionResponse(fmt.Errorf("invalid operation `%s`, "+
			"expect operation to be `CREATE`, `UPDATE` or `DELETE`", ar.Request.Operation))
	}

	return &admissionv1.AdmissionResponse{
		Allowed: true,
	}
}

func validateQueue(queue *schedulingv1beta1.Queue) error {
	errs := field.ErrorList{}
	resourcePath := field.NewPath("requestBody")

	errs = append(errs, validateResourceQuantityOfQueue(queue.Spec, resourcePath.Child("spec"))...)
	errs = append(errs, validateStateOfQueue(queue.Status.State, resourcePath.Child("spec").Child("state"))...)
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

// Verify the resource quantity Of Queue and configure the resource quantity as guaranteed ≤ deserved ≤ capability
func validateResourceQuantityOfQueue(spec schedulingv1beta1.QueueSpec, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}

	if len(spec.Capability) != 0 {
		for resourceName, quantity := range spec.Capability {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(k8score.ResourceName(resourceName), quantity, fldPath.Child("capability").Child(resourceName.String()))...)
		}
	}

	if len(spec.Deserved) != 0 {
		for resourceName, quantity := range spec.Deserved {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(k8score.ResourceName(resourceName), quantity, fldPath.Child("deserved").Child(resourceName.String()))...)
		}
	}

	if len(spec.Guarantee.Resource) != 0 {
		for resourceName, guaranteeQ := range spec.Guarantee.Resource {
			errs = append(errs, k8scorevalid.ValidateResourceQuantityValue(
				k8score.ResourceName(resourceName), guaranteeQ,
				fldPath.Child("guarantee").Child("resource").Child(resourceName.String()))...,
			)

			desQ, exists := spec.Deserved[resourceName]
			if !exists {
				errs = append(errs, field.Invalid(
					fldPath.Child("deserved").Child(resourceName.String()),
					"<nil>",
					fmt.Sprintf("deserved[%s] must be >= guarantee[%s]=%s",
						resourceName, resourceName, guaranteeQ.String()),
				))
				continue
			}

			if desQ.Cmp(guaranteeQ) < 0 {
				errs = append(errs, field.Invalid(
					fldPath.Child("deserved").Child(resourceName.String()),
					desQ.String(),
					fmt.Sprintf("deserved[%s]=%s must be >= guarantee[%s]=%s",
						resourceName, desQ.String(), resourceName, guaranteeQ.String()),
				))
			}
		}
	}

	if len(spec.Deserved) != 0 && len(spec.Capability) != 0 {
		for resourceName, desQ := range spec.Deserved {
			capQ, capExists := spec.Capability[resourceName]
			// If the capability is not configured considers that the capability is not restricted
			if !capExists {
				continue
			}

			if desQ.Cmp(capQ) > 0 {
				errs = append(errs, field.Invalid(
					fldPath.Child("deserved").Child(resourceName.String()),
					desQ.String(),
					fmt.Sprintf(
						"deserved[%s]=%s must be <= capability[%s]=%s",
						resourceName, desQ.String(), resourceName, capQ.String(),
					),
				))
			}
		}
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

	// Only check allocated pods if the flag is enabled
	if config.EnableQueueAllocatedPodsCheck {
		if allocated, ok := queue.Status.Allocated[v1.ResourcePods]; ok && !allocated.IsZero() {
			return fmt.Errorf("queue %s cannot be deleted because it has allocated Pods: %d",
				queue.Name, allocated.Value())
		}
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

	//Check whether the queue level depth exceeds the upper limit.
	if err := validateQueueDepth(queue); err != nil {
		return err
	}

	parentQueue, err := config.QueueLister.Get(queue.Spec.Parent)
	if err != nil {
		return fmt.Errorf("failed to get parent queue of queue %s: %v", queue.Name, err)
	}

	childQueueNames, err := listQueueChild(parentQueue.Name)
	if err != nil {
		return fmt.Errorf("failed to list child queues of queue %s: %v", parentQueue.Name, err)
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

func getSingleResource(r *api.Resource, name v1.ResourceName) float64 {
	switch name {
	case v1.ResourceCPU:
		return r.MilliCPU
	case v1.ResourceMemory:
		return r.Memory
	default:
		if v, ok := r.ScalarResources[name]; ok {
			return v
		}
	}
	return 0
}

// Recursively searches for the ancestor queue that recently defines the capability
func findNearestAncestorCapability(q *schedulingv1beta1.Queue, rname v1.ResourceName) (float64, bool) {
	parent := q.Spec.Parent
	for parent != "" && parent != "root" {
		pq, err := config.QueueLister.Get(parent)
		if err != nil {
			return 0, false
		}

		if pq.Spec.Capability != nil {
			res := api.NewResource(pq.Spec.Capability)
			val := getSingleResource(res, rname)
			if val > 0 {
				return val, true
			}
		}

		parent = pq.Spec.Parent
	}
	return 0, false
}

// Recursively searches for the maximum capability value of a descendant queue
func findSubtreeMaxCapability(q *schedulingv1beta1.Queue, rname v1.ResourceName) float64 {
	if q.Spec.Capability != nil {
		res := api.NewResource(q.Spec.Capability)
		v := getSingleResource(res, rname)
		if v > 0 {
			return v
		}
	}

	children, _ := listQueueChild(q.Name)
	maxV := float64(0)
	for _, cname := range children {
		cq, err := config.QueueLister.Get(cname)
		if err != nil {
			continue
		}
		v := findSubtreeMaxCapability(cq, rname)
		if v > maxV {
			maxV = v
		}
	}

	return maxV
}

func formatResourceWithType(name v1.ResourceName, value float64) string {
	switch name {
	case v1.ResourceCPU:
		q := resource.NewMilliQuantity(int64(value), resource.DecimalSI)
		return q.String()

	case v1.ResourceMemory:
		q := resource.NewQuantity(int64(value), resource.BinarySI)
		return q.String()

	default:
		return fmt.Sprintf("%.2f", value)
	}
}

func validateQueueDepth(queue *schedulingv1beta1.Queue) error {
	depth := 1
	parent := queue.Spec.Parent

	for parent != "" && parent != "root" {
		depth++
		if depth > config.MaxQueueDepth {
			return fmt.Errorf("queue %s exceeds the maximum allowed depth of %d", queue.Name, config.MaxQueueDepth)
		}
		p, err := config.QueueLister.Get(parent)
		if err != nil {
			return fmt.Errorf("failed to get parent queue %s of queue %s: %v",
				parent, queue.Name, err)
		}
		parent = p.Spec.Parent
	}

	return nil
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

		// Check queue's capability <= ancestors's capability
		if err := validateChildAgainstAncestor(queue); err != nil {
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

// validateChildAgainstAncestor validates that child queue's resources don't exceed ancestors's
func validateChildAgainstAncestor(child *schedulingv1beta1.Queue) error {
	if child.Spec.Capability != nil {
		qRes := api.NewResource(child.Spec.Capability)
		resKeys := qRes.ResourceNames()
		for _, r := range resKeys {
			myVal := getSingleResource(qRes, r)
			if upLimit, ok := findNearestAncestorCapability(child, r); ok {
				if myVal > upLimit {
					return fmt.Errorf("queue %s capability[%s]=%v exceeds its ancestor's capability=%v", child.Name, r, formatResourceWithType(r, myVal), formatResourceWithType(r, upLimit))
				}
			}
		}
	}

	return nil
}

// validateSiblingsSum validates that sum of all sibling queues' resources don't exceed parent's limit
// siblings parameter should already be filtered to only include children of the parent queue
func validateSiblingsSum(queue, parent *schedulingv1beta1.Queue, siblings []*schedulingv1beta1.Queue) error {
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()

	parentGuarantee := api.NewResource(parent.Spec.Guarantee.Resource)
	parentDeserved := api.NewResource(parent.Spec.Deserved)

	// siblings are already filtered by parent, just exclude self
	for _, sibling := range siblings {
		if sibling.Name != queue.Name {
			totalGuarantee.Add(api.NewResource(sibling.Spec.Guarantee.Resource))
			if parentGuarantee.LessPartly(totalGuarantee, api.Zero) {
				return fmt.Errorf("parent queue %s validation failed: sum of children's guarantee (%s) exceeds parent's guarantee limit (%s)",
					parent.Name, totalGuarantee, parentGuarantee)
			}

			totalDeserved.Add(api.NewResource(sibling.Spec.Deserved))
			if parentDeserved.LessPartly(totalDeserved, api.Zero) {
				return fmt.Errorf("parent queue %s validation failed: sum of children's deserved (%s) exceeds parent's deserved limit (%s)",
					parent.Name, totalDeserved, parentDeserved)
			}
		}
	}

	// Add the current queue's resources
	totalGuarantee.Add(api.NewResource(queue.Spec.Guarantee.Resource))
	if parentGuarantee.LessPartly(totalGuarantee, api.Zero) {
		return fmt.Errorf("parent queue %s validation failed: sum of children's guarantee (%s) exceeds parent's guarantee limit (%s)",
			parent.Name, totalGuarantee, parentGuarantee)
	}

	totalDeserved.Add(api.NewResource(queue.Spec.Deserved))
	if parentDeserved.LessPartly(totalDeserved, api.Zero) {
		return fmt.Errorf("parent queue %s validation failed: sum of children's deserved (%s) exceeds parent's deserved limit (%s)",
			parent.Name, totalDeserved, parentDeserved)
	}

	return nil
}

// validateChildrenConstraints validates that parent's resources are sufficient for all children
func validateChildrenConstraints(parent *schedulingv1beta1.Queue, children []*schedulingv1beta1.Queue) error {
	totalGuarantee := api.EmptyResource()
	totalDeserved := api.EmptyResource()

	// Recursively checks child queues as parent only when queue is nil and parentQueue.Spec.Capability exists
	if parent.Spec.Capability != nil {
		// Obtains all resource fields defined by the parent capability
		pRes := api.NewResource(parent.Spec.Capability)
		resKeys := pRes.ResourceNames()
		for _, r := range resKeys {
			parentVal := getSingleResource(pRes, r)

			// Maximum value of capability in descendant
			childMax := float64(0)

			// Traverse all subqueues
			for _, cq := range children {
				// Obtains the maximum capability of the sub-queue sub-tree
				v := findSubtreeMaxCapability(cq, r)
				if v > childMax {
					childMax = v
				}
			}

			// The capability of the parent must be greater than or equal to the maximum capability of the subtree
			if parentVal < childMax {
				return fmt.Errorf("queue %s capability[%s]=%v is smaller than its descendants' max capability=%v",
					parent.Name, r, formatResourceWithType(r, parentVal), formatResourceWithType(r, childMax))
			}
		}
	}

	parentGuarantee := api.NewResource(parent.Spec.Guarantee.Resource)
	parentDeserved := api.NewResource(parent.Spec.Deserved)

	for _, child := range children {
		// Accumulate children's guarantee and deserved
		totalGuarantee.Add(api.NewResource(child.Spec.Guarantee.Resource))
		if parentGuarantee.LessPartly(totalGuarantee, api.Zero) {
			return fmt.Errorf("queue %s validation failed: sum of children's guarantee (%s) exceeds parent's guarantee limit (%s)",
				parent.Name, totalGuarantee, parentGuarantee)
		}

		totalDeserved.Add(api.NewResource(child.Spec.Deserved))
		if parentDeserved.LessPartly(totalDeserved, api.Zero) {
			return fmt.Errorf("queue %s validation failed: sum of children's deserved (%s) exceeds parent's deserved limit (%s)",
				parent.Name, totalDeserved, parentDeserved)
		}
	}

	return nil
}
