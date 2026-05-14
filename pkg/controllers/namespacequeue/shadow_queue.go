/*
Copyright 2025 The Volcano Authors.

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
	"crypto/sha256"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

const (
	// shadowQueuePrefix marks every cluster-scoped Queue synthesised from a NamespaceQueue.
	// This prefix makes shadow Queues visually distinct from hand-crafted cluster Queues.
	shadowQueuePrefix = "nsq"

	// ManagedByLabelKey / ManagedByLabelVal are set on every shadow Queue so the
	// controller can list and garbage-collect them by label when a NamespaceQueue
	// is deleted, without relying on an OwnerReference (which Kubernetes forbids for
	// cross-namespace ownership: a namespaced NamespaceQueue cannot own a cluster-scoped Queue).
	ManagedByLabelKey = "scheduling.volcano.sh/managed-by"
	ManagedByLabelVal = "namespacequeue-controller"

	// NSQRefAnnotationKey records the "<namespace>/<name>" of the NamespaceQueue that
	// owns this shadow Queue, enabling the controller to map in both directions.
	NSQRefAnnotationKey = "scheduling.volcano.sh/nsq-ref"
)

// shadowQueueName returns a deterministic, DNS-subdomain-safe cluster Queue name for
// the given NamespaceQueue namespace/name pair.
//
// Name format:  nsq-<hash8>-<name>
//
//   - hash8 is the first 8 hex digits of SHA-256(namespace+"/"+name).
//     This ensures global uniqueness even when namespace or name are truncated,
//     because a simple "namespace-name" concatenation would collide:
//     (namespace="a-b", name="c") and (namespace="a", name="b-c") both produce "a-b-c".
//
//   - name is the NamespaceQueue's own name, truncated so the total length stays
//     within the 253-character DNS subdomain limit.
//
// Example:
//
//	namespace="team-alpha", name="ml-training"
//	→ SHA-256("team-alpha/ml-training") = a3f9b1c2...
//	→ shadow name = "nsq-a3f9b1c2-ml-training"
func shadowQueueName(namespace, name string) string {
	hash := sha256.Sum256([]byte(namespace + "/" + name))
	// 4 bytes = 8 hex chars; collision probability ≈ 1 in 4 billion per cluster.
	sha8 := fmt.Sprintf("%x", hash[:4])

	prefix := shadowQueuePrefix + "-" + sha8 + "-"
	maxNameLen := 253 - len(prefix)
	truncated := name
	if len(truncated) > maxNameLen {
		truncated = truncated[:maxNameLen]
	}
	return prefix + truncated
}

// buildShadowQueue constructs the cluster-scoped Queue that the Volcano scheduler
// will observe and schedule against.
//
// Why a shadow Queue instead of teaching the scheduler about NamespaceQueue?
//
// The scheduler cache (pkg/scheduler/cache/cache.go:995) populates ssn.Queues by
// watching cluster-scoped Queue objects via the existing Queue informer. Every plugin
// (capacity, proportion, gang) and every action (allocate, reclaim, preempt) reads
// ssn.Queues[job.Queue] where job.Queue = PodGroup.Spec.Queue. By synthesising a
// real cluster-scoped Queue, all of this machinery continues to work without a single
// line of scheduler change — the scheduler is completely agnostic to whether a Queue
// was created by a human cluster-admin or by the NamespaceQueue controller.
//
// Spec translation rules:
//   - All resource-governing fields (Capability, Guarantee, Deserved, Weight,
//     Reclaimable, Priority) are copied verbatim from NamespaceQueueSpec.
//   - Reclaimable defaults to true (same as the existing queue mutating webhook).
//   - Weight defaults to 1 (same as existing queue mutating webhook default).
//   - Parent is left empty at PoC stage; the full implementation will set it to
//     the cluster admin's designated root quota Queue for the namespace's resource pool,
//     enabling namespace-local queues to participate in cluster-level hierarchy.
func buildShadowQueue(nsq *schedulingv1beta1.NamespaceQueue) *schedulingv1beta1.Queue {
	trueVal := true
	reclaimable := nsq.Spec.Reclaimable
	if reclaimable == nil {
		reclaimable = &trueVal
	}

	weight := nsq.Spec.Weight
	if weight == 0 {
		weight = 1
	}

	q := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: shadowQueueName(nsq.Namespace, nsq.Name),
			Labels: map[string]string{
				ManagedByLabelKey: ManagedByLabelVal,
			},
			Annotations: map[string]string{
				// Cross-namespace OwnerReferences are forbidden by Kubernetes, so we
				// record provenance in an annotation and GC manually via controller watch.
				NSQRefAnnotationKey: nsq.Namespace + "/" + nsq.Name,
			},
		},
		Spec: schedulingv1beta1.QueueSpec{
			Weight:      weight,
			Reclaimable: reclaimable,
			Priority:    nsq.Spec.Priority,
			// Parent intentionally omitted here (see function doc above).
		},
	}

	if len(nsq.Spec.Capability) > 0 {
		q.Spec.Capability = nsq.Spec.Capability.DeepCopy()
	}
	if len(nsq.Spec.Deserved) > 0 {
		q.Spec.Deserved = nsq.Spec.Deserved.DeepCopy()
	}
	if len(nsq.Spec.Guarantee.Resource) > 0 {
		q.Spec.Guarantee = schedulingv1beta1.Guarantee{
			Resource: nsq.Spec.Guarantee.Resource.DeepCopy(),
		}
	}

	return q
}

// shadowQueueSpecChanged reports whether the shadow Queue's spec has drifted from
// what buildShadowQueue would produce for the current NamespaceQueue.
//
// We compare only the fields that buildShadowQueue explicitly manages — NOT the full
// Spec via reflect.DeepEqual. Using DeepEqual on the entire Spec would cause an
// infinite update loop: the existing queue controller's updateQueueParent sets
// spec.parent="root" on any queue without a parent, so our shadow Queue (which
// leaves Parent empty) would appear to drift on every sync cycle, triggering an
// Update that clears the parent, which triggers updateQueueParent again, ad infinitum.
//
// Fields we own: Weight, Capability, Reclaimable, Guarantee, Deserved, Priority.
// Fields we intentionally ignore: Parent (managed by the queue controller until
// the full clusterQueueRef design is resolved with mentors).
func shadowQueueSpecChanged(existing *schedulingv1beta1.Queue, desired *schedulingv1beta1.Queue) bool {
	if existing.Spec.Weight != desired.Spec.Weight {
		return true
	}
	if existing.Spec.Priority != desired.Spec.Priority {
		return true
	}
	if !resourceListEqual(existing.Spec.Capability, desired.Spec.Capability) {
		return true
	}
	if !resourceListEqual(existing.Spec.Deserved, desired.Spec.Deserved) {
		return true
	}
	if !resourceListEqual(existing.Spec.Guarantee.Resource, desired.Spec.Guarantee.Resource) {
		return true
	}
	if !reclaimableEqual(existing.Spec.Reclaimable, desired.Spec.Reclaimable) {
		return true
	}
	return false
}

func resourceListEqual(a, b v1.ResourceList) bool {
	if len(a) != len(b) {
		return false
	}
	for key, valA := range a {
		valB, ok := b[key]
		if !ok {
			return false
		}
		if valA.Cmp(valB) != 0 {
			return false
		}
	}
	return true
}

func reclaimableEqual(a, b *bool) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

