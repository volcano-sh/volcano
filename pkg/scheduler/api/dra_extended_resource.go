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

package api

import (
	"sync/atomic"

	v1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"
	corev1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
)

// DRAExtendedResourceResolver reports whether an extended resource name is mapped to
// a DeviceClass through spec.extendedResourceName (KEP-5004, DRAExtendedResource).
type DRAExtendedResourceResolver func(rName v1.ResourceName) bool

var draExtendedResourceResolver atomic.Pointer[DRAExtendedResourceResolver]

// SetDRAExtendedResourceResolver installs the resolver used to decide whether an extended
// resource is delegated to DRA. The scheduler cache calls it once when both the
// DynamicResourceAllocation and DRAExtendedResource feature gates are enabled.
// Passing nil disables delegation.
func SetDRAExtendedResourceResolver(fn DRAExtendedResourceResolver) {
	if fn == nil {
		draExtendedResourceResolver.Store(nil)
		return
	}
	draExtendedResourceResolver.Store(&fn)
}

// NewDRAExtendedResourceResolver builds a resolver backed by the DeviceClass mapping kept by
// the shared DRA manager. It returns nil when draManager is nil.
func NewDRAExtendedResourceResolver(draManager fwk.SharedDRAManager) DRAExtendedResourceResolver {
	if draManager == nil {
		return nil
	}
	return func(rName v1.ResourceName) bool {
		return draManager.DeviceClassResolver().GetDeviceClass(rName) != nil
	}
}

// ShouldDelegateResourceToDRA reports whether rName requested by a pod is satisfied by DRA
// rather than by node allocatable on the given node. It mirrors kube-scheduler's
// noderesources.shouldDelegateResourceToDRA: a resource is delegated when the node does not
// advertise it in allocatable (no device plugin) and a DeviceClass maps the resource name.
func ShouldDelegateResourceToDRA(rName v1.ResourceName, node *v1.Node) bool {
	resolver := draExtendedResourceResolver.Load()
	if resolver == nil || !corev1helper.IsExtendedResourceName(rName) {
		return false
	}
	if node != nil {
		if quant, ok := node.Status.Allocatable[rName]; ok && !quant.IsZero() {
			// The node serves this resource through a device plugin; account for it normally.
			return false
		}
	}
	return (*resolver)(rName)
}

// ExcludeDRADelegatedResources returns req without the scalar resources that are delegated
// to DRA on node. req is never mutated; when nothing is delegated, req itself is returned.
func ExcludeDRADelegatedResources(req *Resource, node *v1.Node) *Resource {
	if req == nil || len(req.ScalarResources) == 0 || draExtendedResourceResolver.Load() == nil {
		return req
	}
	var filtered *Resource
	for name := range req.ScalarResources {
		if !ShouldDelegateResourceToDRA(name, node) {
			continue
		}
		if filtered == nil {
			filtered = req.Clone()
		}
		delete(filtered.ScalarResources, name)
	}
	if filtered == nil {
		return req
	}
	return filtered
}

// FitRequest returns the part of req that must fit into this node's Idle/FutureIdle,
// excluding extended resources that are delegated to DRA on this node.
func (ni *NodeInfo) FitRequest(req *Resource) *Resource {
	return ExcludeDRADelegatedResources(req, ni.Node)
}
