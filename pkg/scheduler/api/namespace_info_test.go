/*
Copyright 2018 The Volcano Authors.

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
package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newQuota(name string, weight, req int) *v1.ResourceQuota {
	q := &v1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: make(v1.ResourceList),
		},
		Status: v1.ResourceQuotaStatus{
			Hard: v1.ResourceList{
				v1.ResourceCPU: *resource.NewQuantity(int64(req), resource.DecimalSI),
			},
		},
	}

	if weight >= 0 {
		q.Spec.Hard[v1.ResourceName(NamespaceWeightKey)] = *resource.NewQuantity(int64(weight), resource.DecimalSI)
	}

	return q
}

func TestNamespaceCollection(t *testing.T) {
	c := NewNamespaceCollection("testCollection")
	c.Update(newQuota("abc", 123, 1))

	info := c.Snapshot()
	req := info.QuotaStatus["abc"].Hard[v1.ResourceCPU]
	if req.Value() != 1 {
		t.Errorf("cpu request of quota %s should be %d, but got %d", "abc", 1, req.Value())
	}

	c.Update(newQuota("abc", 456, 2))
	c.Update(newQuota("def", -1, 4))
	c.Update(newQuota("def", 16, 8))
	c.Update(newQuota("ghi", 0, 16))

	info = c.Snapshot()
	if info.Weight != 456 {
		t.Errorf("weight of namespace should be %d, but got %d", 456, info.Weight)
	}
	req = info.QuotaStatus["abc"].Hard[v1.ResourceCPU]
	if req.Value() != 2 {
		t.Errorf("cpu request of quota %s should be %d, but got %d", "abc", 2, req.Value())
	}

	c.Delete(newQuota("abc", 0, 0))

	info = c.Snapshot()
	if info.Weight != 16 {
		t.Errorf("weight of namespace should be %d, but not %d", 16, info.Weight)
	}
	if _, ok := info.QuotaStatus["abc"]; ok {
		t.Errorf("QuotaStatus abc of namespace should not exist")
	}

	c.Delete(newQuota("abc", 0, 0))
	c.Delete(newQuota("def", 15, 0))
	c.Delete(newQuota("ghi", -1, 0))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be default weight %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}
}

func TestEmptyNamespaceCollection(t *testing.T) {
	c := NewNamespaceCollection("testEmptyCollection")

	info := c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}

	// snapshot can be called anytime
	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}

	c.Delete(newQuota("abc", 0, 0))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}

	c.Delete(newQuota("abc", 0, 0))
	c.Delete(newQuota("def", 15, 0))
	c.Delete(newQuota("ghi", -1, 0))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be default weight %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}
}
