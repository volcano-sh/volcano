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

package namespacequeue

import (
	"fmt"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	controllerutil "volcano.sh/volcano/pkg/util"
)

func resolveParent(
	nq *schedulingv1beta1.NamespaceQueue,
) (controllerutil.ResolvedQueueReference, error) {
	if nq == nil {
		return controllerutil.ResolvedQueueReference{}, fmt.Errorf("namespace queue is nil")
	}

	return controllerutil.ResolveNamespaceQueueParentReference(
		nq.Namespace,
		nq.Spec.Parent,
	)
}

func namespaceQueueReference(
	nq *schedulingv1beta1.NamespaceQueue,
) controllerutil.ResolvedQueueReference {
	if nq == nil {
		return controllerutil.ResolvedQueueReference{}
	}

	return controllerutil.ResolvedQueueReference{
		Scope:     controllerutil.NamespaceQueueReferenceScope,
		Namespace: nq.Namespace,
		Name:      nq.Name,
	}
}

func queueReferenceKey(
	reference controllerutil.ResolvedQueueReference,
) string {
	switch reference.Scope {
	case controllerutil.ClusterQueueReferenceScope:
		if reference.Name == "" {
			return ""
		}
		return "cluster/" + reference.Name
	case controllerutil.NamespaceQueueReferenceScope:
		if reference.Namespace == "" || reference.Name == "" {
			return ""
		}
		return "namespace/" + reference.Namespace + "/" + reference.Name
	default:
		return ""
	}
}
