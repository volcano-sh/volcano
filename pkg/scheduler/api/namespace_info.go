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
)

// NamespaceName is name of namespace
type NamespaceName string

// NamespaceInfo records information of namespace
type NamespaceInfo struct {
	// Name is the name of this namespace
	Name NamespaceName
	// QuotaStatus stores the ResourceQuotaStatus of all ResourceQuotas in this namespace
	QuotaStatus map[string]v1.ResourceQuotaStatus
}

// NamespaceCollection will record all details about namespace
type NamespaceCollection struct {
	Name        string
	QuotaStatus map[string]v1.ResourceQuotaStatus
}

// NewNamespaceCollection creates new NamespaceCollection object to record all information about a namespace
func NewNamespaceCollection(name string) *NamespaceCollection {
	n := &NamespaceCollection{
		Name:        name,
		QuotaStatus: make(map[string]v1.ResourceQuotaStatus),
	}
	return n
}

// Update modify the registered information according quota object
func (n *NamespaceCollection) Update(quota *v1.ResourceQuota) {
	n.QuotaStatus[quota.Name] = quota.Status
}

// Delete remove the registered information according quota object
func (n *NamespaceCollection) Delete(quota *v1.ResourceQuota) {
	delete(n.QuotaStatus, quota.Name)
}

// Snapshot will clone a NamespaceInfo without Heap according NamespaceCollection
func (n *NamespaceCollection) Snapshot() *NamespaceInfo {
	return &NamespaceInfo{
		Name:        NamespaceName(n.Name),
		QuotaStatus: n.QuotaStatus,
	}
}
