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
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client"
	informerfactory "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/informers"
	arbclient "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/client/informers/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type quotaManager struct {
	config                 *rest.Config
	quotaAllocatorInformer arbclient.QuotaAllocatorInformer
}

func NewQuotaManager(config *rest.Config) *quotaManager {
	qm := &quotaManager{
		config: config,
	}

	quotaAllocatorClient, _, err := client.NewClient(config)
	if err != nil {
		panic(err)
	}

	sharedInformerFactory := informerfactory.NewSharedInformerFactory(quotaAllocatorClient, 0)
	// create informer for quotaAllocator information
	qm.quotaAllocatorInformer = sharedInformerFactory.QuotaAllocator().QuotaAllocators()
	qm.quotaAllocatorInformer.Informer().AddEventHandler(
		cache.FilteringResourceEventHandler{
			FilterFunc: func(obj interface{}) bool {
				switch t := obj.(type) {
				case *arbv1.QuotaAllocator:
					glog.V(4).Infof("Filter quotaAllocator name(%s) namespace(%s)\n", t.Name, t.Namespace)
					return true
				default:
					return false
				}
			},
			Handler: cache.ResourceEventHandlerFuncs{
				AddFunc:    qm.AddQuotaAllocator,
				DeleteFunc: qm.DeleteQuotaAllocator,
			},
		})

	return qm
}

func (qm *quotaManager) Run(stopCh <-chan struct{}) {
	go qm.quotaAllocatorInformer.Informer().Run(stopCh)
	wait.Until(qm.runOnce, 500*time.Millisecond, stopCh)
}

// run get request from quotaAllocator and update to Quota
func (qm *quotaManager) runOnce() {
	quotaAllocators, err := qm.fetchAllQuotaAllocator()
	if err != nil {
		glog.Error("Fail to fetch all quotaAllocator info")
		return
	}

	qm.updateQuotas(quotaAllocators)
}

func (qm *quotaManager) fetchAllQuotaAllocator() ([]arbv1.QuotaAllocator, error) {
	quotaAllocatorClient, _, err := client.NewClient(qm.config)
	if err != nil {
		return nil, err
	}

	quotaAllocatorList := arbv1.QuotaAllocatorList{}
	err = quotaAllocatorClient.Get().Resource(arbv1.QuotaAllocatorPlural).Do().Into(&quotaAllocatorList)
	if err != nil {
		return nil, err
	}

	return quotaAllocatorList.Items, nil
}

func (qm *quotaManager) updateQuotas(quotaAllocators []arbv1.QuotaAllocator) {
	cs := kubernetes.NewForConfigOrDie(qm.config)

	for _, quotaAllocator := range quotaAllocators {
		rqController := cs.CoreV1().ResourceQuotas(quotaAllocator.Namespace)

		var options meta_v1.ListOptions
		rqList, err := rqController.List(options)
		if err != nil || len(rqList.Items) != 1 {
			glog.V(4).Infof("There are %d quotas under namespace %s, quotaAllocator %s, err %#v", len(rqList.Items), quotaAllocator.Namespace, quotaAllocator.Name, err)
			continue
		}

		updatedRq := rqList.Items[0].DeepCopy()
		if cpuQuantity, ok := quotaAllocator.Status.Allocated.Resources["cpu"]; ok {
			updatedRq.Spec.Hard["limits.cpu"] = cpuQuantity
			updatedRq.Spec.Hard["requests.cpu"] = cpuQuantity
		}
		if memoryQuantity, ok := quotaAllocator.Status.Allocated.Resources["memory"]; ok {
			updatedRq.Spec.Hard["limits.memory"] = memoryQuantity
			updatedRq.Spec.Hard["requests.memory"] = memoryQuantity
		}

		_, err = rqController.Update(updatedRq)
		if err != nil {
			glog.Errorf("Failed to update resource quota %s, %#v", updatedRq.Name, err)
			continue
		}
	}
}

func (qm *quotaManager) AddQuotaAllocator(obj interface{}) {
	quotaAllocator, ok := obj.(*arbv1.QuotaAllocator)
	if !ok {
		glog.Errorf("Cannot convert to *arbv1.QuotaAllocator: %v", obj)
		return
	}

	cs := kubernetes.NewForConfigOrDie(qm.config)
	rqController := cs.CoreV1().ResourceQuotas(quotaAllocator.Namespace)

	rqList, err := rqController.List(meta_v1.ListOptions{})
	if err != nil || len(rqList.Items) > 0 {
		glog.V(4).Infof("There are %d quotas under namespace %s, quotaAllocator %s, err %#v", len(rqList.Items), quotaAllocator.Namespace, quotaAllocator.Name, err)
		return
	}

	// create a default quota for the quotaAllocator
	// new quota name like "quota-QuotaAllocatorName"
	newRq := &v1.ResourceQuota{
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "quota-" + quotaAllocator.Name,
			Namespace: quotaAllocator.Namespace,
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
		glog.Errorf("Failed to create resource quota %s, %#v", newRq.Name, err)
	}

	return
}

func (qm *quotaManager) DeleteQuotaAllocator(obj interface{}) {
	var quotaAllocator *arbv1.QuotaAllocator
	switch t := obj.(type) {
	case *arbv1.QuotaAllocator:
		quotaAllocator = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		quotaAllocator, ok = t.Obj.(*arbv1.QuotaAllocator)
		if !ok {
			glog.Errorf("Cannot convert to *v1.QuotaAllocator: %v", t.Obj)
			return
		}
	default:
		glog.Errorf("Cannot convert to *v1.QuotaAllocator: %v", t)
		return
	}

	// delete the quota for the quotaAllocator
	cs := kubernetes.NewForConfigOrDie(qm.config)
	rqController := cs.CoreV1().ResourceQuotas(quotaAllocator.Namespace)

	rqList, err := rqController.List(meta_v1.ListOptions{})
	if err != nil || len(rqList.Items) != 1 {
		glog.V(4).Infof("There are %d quotas under namespace %s, quotaAllocator %s, err %#v", quotaAllocator.Namespace, quotaAllocator.Name, err)
		return
	}

	err = rqController.Delete(rqList.Items[0].Name, &meta_v1.DeleteOptions{})
	if err != nil {
		glog.Errorf("Failed to delete resource quota %s, %#v", rqList.Items[0].Name, err)
	}
}
