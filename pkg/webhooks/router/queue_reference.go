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

	utilfeature "k8s.io/apiserver/pkg/util/feature"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	commonutil "volcano.sh/volcano/pkg/util"
)

type QueueReferenceScope = commonutil.QueueReferenceScope

const (
	ClusterQueueReferenceScope    = commonutil.ClusterQueueReferenceScope
	NamespaceQueueReferenceScope  = commonutil.NamespaceQueueReferenceScope
	NamespaceQueueParentIndexName = "namespaceQueueParent"
)

// ResolvedQueueReference identifies the queue resource selected by a workload reference.
type ResolvedQueueReference = commonutil.ResolvedQueueReference

// ResolveQueueReference resolves a workload queue reference without looking up the target object.
func ResolveQueueReference(workloadNamespace, reference, defaultQueue string) (ResolvedQueueReference, error) {
	if commonutil.HasNamespaceQueuePrefix(reference) &&
		!utilfeature.DefaultFeatureGate.Enabled(features.NamespaceQueue) {
		return ResolvedQueueReference{}, fmt.Errorf("NamespaceQueue feature is disabled")
	}
	return commonutil.ResolveWorkloadQueueReference(workloadNamespace, reference, defaultQueue)
}

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

func NamespaceQueueParentIndexKey(parent ResolvedQueueReference) string {
	if parent.Scope == commonutil.ClusterQueueReferenceScope {
		return "cluster/" + parent.Name
	}
	return "namespace/" + parent.Namespace + "/" + parent.Name
}
