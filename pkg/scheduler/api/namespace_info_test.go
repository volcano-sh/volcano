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

func newQuota(name string, req int) *v1.ResourceQuota {
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

	return q
}

func TestNamespaceCollection(t *testing.T) {
	c := NewNamespaceCollection("testCollection")
	c.Update(newQuota("abc", 1))

	info := c.Snapshot()
	req := info.QuotaStatus["abc"].Hard[v1.ResourceCPU]
	if req.Value() != 1 {
		t.Errorf("cpu request of quota %s should be %d, but got %d", "abc", 1, req.Value())
	}

	c.Update(newQuota("abc", 2))
	c.Update(newQuota("def", 4))
	c.Update(newQuota("def", 8))
	c.Update(newQuota("ghi", 16))

	info = c.Snapshot()
	req = info.QuotaStatus["abc"].Hard[v1.ResourceCPU]
	if req.Value() != 2 {
		t.Errorf("cpu request of quota %s should be %d, but got %d", "abc", 2, req.Value())
	}

	c.Delete(newQuota("abc", 0))

	info = c.Snapshot()
	if _, ok := info.QuotaStatus["abc"]; ok {
		t.Errorf("QuotaStatus abc of namespace should not exist")
	}

	c.Delete(newQuota("abc", 0))
	c.Delete(newQuota("def", 0))
	c.Delete(newQuota("ghi", 0))
}
