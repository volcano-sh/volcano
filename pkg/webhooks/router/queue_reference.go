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

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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
