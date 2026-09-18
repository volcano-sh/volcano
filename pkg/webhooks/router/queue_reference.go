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

package router

import (
	"fmt"
	"strings"

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	commonutil "volcano.sh/volcano/pkg/util"
)

// QueueReferenceScope identifies whether a workload queue reference targets a
// cluster-scoped Queue or a namespace-scoped NamespaceQueue.
type QueueReferenceScope = commonutil.QueueReferenceScope

const (
	// ClusterQueueReferenceScope identifies a cluster-scoped Queue reference.
	ClusterQueueReferenceScope = commonutil.ClusterQueueReferenceScope
	// NamespaceQueueReferenceScope identifies a namespace-scoped NamespaceQueue reference.
	NamespaceQueueReferenceScope = commonutil.NamespaceQueueReferenceScope
	// NamespaceQueueParentIndexName is the name of the NamespaceQueue parent index.
	NamespaceQueueParentIndexName = "namespaceQueueParent"
)

// ResolvedQueueReference identifies the queue resource selected by a workload reference.
type ResolvedQueueReference = commonutil.ResolvedQueueReference

// QueueReferenceValidationOptions controls compatibility-sensitive workload
// queue validation. Job validation requires a cluster Queue leaf; other
// workload paths retain the existing cluster Queue behavior.
type QueueReferenceValidationOptions struct {
	RequireClusterQueueLeaf bool
}

// ResolveQueueReference parses a workload queue reference without looking up
// the target object. Parsing is deliberately separate from lister validation
// so the same syntax rules can be reused for parent and workload references.
func ResolveQueueReference(workloadNamespace, reference, defaultQueue string) (ResolvedQueueReference, error) {
	if commonutil.HasNamespaceQueuePrefix(reference) &&
		!utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
		return ResolvedQueueReference{}, fmt.Errorf("NamespaceQueue feature is disabled")
	}
	resolved, err := commonutil.ResolveWorkloadQueueReference(workloadNamespace, reference, defaultQueue)
	if err != nil {
		return ResolvedQueueReference{}, err
	}
	if errs := validation.IsDNS1123Subdomain(resolved.Name); len(errs) > 0 {
		return ResolvedQueueReference{}, fmt.Errorf(
			"invalid queue name %q: %s",
			resolved.Name,
			strings.Join(errs, "; "),
		)
	}
	return resolved, nil
}

// ValidateWorkloadQueueReference validates a queue reference against the
// informer-backed admission state. Dynamic hierarchy reconciliation remains the
// responsibility of the NamespaceQueue controller and scheduler.
func ValidateWorkloadQueueReference(
	workloadNamespace, reference, defaultQueue string,
	config *AdmissionServiceConfig,
	options QueueReferenceValidationOptions,
) error {
	if config == nil {
		return fmt.Errorf("admission queue validation config is nil")
	}

	resolved, err := ResolveQueueReference(workloadNamespace, reference, defaultQueue)
	if err != nil {
		return err
	}

	switch resolved.Scope {
	case ClusterQueueReferenceScope:
		if config.QueueLister == nil {
			return fmt.Errorf("cluster Queue lister is not configured")
		}
		queue, err := config.QueueLister.Get(resolved.Name)
		if err != nil {
			return fmt.Errorf("unable to find queue: %w", err)
		}
		if queue.Status.State != schedulingv1beta1.QueueStateOpen {
			return fmt.Errorf("can only submit workload to queue with state `Open`, queue `%s` status is `%s`",
				queue.Name, queue.Status.State)
		}
		if options.RequireClusterQueueLeaf {
			if queue.Name == "root" {
				return fmt.Errorf("can not submit workload to root queue")
			}
			children, err := config.GetQueuesByParent(queue.Name)
			if err != nil {
				return fmt.Errorf("failed to get child queues for queue %s: %w", queue.Name, err)
			}
			if len(children) > 0 {
				return fmt.Errorf("can only submit workload to leaf queue, queue `%s` has %d child queues",
					queue.Name, len(children))
			}
		}

	case NamespaceQueueReferenceScope:
		if config.NamespaceQueueLister == nil {
			return fmt.Errorf("NamespaceQueue lister is not configured")
		}
		queue, err := config.NamespaceQueueLister.
			NamespaceQueues(resolved.Namespace).
			Get(resolved.Name)
		if err != nil {
			return fmt.Errorf("unable to find NamespaceQueue: %w", err)
		}
		if queue.Status.State != schedulingv1beta1.QueueStateOpen {
			return fmt.Errorf("can only submit workload to NamespaceQueue with state `Open`, NamespaceQueue `%s/%s` status is `%s`",
				queue.Namespace, queue.Name, queue.Status.State)
		}
		if !commonutil.IsNamespaceQueueSchedulable(
			queue.Generation,
			string(queue.Status.State),
			queue.Status.Conditions,
		) {
			return namespaceQueueReadinessError(queue)
		}

	default:
		return fmt.Errorf("unsupported queue reference scope %q", resolved.Scope)
	}

	return nil
}

func namespaceQueueReadinessError(queue *schedulingv1beta1.NamespaceQueue) error {
	authorized := apiMeta.FindStatusCondition(
		queue.Status.Conditions,
		commonutil.NamespaceQueueAuthorizedCondition,
	)
	if authorized == nil ||
		authorized.ObservedGeneration != queue.Generation ||
		authorized.Status != metav1.ConditionTrue {
		message := "authorization has not been confirmed for the current generation"
		if authorized != nil && authorized.Message != "" {
			message = authorized.Message
		}
		return fmt.Errorf("NamespaceQueue `%s/%s` is not authorized: %s",
			queue.Namespace, queue.Name, message)
	}

	ready := apiMeta.FindStatusCondition(
		queue.Status.Conditions,
		commonutil.NamespaceQueueReadyCondition,
	)
	message := "readiness has not been confirmed for the current generation"
	if ready != nil && ready.Message != "" {
		message = ready.Message
	}
	return fmt.Errorf("NamespaceQueue `%s/%s` is not ready: %s",
		queue.Namespace, queue.Name, message)
}

// NamespaceQueueParentIndexFunc indexes NamespaceQueues by their resolved
// parent reference. Invalid parent references are omitted from the index and
// are rejected by admission validation with a user-facing error.
func NamespaceQueueParentIndexFunc(obj interface{}) ([]string, error) {
	queue, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return nil, fmt.Errorf("object is not a NamespaceQueue: %T", obj)
	}
	parent, err := commonutil.ResolveNamespaceQueueParentReference(queue.Namespace, queue.Spec.Parent)
	if err != nil {
		return nil, nil
	}
	return []string{NamespaceQueueParentIndexKey(parent)}, nil
}

// NamespaceQueueParentIndexKey returns the collision-free index key for a
// resolved Queue or NamespaceQueue parent.
func NamespaceQueueParentIndexKey(parent ResolvedQueueReference) string {
	if parent.Scope == commonutil.ClusterQueueReferenceScope {
		return "cluster/" + parent.Name
	}
	return "namespace/" + parent.Namespace + "/" + parent.Name
}
