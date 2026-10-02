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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// ---- shadowQueueName tests ----

func TestShadowQueueName_Format(t *testing.T) {
	name := shadowQueueName("team-alpha", "ml-training")

	assert.True(t, strings.HasPrefix(name, "nsq-"), "must start with nsq-")
	assert.LessOrEqual(t, len(name), 253, "must be a valid DNS subdomain (≤253 chars)")
	assert.Contains(t, name, "ml-training", "must contain the original queue name")
}

func TestShadowQueueName_Deterministic(t *testing.T) {
	// Same inputs must always produce the same output.
	a := shadowQueueName("ns1", "q1")
	b := shadowQueueName("ns1", "q1")
	assert.Equal(t, a, b, "shadowQueueName must be deterministic")
}

func TestShadowQueueName_NoCollision(t *testing.T) {
	// The hash must distinguish (ns="a-b", name="c") from (ns="a", name="b-c"),
	// which simple concatenation "a-b-c" would not.
	name1 := shadowQueueName("a-b", "c")
	name2 := shadowQueueName("a", "b-c")
	assert.NotEqual(t, name1, name2, "different namespace/name pairs must produce distinct shadow names")
}

func TestShadowQueueName_LongNameTruncated(t *testing.T) {
	longName := strings.Repeat("a", 300)
	name := shadowQueueName("ns", longName)
	assert.LessOrEqual(t, len(name), 253, "result must not exceed 253 chars even for very long names")
}

func TestShadowQueueName_CrossNamespaceUnique(t *testing.T) {
	// Two namespaces with the same queue name must produce different shadow names.
	name1 := shadowQueueName("team-alpha", "default")
	name2 := shadowQueueName("team-beta", "default")
	assert.NotEqual(t, name1, name2, "same queue name in different namespaces must produce distinct shadow names")
}

// ---- buildShadowQueue tests ----

func makeBool(v bool) *bool { return &v }

func TestBuildShadowQueue_BasicSpec(t *testing.T) {
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "team-alpha",
			Name:      "ml-training",
		},
		Spec: schedulingv1beta1.NamespaceQueueSpec{
			Weight: 5,
			Capability: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("10"),
				v1.ResourceMemory: resource.MustParse("20Gi"),
			},
			Deserved: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("4"),
				v1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Reclaimable: makeBool(true),
			Priority:    10,
		},
	}

	q := buildShadowQueue(nsq)

	// Name must match shadowQueueName
	assert.Equal(t, shadowQueueName("team-alpha", "ml-training"), q.Name)

	// Labels and annotations
	assert.Equal(t, ManagedByLabelVal, q.Labels[ManagedByLabelKey])
	assert.Equal(t, "team-alpha/ml-training", q.Annotations[NSQRefAnnotationKey])

	// Spec translation
	assert.Equal(t, int32(5), q.Spec.Weight)
	assert.Equal(t, int32(10), q.Spec.Priority)
	require.NotNil(t, q.Spec.Reclaimable)
	assert.True(t, *q.Spec.Reclaimable)

	// Resource lists copied correctly
	cpuCap := q.Spec.Capability[v1.ResourceCPU]
	assert.Equal(t, "10", (&cpuCap).String())
	memDes := q.Spec.Deserved[v1.ResourceMemory]
	assert.Equal(t, "8Gi", (&memDes).String())
}

func TestBuildShadowQueue_DefaultReclaimable(t *testing.T) {
	// When Reclaimable is unset, buildShadowQueue must default to true,
	// matching the behaviour of the existing queue mutating webhook.
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{},
	}

	q := buildShadowQueue(nsq)
	require.NotNil(t, q.Spec.Reclaimable, "Reclaimable must be set even when absent in spec")
	assert.True(t, *q.Spec.Reclaimable, "default Reclaimable must be true")
}

func TestBuildShadowQueue_DefaultWeight(t *testing.T) {
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Weight: 0},
	}

	q := buildShadowQueue(nsq)
	assert.Equal(t, int32(1), q.Spec.Weight, "zero weight must default to 1 (proportion plugin requires weight ≥ 1)")
}

func TestBuildShadowQueue_GuaranteeTranslated(t *testing.T) {
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec: schedulingv1beta1.NamespaceQueueSpec{
			Guarantee: schedulingv1beta1.Guarantee{
				Resource: v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("2"),
				},
			},
		},
	}

	q := buildShadowQueue(nsq)
	cpu := q.Spec.Guarantee.Resource[v1.ResourceCPU]
	assert.Equal(t, "2", (&cpu).String(), "Guarantee.Resource must be copied to shadow Queue")
}

func TestBuildShadowQueue_EmptyResourceListsOmitted(t *testing.T) {
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{},
	}

	q := buildShadowQueue(nsq)
	assert.Nil(t, q.Spec.Capability, "empty Capability must not be set on shadow Queue")
	assert.Nil(t, q.Spec.Deserved, "empty Deserved must not be set on shadow Queue")
}

func TestBuildShadowQueue_SpecIsolated(t *testing.T) {
	// Mutating the NamespaceQueue after buildShadowQueue must not affect the shadow Queue,
	// confirming that resource lists are deep-copied, not aliased.
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec: schedulingv1beta1.NamespaceQueueSpec{
			Capability: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("8"),
			},
		},
	}

	q := buildShadowQueue(nsq)
	nsq.Spec.Capability[v1.ResourceCPU] = resource.MustParse("999")

	cpu := q.Spec.Capability[v1.ResourceCPU]
	assert.Equal(t, "8", (&cpu).String(), "shadow Queue must be deep-copied from NamespaceQueue spec")
}

// ---- shadowQueueSpecChanged tests ----

func TestShadowQueueSpecChanged_DetectsDrift(t *testing.T) {
	nsq := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "q"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Weight: 3},
	}
	desired := buildShadowQueue(nsq)

	existing := desired.DeepCopy()
	assert.False(t, shadowQueueSpecChanged(existing, desired), "no change should not trigger update")

	existing.Spec.Weight = 99
	assert.True(t, shadowQueueSpecChanged(existing, desired), "weight change must be detected")
}
