/*
Copyright 2019 The Volcano Authors.

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
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

type parentScope string

const (
	clusterParentScope   parentScope = "cluster"
	namespaceParentScope parentScope = "namespace"

	defaultParentReference = "cluster/default"
	clusterParentPrefix    = "cluster/"
)

type parentTarget struct {
	scope     parentScope
	namespace string
	name      string
}

func resolveParent(nq *schedulingv1beta1.NamespaceQueue) (parentTarget, error) {
	if nq == nil {
		return parentTarget{}, fmt.Errorf("namespace queue is nil")
	}

	parent := nq.Spec.Parent
	if parent == "" {
		parent = defaultParentReference
	}

	if strings.HasPrefix(parent, clusterParentPrefix) {
		name := strings.TrimPrefix(parent, clusterParentPrefix)

		if name == "" || name == "root" {
			return parentTarget{}, fmt.Errorf("invalid cluster parent %q", parent)
		}

		if err := validateParentName(name); err != nil {
			return parentTarget{}, err
		}

		return parentTarget{
			scope: clusterParentScope,
			name:  name,
		}, nil
	}

	if nq.Namespace == "" {
		return parentTarget{}, fmt.Errorf("namespace queue namespace is empty")
	}

	if err := validateParentName(parent); err != nil {
		return parentTarget{}, err
	}

	return parentTarget{
		scope:     namespaceParentScope,
		namespace: nq.Namespace,
		name:      parent,
	}, nil
}

func validateParentName(name string) error {
	if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
		return fmt.Errorf(
			"invalid parent name %q: %s",
			name,
			strings.Join(errs, "; "),
		)
	}

	return nil
}
