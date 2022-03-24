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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// NamespaceName is name of namespace
type NamespaceName string

const (
	// NamespaceWeightKey is the key in ResourceQuota.spec.hard indicating the weight of this namespace
	NamespaceWeightKey = "volcano.sh/namespace.weight"
	// DefaultNamespaceWeight is the default weight of namespace
	DefaultNamespaceWeight = 1
)

// NamespaceInfo records information of namespace
type NamespaceInfo struct {
	// Name is the name of this namespace
	Name NamespaceName
	// Weight is the highest weight among many ResourceQuota.
	Weight int64

	// QuotaStatus stores the ResourceQuotaStatus of all ResourceQuotas in this namespace
	QuotaStatus map[string]v1.ResourceQuotaStatus
}

// GetWeight returns weight of a namespace, any invalid case would get default value
func (n *NamespaceInfo) GetWeight() int64 {
	if n == nil || n.Weight == 0 {
		return DefaultNamespaceWeight
	}
	return n.Weight
}

type quotaItem struct {
	name   string
	weight int64
}

func quotaItemKeyFunc(obj interface{}) (string, error) {
	item, ok := obj.(*quotaItem)
	if !ok {
		return "", fmt.Errorf("obj with type %T could not parse", obj)
	}
	return item.name, nil
}

// for big root heap
func quotaItemLessFunc(a interface{}, b interface{}) bool {
	A := a.(*quotaItem)
	B := b.(*quotaItem)
	return A.weight > B.weight
}

// NamespaceCollection will record all details about namespace
type NamespaceCollection struct {
	Name string

	quotaWeight *cache.Heap

	QuotaStatus map[string]v1.ResourceQuotaStatus
}

// NewNamespaceCollection creates new NamespaceCollection object to record all information about a namespace
func NewNamespaceCollection(name string) *NamespaceCollection {
	n := &NamespaceCollection{
		Name:        name,
		quotaWeight: cache.NewHeap(quotaItemKeyFunc, quotaItemLessFunc),
		QuotaStatus: make(map[string]v1.ResourceQuotaStatus),
	}
	// add at least one item into quotaWeight.
	// Because cache.Heap.Pop would be blocked until queue is not empty
	n.updateWeight(&quotaItem{
		name:   NamespaceWeightKey,
		weight: DefaultNamespaceWeight,
	})
	return n
}

func (n *NamespaceCollection) deleteWeight(q *quotaItem) {
	n.quotaWeight.Delete(q)
}

func (n *NamespaceCollection) updateWeight(q *quotaItem) {
	n.quotaWeight.Update(q)
}

func itemFromQuota(quota *v1.ResourceQuota) *quotaItem {
	var weight int64 = DefaultNamespaceWeight

	quotaWeight, ok := quota.Spec.Hard[NamespaceWeightKey]
	if ok {
		weight = quotaWeight.Value()
	}

	item := &quotaItem{
		name:   quota.Name,
		weight: weight,
	}
	return item
}

// Update modify the registered information according quota object
func (n *NamespaceCollection) Update(quota *v1.ResourceQuota) {
	n.updateWeight(itemFromQuota(quota))
	n.QuotaStatus[quota.Name] = quota.Status
}

// Delete remove the registered information according quota object
func (n *NamespaceCollection) Delete(quota *v1.ResourceQuota) {
	n.deleteWeight(itemFromQuota(quota))
	delete(n.QuotaStatus, quota.Name)
}

// Snapshot will clone a NamespaceInfo without Heap according NamespaceCollection
func (n *NamespaceCollection) Snapshot() *NamespaceInfo {
	var weight int64 = DefaultNamespaceWeight

	obj, err := n.quotaWeight.Pop()
	if err != nil {
		klog.Warningf("namespace %s, quota weight meets error %v when pop", n.Name, err)
	} else {
		item := obj.(*quotaItem)
		weight = item.weight
		n.quotaWeight.Add(item)
	}

	return &NamespaceInfo{
		Name:        NamespaceName(n.Name),
		Weight:      weight,
		QuotaStatus: n.QuotaStatus,
	}
}
