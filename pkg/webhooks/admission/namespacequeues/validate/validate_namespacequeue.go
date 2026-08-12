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
	"k8s.io/apimachinery/pkg/labels"
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
	if !utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
		if ar.Request != nil && ar.Request.Operation == admissionv1.Delete {
			return &admissionv1.AdmissionResponse{Allowed: true}
		}
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
	parent, err := commonutil.ResolveNamespaceQueueParentReference(
		namespaceQueue.Namespace, namespaceQueue.Spec.Parent,
	)
	if err != nil {
		return err
	}

	if parent.Scope == commonutil.ClusterQueueReferenceScope {
		queues, err := namespaceQueuesByParent(parent, namespaceQueue.Namespace)
		if err != nil {
			return fmt.Errorf("failed to list NamespaceQueues in namespace %q: %w", namespaceQueue.Namespace, err)
		}
		for _, existing := range queues {
			if existing.Name == namespaceQueue.Name {
				continue
			}
			existingParent, err := commonutil.ResolveNamespaceQueueParentReference(existing.Namespace, existing.Spec.Parent)
			if err == nil && existingParent == parent {
				return fmt.Errorf(
					"namespace %q already attaches NamespaceQueue %q to Queue %q",
					namespaceQueue.Namespace, existing.Name, parent.Name,
				)
			}
		}
		return nil
	}

	depth, err := commonutil.NamespaceQueueDepth(
		namespaceQueue,
		func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error) {
			return config.NamespaceQueueLister.NamespaceQueues(namespace).Get(name)
		},
	)
	if err != nil {
		return err
	}
	if depth > config.MaxNamespaceQueueDepth {
		return fmt.Errorf(
			"NamespaceQueue hierarchy depth %d exceeds maximum depth %d",
			depth,
			config.MaxNamespaceQueueDepth,
		)
	}
	return nil
}

func validateNamespaceQueueDeleting(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue == nil {
		return fmt.Errorf("NamespaceQueue is nil")
	}
	if namespaceQueue.Namespace == "" || namespaceQueue.Name == "" {
		return fmt.Errorf("NamespaceQueue namespace and name must not be empty")
	}

	children, err := namespaceQueuesByParent(commonutil.ResolvedQueueReference{
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
	for _, child := range children {
		if child.Name != namespaceQueue.Name {
			childNames = append(childNames, child.Name)
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

	if !util.IsNamespaceQueueClosedAndDrained(namespaceQueue) {
		return fmt.Errorf(
			"NamespaceQueue %q/%q must be closed and drained before deletion",
			namespaceQueue.Namespace,
			namespaceQueue.Name,
		)
	}

	return nil
}

func namespaceQueuesByParent(
	parent commonutil.ResolvedQueueReference,
	namespace string,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	if config.NamespaceQueueInformer != nil {
		objects, err := config.NamespaceQueueInformer.GetIndexer().ByIndex(
			router.NamespaceQueueParentIndexName,
			router.NamespaceQueueParentIndexKey(parent),
		)
		if err != nil {
			return nil, err
		}
		queues := make([]*schedulingv1beta1.NamespaceQueue, 0, len(objects))
		for _, object := range objects {
			queue, ok := object.(*schedulingv1beta1.NamespaceQueue)
			if ok && (namespace == "" || queue.Namespace == namespace) {
				queues = append(queues, queue)
			}
		}
		return queues, nil
	}
	queues, err := config.NamespaceQueueLister.NamespaceQueues(namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	matched := make([]*schedulingv1beta1.NamespaceQueue, 0, len(queues))
	for _, queue := range queues {
		resolved, err := commonutil.ResolveNamespaceQueueParentReference(queue.Namespace, queue.Spec.Parent)
		if err == nil && resolved == parent {
			matched = append(matched, queue)
		}
	}
	return matched, nil
}

func validateNamespaceQueueParent(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue == nil {
		return fmt.Errorf("NamespaceQueue is nil")
	}

	parent := namespaceQueue.Spec.Parent
	if parent == "" {
		parent = "cluster/default"
	}
	if parent == "cluster/root" {
		return fmt.Errorf("cluster Queue %q cannot be used as a NamespaceQueue parent", "root")
	}

	target, err := commonutil.ResolveNamespaceQueueParentReference(
		namespaceQueue.Namespace,
		parent,
	)
	if err != nil {
		return err
	}

	if target.Scope != commonutil.ClusterQueueReferenceScope {
		return nil
	}

	queue, err := config.QueueLister.Get(target.Name)
	if err != nil {
		return fmt.Errorf("unable to find parent Queue %q: %w", target.Name, err)
	}

	if !isNamespaceAllowed(queue, namespaceQueue.Namespace) {
		return fmt.Errorf(
			"namespace %q is not allowed to use parent Queue %q",
			namespaceQueue.Namespace,
			queue.Name,
		)
	}

	return nil
}

func isNamespaceAllowed(queue *schedulingv1beta1.Queue, namespace string) bool {
	for _, allowedNamespace := range queue.Spec.AllowedNamespaces {
		if allowedNamespace == "*" || allowedNamespace == namespace {
			return true
		}
	}

	return false
}
