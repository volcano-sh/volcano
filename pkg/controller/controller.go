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
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

type QueueController struct {
	config       *rest.Config
	cache        schedulercache.Cache
	allocator    policy.Interface
	quotaManager *quotaManager
}

func NewQueueController(config *rest.Config, cache schedulercache.Cache, allocator policy.Interface) *QueueController {
	queueController := &QueueController{
		config:    config,
		cache:     cache,
		allocator: allocator,
		quotaManager: &quotaManager{
			config: config,
		},
	}

	return queueController
}

func (q *QueueController) Run() {
	go q.quotaManager.Run()
	go wait.Until(q.runOnce, 2*time.Second, wait.NeverStop)
}

func (q *QueueController) runOnce() {
	snapshot := q.cache.Dump()
	jobGroups := q.allocator.Group(snapshot.Queues)
	queues := q.allocator.Allocate(jobGroups, snapshot.Nodes)

	q.updateQueues(queues)
}

func (q *QueueController) updateQueues(queues map[string]*schedulercache.QueueInfo) {
	queueClient, _, err := client.NewClient(q.config)
	if err != nil {
		glog.Error("fail to create queue client")
		return
	}

	for _, queue := range queues {
		result := apiv1.Queue{}
		err = queueClient.Put().
			Resource(apiv1.QueuePlural).
			Namespace(queue.Queue().Namespace).
			Name(queue.Queue().Name).
			Body(queue.Queue()).
			Do().Into(&result)
		if err != nil {
			glog.Errorf("fail to update queue info, name %s, %#v", queue.Queue().Name, err)
		}
	}
}
