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
	qInformerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	qclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/queue/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type quotaManager struct {
	config        *rest.Config
	queueInformer qclient.QueueInformer
}

func NewQuotaManager(config *rest.Config) *quotaManager {
	qm := &quotaManager{
		config: config,
	}

	queueClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	qInformerFactory := qInformerfactory.NewSharedInformerFactory(queueClient, 0)
	// create informer for queue information
	qm.queueInformer = qInformerFactory.Queue().Queues()
	qm.queueInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *apiv1.Queue:
					glog.V(4).Infof("filter queue name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qm.AddQueue,
				DeleteFunc: qm.DeleteQueue,
			},
		})

	return qm
}

func (qm *quotaManager) Run(stopCh <-chan struct{}) {
	go qm.queueInformer.Informer().Run(stopCh)
	wait.Until(qm.runOnce, 500*time.Millisecond, stopCh)
}

// run get request from queue and update to Quota
func (qm *quotaManager) runOnce() {
	queues, err := qm.fetchAllQueue()
	if err != nil {
		glog.Error("fail to fetch all queue info")
		return
	}

	qm.updateQuotas(queues)
}

func (qm *quotaManager) fetchAllQueue() ([]apiv1.Queue, error) {
	queueClient, _, err := client.NewClient(qm.config)
	if err != nil {
		return nil, err
	}

	queueList := apiv1.QueueList{}
	err = queueClient.Get().Resource(apiv1.QueuePlural).Do().Into(&queueList)
	if err != nil {
		return nil, err
	}

	return queueList.Items, nil
}

func (qm *quotaManager) updateQuotas(queues []apiv1.Queue) {
	cs := kubernetes.NewForConfigOrDie(qm.config)

	for _, queue := range queues {
		rqController := cs.CoreV1().ResourceQuotas(queue.Namespace)

		var options meta_v1.ListOptions
		rqList, err := rqController.List(options)
		if err != nil || len(rqList.Items) != 1 {
			glog.Errorf("ecounter an error or more than one resourceQuota, namespace %s, err %#v", queue.Namespace, err)
			continue
		}

		updatedRq := rqList.Items[0].DeepCopy()
		if cpuQuantity, ok := queue.Status.Allocated.Resources["cpu"]; ok {
			updatedRq.Spec.Hard["limits.cpu"] = cpuQuantity
			updatedRq.Spec.Hard["requests.cpu"] = cpuQuantity
		}
		if memoryQuantity, ok := queue.Status.Allocated.Resources["memory"]; ok {
			updatedRq.Spec.Hard["limits.memory"] = memoryQuantity
			updatedRq.Spec.Hard["requests.memory"] = memoryQuantity
		}

		_, err = rqController.Update(updatedRq)
		if err != nil {
			glog.Errorf("failed to update resource quota %s, %#v", updatedRq.Name, err)
			continue
		}
	}
}

func (qm *quotaManager) AddQueue(obj interface{}) {
	queue, ok := obj.(*apiv1.Queue)
	if !ok {
		glog.Errorf("cannot convert to *apiv1.Queue: %v", obj)
		return
	}

	cs := kubernetes.NewForConfigOrDie(qm.config)
	rqController := cs.CoreV1().ResourceQuotas(queue.Namespace)

	rqList, err := rqController.List(meta_v1.ListOptions{})
	if err != nil || len(rqList.Items) > 0 {
		glog.V(4).Infof("ecounter an error or a quota is already created for a queue, namespace %s, err %#v", queue.Namespace, err)
		return
	}

	// create a default quota for the queue
	// new quota name like "quota-QueueName"
	newRq := &v1.ResourceQuota{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "quota-" + queue.Name,
			Namespace: queue.Namespace,
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: map[v1.ResourceName]resource.Quantity{
				"limits.cpu":      resource.MustParse("0"),
				"requests.cpu":    resource.MustParse("0"),
				"limits.memory":   resource.MustParse("0"),
				"requests.memory": resource.MustParse("0"),
			},
		},
	}

	_, err = rqController.Create(newRq)
	if err != nil {
		glog.Errorf("failed to create resource quota %s, %#v", newRq.Name, err)
	}

	return
}

func (qm *quotaManager) DeleteQueue(obj interface{}) {
	var queue *apiv1.Queue
	switch t := obj.(type) {
	case *apiv1.Queue:
		queue = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		queue, ok = t.Obj.(*apiv1.Queue)
		if !ok {
			glog.Errorf("cannot convert to *v1.Queue: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("cannot convert to *v1.Queue: %v", t)
		return
	}

	// delete the quota for the queue
	cs := kubernetes.NewForConfigOrDie(qm.config)
	rqController := cs.CoreV1().ResourceQuotas(queue.Namespace)

	rqList, err := rqController.List(meta_v1.ListOptions{})
	if err != nil || len(rqList.Items) != 1 {
		glog.V(4).Infof("ecounter an error or the quota number of the queue is not 1, namespace %s, err %#v", queue.Namespace, err)
		return
	}

	err = rqController.Delete(rqList.Items[0].Name, &meta_v1.DeleteOptions{})
	if err != nil {
		glog.Errorf("failed to delete resource quota %s, %#v", rqList.Items[0].Name, err)
	}
}
