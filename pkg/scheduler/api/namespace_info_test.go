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

func newQuota(name string, weight int) *v1.ResourceQuota {
	q := &v1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: make(v1.ResourceList),
		},
	}

	if weight >= 0 {
		q.Spec.Hard[v1.ResourceName(NamespaceWeightKey)] = *resource.NewQuantity(int64(weight), resource.DecimalSI)
	}

	return q
}

func TestNamespaceCollection(t *testing.T) {
	c := NewNamespaceCollection("testCollection")
	c.Update(newQuota("abc", 123))
	c.Update(newQuota("abc", 456))
	c.Update(newQuota("def", -1))
	c.Update(newQuota("def", 16))
	c.Update(newQuota("ghi", 0))

	info := c.Snapshot()
	if info.Weight != 456 {
		t.Errorf("weight of namespace should be %d, but not %d", 456, info.Weight)
	}

	c.Delete(newQuota("abc", 0))

	info = c.Snapshot()
	if info.Weight != 16 {
		t.Errorf("weight of namespace should be %d, but not %d", 16, info.Weight)
	}
}
