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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/preemption"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

type QueueController struct {
	config       *rest.Config
	cache        schedulercache.Cache
	allocator    policy.Interface
	preemptor    preemption.Interface
	quotaManager *quotaManager
}

func NewQueueController(config *rest.Config, cache schedulercache.Cache, allocator policy.Interface, preemptor preemption.Interface) *QueueController {
	queueController := &QueueController{
		config:       config,
		cache:        cache,
		allocator:    allocator,
		preemptor:    preemptor,
		quotaManager: NewQuotaManager(config),
	}

	return queueController
}

func (q *QueueController) Run(stopCh <-chan struct{}) {
	go q.quotaManager.Run(stopCh)
	go q.preemptor.Run(stopCh)
	go wait.Until(q.runOnce, 2*time.Second, stopCh)
}

func (q *QueueController) runOnce() {
	snapshot := q.cache.Dump()
	jobGroups := q.allocator.Group(snapshot.Queues)
	queues := q.allocator.Allocate(jobGroups, snapshot.Nodes)

	queuesForPreempt, _ := q.preemptor.Preprocessing(queues, snapshot.Pods)
	q.preemptor.PreemptResources(queuesForPreempt)
}
