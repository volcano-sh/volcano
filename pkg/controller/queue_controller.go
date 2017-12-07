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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/preemption"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
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

// update assign result to api server
func (q *QueueController) updateTaskSet(assignedTS map[string]*schedulercache.TaskSetInfo) {
	for _, ts := range assignedTS {
		cpuRes := ts.TaskSet().Status.Allocated.Resources["cpu"].DeepCopy()
		memRes := ts.TaskSet().Status.Allocated.Resources["memory"].DeepCopy()
		cpuInt, _ := cpuRes.AsInt64()
		memInt, _ := memRes.AsInt64()
		glog.V(4).Infof("scheduler, assign taskset %s cpu %d memory %d\n", ts.Name(), cpuInt, memInt)
	}
	taskSetClient, _, err := client.NewTaskSetClient(q.config)
	if err != nil {
		panic(err)
	}
	taskSetList := apiv1.TaskSetList{}
	err = taskSetClient.Get().Resource(apiv1.TaskSetPlural).Do().Into(&taskSetList)
	for _, t := range taskSetList.Items {
		updateTS, exist := assignedTS[t.Name]
		if !exist {
			glog.V(4).Infof("taskset %s in api server doesn't exist in scheduler cache\n", t.Name)
			continue
		}

		result := apiv1.TaskSet{}
		err = taskSetClient.Put().
			Resource(apiv1.TaskSetPlural).
			Namespace(updateTS.TaskSet().Namespace).
			Name(updateTS.TaskSet().Name).
			Body(updateTS.TaskSet()).
			Do().Into(&result)
		if err != nil {
			glog.V(4).Infof("Fail to update taskset %s, %#v\n", updateTS.TaskSet().Name, err)
		}
	}
}

func (q *QueueController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	snapshot := q.cache.Dump()
	jobGroups, allPods := q.allocator.Group(snapshot.Queues, snapshot.TaskSets, snapshot.Pods)
	queues := q.allocator.Allocate(jobGroups, snapshot.Nodes)

	queuesForPreempt, _ := q.preemptor.Preprocessing(queues, allPods)
	q.preemptor.PreemptResources(queuesForPreempt)

	assignedTS := q.allocator.Assign(queuesForPreempt, snapshot.TaskSets)

	q.updateTaskSet(assignedTS)
}
