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
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/arbclientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/preemption"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// update assign result to api server
func (q *QueueController) updateQueueJob(assignedQJ map[string]*schedulercache.QueueJobInfo) {
	for _, qj := range assignedQJ {
		cpuRes := qj.QueueJob().Status.Allocated.Resources["cpu"].DeepCopy()
		memRes := qj.QueueJob().Status.Allocated.Resources["memory"].DeepCopy()
		cpuInt, _ := cpuRes.AsInt64()
		memInt, _ := memRes.AsInt64()
		glog.V(4).Infof("scheduler, assign queuejob %s cpu %d memory %d\n", qj.Name(), cpuInt, memInt)
	}

	cs, err := arbclientset.NewForConfig(q.config)
	if err != nil {
		glog.Errorf("Fail to create client for queuejob, %#v", err)
		return
	}

	queueJobList, err := cs.ArbV1().Queuejobs("").List(meta_v1.ListOptions{})
	if err != nil {
		glog.Errorf("Fail to get queuejob list, %#v", err)
		return
	}
	for _, t := range queueJobList.Items {
		updateQJ, exist := assignedQJ[t.Name]
		if !exist {
			glog.V(4).Infof("queuejob %s in api server doesn't exist in scheduler cache\n", t.Name)
			continue
		}
		_, err = cs.ArbV1().Queuejobs(updateQJ.QueueJob().Namespace).Update(updateQJ.QueueJob())
		if err != nil {
			glog.V(4).Infof("Fail to update queuejob %s, %#v\n", updateQJ.QueueJob().Name, err)
		}
	}
}

func (q *QueueController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	snapshot := q.cache.Dump()
	jobGroups, allPods := q.allocator.Group(snapshot.Queues, snapshot.QueueJobs, snapshot.Pods)
	queues := q.allocator.Allocate(jobGroups, snapshot.Nodes)

	queuesForPreempt, _ := q.preemptor.Preprocessing(queues, allPods)
	q.preemptor.PreemptResources(queuesForPreempt)

	assignedQJ := q.allocator.Assign(queuesForPreempt, snapshot.QueueJobs)

	q.updateQueueJob(assignedQJ)
}
