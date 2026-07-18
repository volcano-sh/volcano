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

package utils

import (
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientsetfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
)

func TestUpdateHyperNodeDoesNotMutateCache(t *testing.T) {
	existing := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{Name: "hn0"},
		Spec:       topologyv1alpha1.HyperNodeSpec{Tier: 1},
	}

	vcClient := vcclientsetfake.NewSimpleClientset(existing)
	informer := vcinformer.NewSharedInformerFactory(vcClient, 0).Topology().V1alpha1().HyperNodes()
	if err := informer.Informer().GetIndexer().Add(existing); err != nil {
		t.Fatalf("failed to seed the cache: %v", err)
	}

	updated := existing.DeepCopy()
	updated.Spec.Tier = 2

	if err := UpdateHyperNode(vcClient, informer.Lister(), updated); err != nil {
		t.Fatalf("UpdateHyperNode returned an error: %v", err)
	}

	// The lister hands back the shared cache object, so the update must not have
	// touched the one still sitting in the cache.
	cached, err := informer.Lister().Get("hn0")
	if err != nil {
		t.Fatalf("failed to read back from the cache: %v", err)
	}
	if cached.Spec.Tier != 1 {
		t.Errorf("cached hypernode was mutated: tier = %d, want 1", cached.Spec.Tier)
	}
}

func TestUpdateHyperNodeStatusUsesFreshResourceVersion(t *testing.T) {
	existing := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{Name: "hn0", ResourceVersion: "1"},
		Spec:       topologyv1alpha1.HyperNodeSpec{Tier: 1},
	}

	vcClient := vcclientsetfake.NewSimpleClientset(existing)
	informer := vcinformer.NewSharedInformerFactory(vcClient, 0).Topology().V1alpha1().HyperNodes()
	if err := informer.Informer().GetIndexer().Add(existing); err != nil {
		t.Fatalf("failed to seed the cache: %v", err)
	}

	// Stand in for the apiserver: the spec write bumps the version, and the
	// status write is rejected if it still carries the old one.
	statusVersion := ""
	vcClient.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		hyperNode := action.(k8stesting.UpdateAction).GetObject().(*topologyv1alpha1.HyperNode)
		if action.GetSubresource() == "status" {
			statusVersion = hyperNode.ResourceVersion
			if hyperNode.ResourceVersion != "2" {
				return true, nil, apierrors.NewConflict(topologyv1alpha1.Resource("hypernodes"),
					hyperNode.Name, fmt.Errorf("the object has been modified"))
			}
			return true, hyperNode, nil
		}
		applied := hyperNode.DeepCopy()
		applied.ResourceVersion = "2"
		return true, applied, nil
	})

	updated := existing.DeepCopy()
	updated.Spec.Tier = 2

	if err := UpdateHyperNode(vcClient, informer.Lister(), updated); err != nil {
		t.Fatalf("UpdateHyperNode returned an error: %v", err)
	}
	if statusVersion != "2" {
		t.Errorf("status update sent resourceVersion %q, want 2 (the one Update handed back)", statusVersion)
	}
}
