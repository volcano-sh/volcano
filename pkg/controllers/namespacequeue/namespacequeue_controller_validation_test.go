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
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	controllerutil "volcano.sh/volcano/pkg/util"
)

func TestValidateChildrenAggregateCacheInvalidatesOnNamespaceQueueAdd(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			Guarantee: schedulingv1beta1.Guarantee{Resource: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			}},
		},
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatal(err)
	}

	first := newTestNamespaceQueue("team-a", "first", "cluster/research")
	first.Spec.Guarantee.Resource = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("40m"),
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(first); err != nil {
		t.Fatal(err)
	}

	parentRef := controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  "research",
	}
	if err := controller.validateChildrenAggregate(parentRef); err != nil {
		t.Fatalf("initial aggregate validation failed: %v", err)
	}

	second := newTestNamespaceQueue("team-a", "second", "cluster/research")
	second.Spec.Guarantee.Resource = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("70m"),
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(second); err != nil {
		t.Fatal(err)
	}
	// The add event invalidates the cached successful result before the next
	// reconciliation observes the new sibling aggregate.
	controller.addNamespaceQueue(second)

	err := controller.validateChildrenAggregate(parentRef)
	if err == nil || !strings.Contains(err.Error(), "sum of child guarantees") {
		t.Fatalf("aggregate validation error = %v, want child guarantee violation", err)
	}
}

func TestValidateChildrenAggregateCacheInvalidatesOnQueueSpecUpdate(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research", ResourceVersion: "1"},
		Spec: schedulingv1beta1.QueueSpec{
			Guarantee: schedulingv1beta1.Guarantee{Resource: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("100m"),
			}},
		},
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatal(err)
	}
	child := newTestNamespaceQueue("team-a", "child", "cluster/research")
	child.Spec.Guarantee.Resource = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("60m"),
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
		t.Fatal(err)
	}

	parentRef := controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  "research",
	}
	if err := controller.validateChildrenAggregate(parentRef); err != nil {
		t.Fatalf("initial aggregate validation failed: %v", err)
	}

	updatedParent := parent.DeepCopy()
	updatedParent.ResourceVersion = "2"
	updatedParent.Spec.Guarantee.Resource = corev1.ResourceList{
		corev1.ResourceCPU: resource.MustParse("50m"),
	}
	if err := controller.queueInformer.Informer().GetIndexer().Update(updatedParent); err != nil {
		t.Fatal(err)
	}
	controller.updateQueue(parent, updatedParent)

	err := controller.validateChildrenAggregate(parentRef)
	if err == nil || !strings.Contains(err.Error(), "sum of child guarantees") {
		t.Fatalf("aggregate validation error = %v, want parent guarantee violation", err)
	}
}

func BenchmarkValidateChildrenAggregate(b *testing.B) {
	for _, childCount := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("recompute/%d", childCount), func(b *testing.B) {
			controller, parentRef := benchmarkAggregateController(b, childCount)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				controller.invalidateAggregateCache()
				if err := controller.validateChildrenAggregate(parentRef); err != nil {
					b.Fatal(err)
				}
			}
		})

		b.Run(fmt.Sprintf("cached/%d", childCount), func(b *testing.B) {
			controller, parentRef := benchmarkAggregateController(b, childCount)
			if err := controller.validateChildrenAggregate(parentRef); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := controller.validateChildrenAggregate(parentRef); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkAggregateController(
	b *testing.B,
	childCount int,
) (*namespaceQueueController, controllerutil.ResolvedQueueReference) {
	b.Helper()
	controller := newFakeNamespaceQueueController(b)
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			Guarantee: schedulingv1beta1.Guarantee{Resource: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("1000000m"),
			}},
		},
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < childCount; i++ {
		child := newTestNamespaceQueue("team-a", fmt.Sprintf("child-%d", i), "cluster/research")
		child.Spec.Guarantee.Resource = corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("1m"),
		}
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
			b.Fatal(err)
		}
	}
	return controller, controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  "research",
	}
}
