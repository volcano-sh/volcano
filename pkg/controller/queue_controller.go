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

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/preemption"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"k8s.io/api/core/v1"
)

type QueueController struct {
	config    *rest.Config
	cache     schedulercache.Cache
	allocator policy.Interface
	preemptor preemption.Interface
}

func NewQueueController(config *rest.Config, cache schedulercache.Cache, allocator policy.Interface, preemptor preemption.Interface) *QueueController {
	queueController := &QueueController{
		config:    config,
		cache:     cache,
		allocator: allocator,
		preemptor: preemptor,
	}

	return queueController
}

func createQueueCRD(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createQueueJobCRD(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJobCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (q *QueueController) Run(stopCh <-chan struct{}) {
	// initialized
	createQueueCRD(q.config)
	createQueueJobCRD(q.config)

	go q.preemptor.Run(stopCh)
	go wait.Until(q.runOnce, 2*time.Second, stopCh)
}

// update assign result to api server
func (q *QueueController) updateQueueJob(assignedQJ map[string]*schedulercache.QueueJobInfo) {
	for _, qj := range assignedQJ {
		cpuRes := qj.QueueJob().Status.Allocated["cpu"].DeepCopy()
		memRes := qj.QueueJob().Status.Allocated["memory"].DeepCopy()
		cpuInt, _ := cpuRes.AsInt64()
		memInt, _ := memRes.AsInt64()
		glog.V(4).Infof("scheduler, assign queuejob %s cpu %d memory %d\n", qj.Name(), cpuInt, memInt)
	}

	cs, err := clientset.NewForConfig(q.config)
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

func (q *QueueController) update(queue *schedulercache.QueueInfo) {
	// TODO:
	//   1. Update Queue, so quota manager will update ResourceQuota accordingly
	//   2. Update QueueJob
	//   3. Send Queue allocation info to preemption
}

func (q *QueueController) aggregate(
	jobGroup map[string][]*schedulercache.QueueJobInfo,
	queues []*schedulercache.QueueInfo,
	pods []*schedulercache.PodInfo,
	nodes []*schedulercache.NodeInfo,
) map[string]*schedulercache.QueueInfo {

	result := map[string]*schedulercache.QueueInfo{}
	// merge customer-defined queue and virtual queue, update queue info
	for _, queue := range queues {
		queue.Used = schedulercache.EmptyResource()
		queue.Pods = []*schedulercache.PodInfo{}
		for _, pod := range pods {
			if pod.Pod.Status.Phase == v1.PodRunning && pod.Namespace == queue.Queue.Namespace {
				queue.Pods = append(queue.Pods, pod.Clone())
				queue.Used.Add(pod.Request)
			}
		}

		result[queue.Name] = queue.Clone()
		if jobs, exist := jobGroup[queue.Name]; exist {
			result[queue.Name].Jobs = jobs
		}
	}

	// update node info
	for _, node := range nodes {
		node.Idle = node.Allocatable
		node.Used = schedulercache.EmptyResource()
		node.Pods = []*schedulercache.PodInfo{}
		for _, pod := range pods {
			if pod.Pod.Status.Phase == v1.PodRunning && pod.Pod.Spec.Hostname == node.Name {
				node.Pods = append(node.Pods, pod.Clone())
				node.Used.Add(pod.Request)
			}
		}
		node.Idle.Sub(node.Used)
	}

	return result
}

func (q *QueueController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	snapshot := q.cache.Snapshot()

	queueJobGroup := q.allocator.Group(snapshot.QueueJobs)

	// TODO:
	//   1. merge customer-defined queue and virtual queue
	//   2. Update Node's resource info
	//   3. filter Finished pod out
	queues := q.aggregate(queueJobGroup, snapshot.Queues, snapshot.Pods, snapshot.Nodes)

	queues = q.allocator.Allocate(queues, snapshot.Nodes)

	for _, queue := range queues {
		q.update(queue)
	}
}
