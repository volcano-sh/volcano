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

package quota

import (
	"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	informerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers"
	arbclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/client/informers/v1"
)

type queueQuotaController struct {
	quotaClient   *kubernetes.Clientset
	queueInformer arbclient.QueueInformer
}

func NewQuotaController(config *rest.Config) *queueQuotaController {
	qm := &queueQuotaController{
		quotaClient: kubernetes.NewForConfigOrDie(config),
	}

	queueClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	sharedInformerFactory := informerfactory.NewSharedInformerFactory(queueClient, 0)
	// create informer for queue information
	qm.queueInformer = sharedInformerFactory.Queue().Queues()
	qm.queueInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.Queue:
					glog.V(4).Infof("Filter queue name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qm.AddQueue,
				UpdateFunc: qm.UpdateQueue,
			},
		})

	return qm
}

func (qqc *queueQuotaController) Run(stopCh <-chan struct{}) {
	go qqc.queueInformer.Informer().Run(stopCh)
}

func (qqc *queueQuotaController) AddQueue(obj interface{}) {
	queue, ok := obj.(*arbv1.Queue)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.Queue: %v", obj)
		return
	}

	rqController := qqc.quotaClient.CoreV1().ResourceQuotas(queue.Namespace)

	rqList, err := rqController.List(meta_v1.ListOptions{})
	if err != nil || len(rqList.Items) > 0 {
		glog.V(4).Infof("There are %d quotas under namespace %s, queue %s, err %#v", len(rqList.Items), queue.Namespace, queue.Name, err)
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
			Hard: map[v1.ResourceName]resource.Quantity{},
		},
	}

	_, err = rqController.Create(newRq)
	if err != nil {
		glog.Errorf("Failed to create resource quota %s, %#v", newRq.Name, err)
	}

	return
}

func (qqc *queueQuotaController) UpdateQueue(oldObj, newObj interface{}) {
	queue, ok := newObj.(*arbv1.Queue)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.Queue: %v", newObj)
		return
	}

	rqController := qqc.quotaClient.CoreV1().ResourceQuotas(queue.Namespace)
	if rqList, err := rqController.List(meta_v1.ListOptions{}); err != nil {
		for _, rq := range rqList.Items {
			rq.Spec.Hard = queue.Status.Allocated
			qqc.quotaClient.CoreV1().ResourceQuotas(queue.Namespace).Update(&rq)
		}
	}
}
