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
	"fmt"
	"time"

	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	schedcache "github.com/kubernetes-incubator/kube-arbitrator/pkg/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/preemption"
)

type PolicyController struct {
	config          *rest.Config
	clientset       *clientset.Clientset
	cache           schedcache.Cache
	allocator       policy.Interface
	preemption      preemption.Interface
	queueController *QueueController
}

func NewPolicyController(config *rest.Config, allocator policy.Interface, preemption preemption.Interface) (*PolicyController, error) {
	cs, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("fail to create client for PolicyController: %#v", err)
	}

	queueController := &PolicyController{
		config:     config,
		clientset:  cs,
		cache:      schedcache.New(config),
		allocator:  allocator,
		preemption: preemption,
	}

	return queueController, nil
}

func (pc *PolicyController) Run(stopCh <-chan struct{}) {
	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	go pc.preemption.Run(stopCh)
	go wait.Until(pc.runOnce, 2*time.Second, stopCh)
}

// update assign result to api server
func (pc *PolicyController) updateQueueJob(jobs []*schedcache.QueueJobInfo) {
	// TODO: only get running QueueJobs to update
	queueJobList, err := pc.clientset.ArbV1().Queuejobs("").List(meta_v1.ListOptions{})
	if err != nil {
		glog.Errorf("Fail to get queuejob list, %#v", err)
		return
	}

	jobMap := make(map[string]*schedcache.QueueJobInfo)
	for _, job := range jobs {
		jobMap[job.Name] = job
	}

	for _, t := range queueJobList.Items {
		updateQJ, exist := jobMap[t.Name]
		if !exist {
			glog.V(4).Infof("queuejob %s in api server doesn't exist in scheduler cache\n", t.Name)
			continue
		}

		t.Status.Allocated = updateQJ.Allocated.ResourceList()

		_, err = pc.clientset.ArbV1().Queuejobs(t.Namespace).Update(&t)
		if err != nil {
			glog.V(4).Infof("Fail to update queuejob %s: %#v\n", t.Name, err)
		}
	}
}

// update assign result to api server
func (pc *PolicyController) updateQueue(queue *schedcache.QueueInfo) {
	queueObj, err := pc.clientset.ArbV1().Queues("").Get(queue.Name, meta_v1.GetOptions{})
	if err != nil {
		glog.Errorf("Fail to get queuejob list, %#v", err)
		return
	}

	queueObj.Status.Allocated = queue.Allocated.ResourceList()
	queueObj.Status.Deserved = queue.Deserved.ResourceList()
	queueObj.Status.Used = queue.Used.ResourceList()

	_, err = pc.clientset.ArbV1().Queues("").Update(queueObj)
	if err != nil {
		glog.V(4).Infof("Fail to update queuejob %s: %#v\n", queueObj.Name, err)
	}
}

func (pc *PolicyController) update(queue *schedcache.QueueInfo) {
	pc.updateQueueJob(queue.Jobs)
	pc.updateQueue(queue)

	pc.preemption.Enqueue(queue)
}

func (pc *PolicyController) mergeQueue(queueJobs map[string][]*schedcache.QueueJobInfo, queues []*schedcache.QueueInfo, pods []*schedcache.PodInfo) map[string]*schedcache.QueueInfo {
	result := map[string]*schedcache.QueueInfo{}
	// Append user-defined queue to the result
	for _, queue := range queues {
		for _, pod := range pods {
			if pod.Phase == v1.PodRunning && pod.Namespace == queue.Namespace {
				queue.Pods = append(queue.Pods, pod)
				queue.Used.Add(pod.Request)
			}
		}

		if jobs, exist := queueJobs[queue.Name]; exist {
			queue.Jobs = jobs
			delete(queueJobs, queue.Name)
		}

		result[queue.Name] = queue
	}

	// TODO: Build virtual Queue for QueueJobs

	return result
}

// updateNodes updates node's resource usage and running pods; re-build those info in each schedule cycle to
// avoid race-condition, but may impact performance.
// TODO: update node info when pod updated.
func (pc *PolicyController) updateNodes(nodes []*schedcache.NodeInfo, pods []*schedcache.PodInfo) {
	for _, node := range nodes {
		node.Idle = node.Allocatable
		node.Used = schedcache.EmptyResource()
		node.Pods = []*schedcache.PodInfo{}
		for _, pod := range pods {
			if pod.Phase == v1.PodRunning && pod.Hostname == node.Name {
				node.Pods = append(node.Pods, pod)
				node.Used.Add(pod.Request)
			}
		}
		node.Idle.Sub(node.Used)
	}
}

func (pc *PolicyController) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	snapshot := pc.cache.Snapshot()

	queueJobs := pc.allocator.Group(snapshot.QueueJobs)

	queues := pc.mergeQueue(queueJobs, snapshot.Queues, snapshot.Pods)

	pc.updateNodes(snapshot.Nodes, snapshot.Pods)

	queues = pc.allocator.Allocate(queues, snapshot.Nodes)

	for _, queue := range queues {
		pc.update(queue)
	}
}
