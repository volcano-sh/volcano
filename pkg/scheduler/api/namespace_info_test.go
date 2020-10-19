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
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"testing"
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

func newQuotaWithResource(name string, weight int, quotaHard []string, quotaUsed []string) *v1.ResourceQuota {
	q := &v1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourceRequestsCPU:        resource.MustParse(quotaHard[0]),
				v1.ResourceRequestsMemory:     resource.MustParse(quotaHard[1]),
				"requests." + GPUResourceName: resource.MustParse(quotaHard[2]),
			},
		},
		Status: v1.ResourceQuotaStatus{
			Hard: v1.ResourceList{
				v1.ResourceRequestsCPU:        resource.MustParse(quotaHard[0]),
				v1.ResourceRequestsMemory:     resource.MustParse(quotaHard[1]),
				"requests." + GPUResourceName: resource.MustParse(quotaHard[2]),
			},
			Used: v1.ResourceList{
				v1.ResourceRequestsCPU:        resource.MustParse(quotaUsed[0]),
				v1.ResourceRequestsMemory:     resource.MustParse(quotaUsed[1]),
				"requests." + GPUResourceName: resource.MustParse(quotaUsed[2]),
			},
		},
	}

	if weight >= 0 {
		q.Spec.Hard[v1.ResourceName(NamespaceWeightKey)] = *resource.NewQuantity(int64(weight), resource.DecimalSI)
	}

	return q
}

func expectResource(resource []float64) *Resource {
	return &Resource{
		MilliCPU: resource[0],
		Memory:   resource[1],
		ScalarResources: map[v1.ResourceName]float64{
			GPUResourceName: resource[2],
		},
	}
}

func TestNamespaceCollection(t *testing.T) {
	c := NewNamespaceCollection("testCollection")
	c.Update(newQuotaWithResource("abc", 123, []string{"8", "80000000", "8"}, []string{"2", "30000000", "2"}))
	c.Update(newQuotaWithResource("abc", 456, []string{"5", "70000000", "5"}, []string{"2", "30000000", "2"}))
	c.Update(newQuotaWithResource("def", -1, []string{"9", "90000000", "6"}, []string{"2", "30000000", "2"}))
	c.Update(newQuotaWithResource("def", 16, []string{"9", "60000000", "4"}, []string{"2", "30000000", "2"}))
	c.Update(newQuota("ghi", 0))

	info := c.Snapshot()
	if info.Weight != 456 {
		t.Errorf("weight of namespace should be %d, but not %d", 456, info.Weight)
	}
	if !reflect.DeepEqual(info.RQToResource, expectResource([]float64{float64(3000), float64(30000000), float64(2000)})) {
		t.Errorf("Convert resource test failed, expected %+v, but got %+v", expectResource([]float64{float64(3000), float64(30000000), float64(2000)}), info.RQToResource)
	}

	c.Delete(newQuota("abc", 0))

	info = c.Snapshot()
	if info.Weight != 16 {
		t.Errorf("weight of namespace should be %d, but not %d", 16, info.Weight)
	}
	if _, ok := info.RQStatus["abc"]; ok {
		t.Errorf("RQStatus abc of namespace should not exist")
	}
	if !reflect.DeepEqual(info.RQToResource, expectResource([]float64{float64(7000), float64(30000000), float64(2000)})) {
		t.Errorf("Convert resource test failed, expected %+v, but got %+v", expectResource([]float64{float64(7000), float64(30000000), float64(2000)}), info.RQToResource)
	}

	c.Delete(newQuota("abc", 0))
	c.Delete(newQuota("def", 15))
	c.Delete(newQuota("ghi", -1))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be default weight %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}
	if !reflect.DeepEqual(info.RQToResource, EmptyResource()) {
		t.Errorf("resource should be equal to EmptyResource()")
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

	c.Delete(newQuota("abc", 0))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}

	c.Delete(newQuota("abc", 0))
	c.Delete(newQuota("def", 15))
	c.Delete(newQuota("ghi", -1))

	info = c.Snapshot()
	if info.Weight != DefaultNamespaceWeight {
		t.Errorf("weight of namespace should be default weight %d, but not %d", DefaultNamespaceWeight, info.Weight)
	}
}
