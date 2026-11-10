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

package util

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

// QueueReferenceLookup supplies informer-backed queue lookups to admission.
type QueueReferenceLookup interface {
	GetQueue(name string) (*schedulingv1beta1.Queue, error)
	GetNamespaceQueue(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error)
	GetQueuesByParent(name string) ([]*schedulingv1beta1.Queue, error)
	GetNamespaceQueuesByParent(parent commonutil.ResolvedQueueReference, namespace string) ([]*schedulingv1beta1.NamespaceQueue, error)
}

// QueueReferenceValidationOptions controls workload queue validation.
type QueueReferenceValidationOptions struct {
	// RequireClusterQueueLeaf preserves Job's existing cluster Queue leaf rule.
	RequireClusterQueueLeaf bool
}

// ResolveQueueReference parses a workload queue reference without looking up
// the target object. Parsing is deliberately separate from lister validation
// so the same syntax rules can be reused for parent and workload references.
func ResolveQueueReference(workloadNamespace, reference, defaultQueue string) (commonutil.ResolvedQueueReference, error) {
	if commonutil.HasNamespaceQueuePrefix(reference) &&
		!utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
		return commonutil.ResolvedQueueReference{}, fmt.Errorf("NamespaceQueue feature is disabled")
	}
	resolved, err := commonutil.ResolveWorkloadQueueReference(workloadNamespace, reference, defaultQueue)
	if err != nil {
		return commonutil.ResolvedQueueReference{}, err
	}
	if errs := validation.IsDNS1123Subdomain(resolved.Name); len(errs) > 0 {
		return commonutil.ResolvedQueueReference{}, fmt.Errorf(
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
	config QueueReferenceLookup,
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
	case commonutil.ClusterQueueReferenceScope:
		queue, err := config.GetQueue(resolved.Name)
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
		if utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
			namespaceChildren, err := config.GetNamespaceQueuesByParent(
				commonutil.ResolvedQueueReference{Scope: commonutil.ClusterQueueReferenceScope, Name: queue.Name},
				"",
			)
			if err != nil {
				return fmt.Errorf("failed to get NamespaceQueue children for queue %s: %w", queue.Name, err)
			}
			if len(namespaceChildren) > 0 {
				return fmt.Errorf("can only submit workload to leaf queue, queue `%s` has %d NamespaceQueue children",
					queue.Name, len(namespaceChildren))
			}
		}

	case commonutil.NamespaceQueueReferenceScope:
		queue, err := config.GetNamespaceQueue(resolved.Namespace, resolved.Name)
		if err != nil {
			return fmt.Errorf("unable to find NamespaceQueue: %w", err)
		}
		if queue.Status.State != schedulingv1beta1.QueueStateOpen {
			return fmt.Errorf("can only submit workload to NamespaceQueue with state `Open`, NamespaceQueue `%s/%s` status is `%s`",
				queue.Namespace, queue.Name, queue.Status.State)
		}
		children, err := config.GetNamespaceQueuesByParent(
			commonutil.ResolvedQueueReference{
				Scope:     commonutil.NamespaceQueueReferenceScope,
				Namespace: queue.Namespace,
				Name:      queue.Name,
			},
			queue.Namespace,
		)
		if err != nil {
			return fmt.Errorf("failed to get child NamespaceQueues for %s/%s: %w",
				queue.Namespace, queue.Name, err)
		}
		if len(children) > 0 {
			return fmt.Errorf("can only submit workload to leaf NamespaceQueue, NamespaceQueue `%s/%s` has %d child queues",
				queue.Namespace, queue.Name, len(children))
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
	ready := apiMeta.FindStatusCondition(
		queue.Status.Conditions,
		commonutil.NamespaceQueueReadyCondition,
	)
	if ready != nil &&
		ready.ObservedGeneration == queue.Generation &&
		ready.Status != metav1.ConditionTrue {
		message := "readiness has not been confirmed for the current generation"
		if ready.Message != "" {
			message = ready.Message
		}
		return fmt.Errorf("NamespaceQueue `%s/%s` is not ready: %s",
			queue.Namespace, queue.Name, message)
	}

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

	message := "readiness has not been confirmed for the current generation"
	if ready != nil && ready.Message != "" {
		message = ready.Message
	}
	return fmt.Errorf("NamespaceQueue `%s/%s` is not ready: %s",
		queue.Namespace, queue.Name, message)
}
