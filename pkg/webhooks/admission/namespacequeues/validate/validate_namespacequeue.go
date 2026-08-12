/*
Copyright 2026 The Volcano Authors.

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
	"sort"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	commonutil "volcano.sh/volcano/pkg/util"
	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/namespacequeues/validate",
	Func: AdmitNamespaceQueues,

	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatenamespacequeue.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update, whv1.Delete},
					Rule: whv1.Rule{
						APIGroups:   []string{schedulingv1beta1.SchemeGroupVersion.Group},
						APIVersions: []string{schedulingv1beta1.SchemeGroupVersion.Version},
						Resources:   []string{"namespacequeues"},
					},
				},
			},
		}},
	},
}

var config = &router.AdmissionServiceConfig{}

// AdmitNamespaceQueues validates NamespaceQueue create, update, and delete requests.
func AdmitNamespaceQueues(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	if ar.Request == nil {
		if !utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
			return util.ToAdmissionResponse(fmt.Errorf("NamespaceQueue feature is disabled"))
		}
		return util.ToAdmissionResponse(fmt.Errorf("admission request must not be nil"))
	}

	// Status is controller-owned and cannot change the NamespaceQueue spec.
	// It must remain writable after parent authorization changes so the
	// controller can publish the denied state and complete finalizer cleanup.
	if ar.Request.SubResource == "status" {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	// Finalizer and deletion metadata updates do not change the validated spec.
	// Allowing them independently prevents stale cross-resource authorization
	// from blocking lifecycle convergence.
	if ar.Request.Operation == admissionv1.Update {
		currentNamespaceQueue, err := schema.DecodeNamespaceQueue(ar.Request.Object, ar.Request.Resource)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}
		old, err := schema.DecodeNamespaceQueue(ar.Request.OldObject, ar.Request.Resource)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}
		if equality.Semantic.DeepEqual(old.Spec, currentNamespaceQueue.Spec) {
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
	}

	if !utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
		return util.ToAdmissionResponse(fmt.Errorf("NamespaceQueue feature is disabled"))
	}
	klog.V(3).Infof("Admitting %s NamespaceQueue %s/%s.", ar.Request.Operation, ar.Request.Namespace, ar.Request.Name)

	if ar.Request.Operation != admissionv1.Create &&
		ar.Request.Operation != admissionv1.Update &&
		ar.Request.Operation != admissionv1.Delete {
		return util.ToAdmissionResponse(fmt.Errorf(
			"invalid operation `%s`, expect operation to be `CREATE`, `UPDATE` or `DELETE`",
			ar.Request.Operation,
		))
	}

	object := ar.Request.Object
	if ar.Request.Operation == admissionv1.Delete {
		object = ar.Request.OldObject
	}

	namespaceQueue, err := schema.DecodeNamespaceQueue(object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}

	switch ar.Request.Operation {
	case admissionv1.Create, admissionv1.Update:
		if ar.Request.Operation == admissionv1.Update {
			oldNamespaceQueue, err := schema.DecodeNamespaceQueue(ar.Request.OldObject, ar.Request.Resource)
			if err != nil {
				return util.ToAdmissionResponse(err)
			}
			if err := commonutil.ValidateNamespaceQueueParentChange(oldNamespaceQueue, namespaceQueue); err != nil {
				return util.ToAdmissionResponse(err)
			}
		}
		if err := commonutil.ValidateQueueResourceRelations(
			namespaceQueue.Spec.Capability,
			namespaceQueue.Spec.Guarantee.Resource,
			namespaceQueue.Spec.Deserved,
		); err != nil {
			return util.ToAdmissionResponse(err)
		}
		if err := validateNamespaceQueueParent(namespaceQueue); err != nil {
			return util.ToAdmissionResponse(err)
		}
		if err := validateNamespaceQueueHierarchy(namespaceQueue); err != nil {
			return util.ToAdmissionResponse(err)
		}
	case admissionv1.Delete:
		if err := validateNamespaceQueueDeleting(namespaceQueue); err != nil {
			return util.ToAdmissionResponse(err)
		}
	}

	return &admissionv1.AdmissionResponse{Allowed: true}
}

func validateNamespaceQueueHierarchy(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	// Admission must reject invalid topology before the object reaches the
	// controller; dynamic readiness and resource aggregation remain controller
	// responsibilities because informer state can lag the API server.
	_, err := commonutil.ResolveNamespaceQueueParentReference(
		namespaceQueue.Namespace, namespaceQueue.Spec.Parent,
	)
	if err != nil {
		return err
	}

	return validateNamespaceQueueSubtreeDepth(namespaceQueue)
}

type namespaceQueueDepthItem struct {
	namespaceQueue *schedulingv1beta1.NamespaceQueue
	depth          int
}

// validateNamespaceQueueSubtreeDepth validates descendants against the
// proposed parent relationship. The candidate object replaces its informer
// copy so UPDATE admission never evaluates the old parent. Breadth-first
// traversal bounds work by the configured subtree depth and visits each cached
// object at most once.
func validateNamespaceQueueSubtreeDepth(
	candidate *schedulingv1beta1.NamespaceQueue,
) error {
	rootDepth, err := commonutil.NamespaceQueueDepth(
		candidate,
		func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error) {
			return config.NamespaceQueueLister.NamespaceQueues(namespace).Get(name)
		},
	)
	if err != nil {
		return err
	}
	if rootDepth > config.MaxNamespaceQueueDepth {
		return fmt.Errorf(
			"NamespaceQueue hierarchy depth %d exceeds maximum depth %d",
			rootDepth,
			config.MaxNamespaceQueueDepth,
		)
	}

	frontier := []namespaceQueueDepthItem{{namespaceQueue: candidate, depth: rootDepth}}
	visited := map[types.NamespacedName]struct{}{
		{Namespace: candidate.Namespace, Name: candidate.Name}: {},
	}

	for len(frontier) > 0 {
		currentQueue := frontier[0]
		frontier = frontier[1:]

		childQueues, err := namespaceQueueChildren(
			commonutil.ResolvedQueueReference{
				Scope:     commonutil.NamespaceQueueReferenceScope,
				Namespace: currentQueue.namespaceQueue.Namespace,
				Name:      currentQueue.namespaceQueue.Name,
			},
			currentQueue.namespaceQueue.Namespace,
			candidate,
		)
		if err != nil {
			return fmt.Errorf("failed to list NamespaceQueue descendants: %w", err)
		}

		for _, childQueue := range childQueues {
			queueKey := types.NamespacedName{Namespace: childQueue.Namespace, Name: childQueue.Name}
			if _, found := visited[queueKey]; found {
				return fmt.Errorf("%w at %q", commonutil.ErrNamespaceQueueHierarchyCycle, queueKey.String())
			}
			visited[queueKey] = struct{}{}

			depth := currentQueue.depth + 1
			if depth > config.MaxNamespaceQueueDepth {
				return fmt.Errorf(
					"NamespaceQueue subtree depth %d exceeds maximum depth %d",
					depth,
					config.MaxNamespaceQueueDepth,
				)
			}
			frontier = append(frontier, namespaceQueueDepthItem{namespaceQueue: childQueue, depth: depth})
		}
	}

	return nil
}

func namespaceQueueChildren(
	parentRef commonutil.ResolvedQueueReference,
	namespace string,
	candidate *schedulingv1beta1.NamespaceQueue,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	// The candidate is overlaid on the informer snapshot because UPDATE
	// admission runs before the new object is visible to the informer.
	childQueues, err := namespaceQueuesByParent(parentRef, namespace)
	if err != nil {
		return nil, err
	}

	candidateKey := types.NamespacedName{Namespace: candidate.Namespace, Name: candidate.Name}
	filteredQueues := make([]*schedulingv1beta1.NamespaceQueue, 0, len(childQueues)+1)
	for _, childQueue := range childQueues {
		queueKey := types.NamespacedName{Namespace: childQueue.Namespace, Name: childQueue.Name}
		if queueKey != candidateKey {
			filteredQueues = append(filteredQueues, childQueue)
		}
	}

	resolvedParentRef, err := commonutil.ResolveNamespaceQueueParentReference(
		candidate.Namespace,
		candidate.Spec.Parent,
	)
	if err == nil && resolvedParentRef == parentRef {
		filteredQueues = append(filteredQueues, candidate)
	}

	return filteredQueues, nil
}

func validateNamespaceQueueDeleting(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue == nil {
		return fmt.Errorf("NamespaceQueue is nil")
	}
	if namespaceQueue.Namespace == "" || namespaceQueue.Name == "" {
		return fmt.Errorf("NamespaceQueue namespace and name must not be empty")
	}

	// Deletion requires descendants to be detached and workload/runtime state to
	// be drained by the user. The controller does not terminate workloads as a
	// side effect of deletion.
	childQueues, err := namespaceQueuesByParent(commonutil.ResolvedQueueReference{
		Scope:     commonutil.NamespaceQueueReferenceScope,
		Namespace: namespaceQueue.Namespace,
		Name:      namespaceQueue.Name,
	}, namespaceQueue.Namespace)
	if err != nil {
		return fmt.Errorf(
			"failed to list child NamespaceQueues for %q/%q: %w",
			namespaceQueue.Namespace,
			namespaceQueue.Name,
			err,
		)
	}

	var childNames []string
	for _, childQueue := range childQueues {
		if childQueue.Name != namespaceQueue.Name {
			childNames = append(childNames, childQueue.Name)
		}
	}
	if len(childNames) > 0 {
		sort.Strings(childNames)
		return fmt.Errorf(
			"NamespaceQueue %q/%q cannot be deleted because it has child NamespaceQueues: %s",
			namespaceQueue.Namespace,
			namespaceQueue.Name,
			strings.Join(childNames, ", "),
		)
	}

	if !commonutil.IsNamespaceQueueDrained(namespaceQueue.Status) {
		return fmt.Errorf(
			"NamespaceQueue %q/%q must be drained before deletion",
			namespaceQueue.Namespace,
			namespaceQueue.Name,
		)
	}

	return nil
}

func namespaceQueuesByParent(
	parentRef commonutil.ResolvedQueueReference,
	namespace string,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	return config.GetNamespaceQueuesByParent(parentRef, namespace)
}

func validateNamespaceQueueParent(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue == nil {
		return fmt.Errorf("NamespaceQueue is nil")
	}

	parentReference := namespaceQueue.Spec.Parent
	if parentReference == "cluster/root" {
		return fmt.Errorf("cluster Queue %q cannot be used as a NamespaceQueue parent", "root")
	}

	resolvedParentRef, err := commonutil.ResolveNamespaceQueueParentReference(
		namespaceQueue.Namespace,
		parentReference,
	)
	if err != nil {
		return err
	}

	// NamespaceQueue parents are resolved locally; cluster Queue parents are
	// additionally authorized against Queue.spec.allowedNamespaces.
	if resolvedParentRef.Scope != commonutil.ClusterQueueReferenceScope {
		return nil
	}

	parentQueue, err := config.QueueLister.Get(resolvedParentRef.Name)
	if err != nil {
		return fmt.Errorf("unable to find parent Queue %q: %w", resolvedParentRef.Name, err)
	}

	if !isNamespaceAllowed(parentQueue, namespaceQueue.Namespace) {
		return fmt.Errorf(
			"namespace %q is not allowed to use parent Queue %q",
			namespaceQueue.Namespace,
			parentQueue.Name,
		)
	}

	return nil
}

func isNamespaceAllowed(parentQueue *schedulingv1beta1.Queue, namespace string) bool {
	// A wildcard is intentionally evaluated here rather than normalized into
	// the API object so existing Queue objects retain their original spec.
	for _, allowedNamespace := range parentQueue.Spec.AllowedNamespaces {
		if allowedNamespace == "*" || allowedNamespace == namespace {
			return true
		}
	}

	return false
}
