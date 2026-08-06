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

	"k8s.io/apimachinery/pkg/util/validation"
)

const NamespaceQueueReferencePrefix = "namespace/"

type QueueReferenceScope string

const (
	ClusterQueueReferenceScope   QueueReferenceScope = "cluster"
	NamespaceQueueReferenceScope QueueReferenceScope = "namespace"
)

// ResolvedQueueReference identifies a queue resource without coupling callers
// to scheduler IDs or controller-specific parent targets.
type ResolvedQueueReference struct {
	Scope     QueueReferenceScope
	Namespace string
	Name      string
}

// HasNamespaceQueuePrefix reports whether a workload reference is intended
// for the NamespaceQueue path. It deliberately does not validate the name.
func HasNamespaceQueuePrefix(reference string) bool {
	return strings.HasPrefix(reference, NamespaceQueueReferencePrefix)
}

// ResolveWorkloadQueueReference resolves a Job, PodGroup, or queue annotation
// reference. An unqualified workload reference selects a cluster Queue.
func ResolveWorkloadQueueReference(
	workloadNamespace, reference, defaultQueue string,
) (ResolvedQueueReference, error) {
	if reference == "" {
		reference = defaultQueue
	}

	if !strings.Contains(reference, "/") {
		if reference == "" {
			return ResolvedQueueReference{}, fmt.Errorf("queue reference is empty")
		}
		return ResolvedQueueReference{
			Scope: ClusterQueueReferenceScope,
			Name:  reference,
		}, nil
	}

	if !HasNamespaceQueuePrefix(reference) {
		return ResolvedQueueReference{}, fmt.Errorf("invalid queue reference %q", reference)
	}

	name := strings.TrimPrefix(reference, NamespaceQueueReferencePrefix)
	if workloadNamespace == "" || name == "" || strings.Contains(name, "/") {
		return ResolvedQueueReference{}, fmt.Errorf("invalid queue reference %q", reference)
	}

	return ResolvedQueueReference{
		Scope:     NamespaceQueueReferenceScope,
		Namespace: workloadNamespace,
		Name:      name,
	}, nil
}

// ResolveNamespaceQueueParentReference resolves NamespaceQueue.spec.parent.
// An unqualified parent selects a NamespaceQueue in the same namespace.
func ResolveNamespaceQueueParentReference(
	namespace, reference string,
) (ResolvedQueueReference, error) {
	if reference == "" {
		reference = "cluster/default"
	}

	const clusterPrefix = "cluster/"
	if strings.HasPrefix(reference, clusterPrefix) {
		name := strings.TrimPrefix(reference, clusterPrefix)
		if name == "" || name == "root" {
			return ResolvedQueueReference{}, fmt.Errorf("invalid cluster parent %q", reference)
		}
		if err := validateQueueName(name); err != nil {
			return ResolvedQueueReference{}, err
		}
		return ResolvedQueueReference{
			Scope: ClusterQueueReferenceScope,
			Name:  name,
		}, nil
	}

	if namespace == "" {
		return ResolvedQueueReference{}, fmt.Errorf("namespace queue namespace is empty")
	}
	if err := validateQueueName(reference); err != nil {
		return ResolvedQueueReference{}, err
	}

	return ResolvedQueueReference{
		Scope:     NamespaceQueueReferenceScope,
		Namespace: namespace,
		Name:      reference,
	}, nil
}

func validateQueueName(name string) error {
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf("invalid parent name %q: %s", name, strings.Join(errs, "; "))
	}
	return nil
}
