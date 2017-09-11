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

package controller

import (
	"time"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

type ResourceQuotaAllocatorController struct {
	cache        schedulercache.Cache
	allocator    policy.Interface
	quotaManager *quotaManager
}

func NewResourceQuotaAllocatorController(config *rest.Config, cache schedulercache.Cache, allocator policy.Interface) *ResourceQuotaAllocatorController {
	rqaController := &ResourceQuotaAllocatorController{
		cache:     cache,
		allocator: allocator,
		quotaManager: &quotaManager{
			config: config,
			ch:     make(chan updatedResource, 100),
		},
	}

	return rqaController
}

func (r *ResourceQuotaAllocatorController) Run() {
	go r.quotaManager.run()
	wait.Until(r.runOnce, 2*time.Second, wait.NeverStop)
}

func (r *ResourceQuotaAllocatorController) runOnce() {
	snapshot := r.cache.Dump()
	jobGroups := r.allocator.Group(snapshot.Allocators)
	for _, jobs := range jobGroups {
		allocations := r.allocator.Allocate(jobs, snapshot.Nodes)
		for _, alloc := range allocations {
			r.quotaManager.updateQuota(alloc)
		}
	}
}
