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

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/policy/preemption"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

type QuotaAllocatorController struct {
	config       *rest.Config
	cache        cache.Cache
	allocator    policy.Interface
	preemptor    preemption.Interface
	quotaManager *quotaManager
}

func NewQuotaAllocatorController(config *rest.Config, cache cache.Cache, allocator policy.Interface, preemptor preemption.Interface) *QuotaAllocatorController {
	quotaAllocatorController := &QuotaAllocatorController{
		config:       config,
		cache:        cache,
		allocator:    allocator,
		preemptor:    preemptor,
		quotaManager: NewQuotaManager(config),
	}

	return quotaAllocatorController
}

func (q *QuotaAllocatorController) Run(stopCh <-chan struct{}) {
	go q.quotaManager.Run(stopCh)
	go q.preemptor.Run(stopCh)
	go wait.Until(q.runOnce, 2*time.Second, stopCh)
}

func (q *QuotaAllocatorController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	snapshot := q.cache.Dump()
	jobGroups, allPods := q.allocator.Group(snapshot.QuotaAllocators, snapshot.Pods)
	quotaAllocators := q.allocator.Allocate(jobGroups, snapshot.Nodes)

	quotaAllocatorsForPreempt, _ := q.preemptor.Preprocessing(quotaAllocators, allPods)
	q.preemptor.PreemptResources(quotaAllocatorsForPreempt)
}
