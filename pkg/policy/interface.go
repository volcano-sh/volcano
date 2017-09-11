/*
Copyright 2017 The Kubernetes Authors.

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

package policy

import (
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

// Interface is the interface of policy.
type Interface interface {
	// The unique name of allocator.
	Name() string

	// Initialize initializes the allocator plugins.
	Initialize()

	// Group grouping the job into different bucket, and allocate those resources based on those groups.
	Group(job []*schedulercache.ResourceQuotaAllocatorInfo) map[string][]*schedulercache.ResourceQuotaAllocatorInfo

	// Allocate allocates the cluster's resources into each group.
	Allocate(jobs []*schedulercache.ResourceQuotaAllocatorInfo, nodes []*schedulercache.NodeInfo) map[string]*schedulercache.ResourceQuotaAllocatorInfo

	// Assign allocates resources of group into each jobs.
	Assign(jobs []*schedulercache.ResourceQuotaAllocatorInfo, alloc *schedulercache.ResourceQuotaAllocatorInfo) *schedulercache.Resource

	// Polish returns the Pods that should be evict to release resources.
	Polish(job *schedulercache.ResourceQuotaAllocatorInfo, res *schedulercache.Resource) []*schedulercache.ResourceQuotaAllocatorInfo

	// UnIntialize un-initializes the allocator plugins.
	UnInitialize()
}
