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

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clientcache "k8s.io/client-go/tools/cache"
)

type quotaManager struct {
	config *rest.Config
	queue  *clientcache.FIFO
}

type updatedResource struct {
	ns     string
	cpu    int64
	memory int64
}

func quotaKeyFunc(obj interface{}) (string, error) {
	res, ok := obj.(updatedResource)
	if !ok {
		return "", fmt.Errorf("obj is not updatedResource type: %v", obj)
	}
	return res.ns, nil
}

// updateQuota add update request based on ResourceQuotaAllocator to queue
func (qm *quotaManager) updateQuota(queue *schedulercache.QueueInfo) {
	if queue == nil {
		return
	}

	res := updatedResource{
		ns: queue.Queue().Namespace,
	}
	for k, v := range queue.Queue().Status.Deserved.Resources {
		switch k {
		case "cpu":
			if cpu, ok := v.AsInt64(); ok {
				res.cpu = cpu
			} else {
				glog.Errorf("cannot parse share cpu %#v", v)
			}
		case "memory":
			if memory, ok := v.AsInt64(); ok {
				res.memory = memory
			} else {
				glog.Errorf("cannot parse share memory %#v", v)
			}
		}
	}

	qm.queue.Add(res)
}

// run get request from queue and update to Quota
func (qm *quotaManager) run() {
	cs := kubernetes.NewForConfigOrDie(qm.config)

	processToUpdateRQ := func(obj interface{}) error {
		res, ok := obj.(updatedResource)
		if !ok {
			return fmt.Errorf("cannot convert obj %#v to updatedResource", obj)
		}

		rqController := cs.CoreV1().ResourceQuotas(res.ns)
		var options meta_v1.ListOptions
		rqList, err := rqController.List(options)
		if len(rqList.Items) != 1 || err != nil {
			return fmt.Errorf("more than one resourceQuota or ecounter an error")
		}

		for _, rq := range rqList.Items {
			updatedRq := rq.DeepCopy()
			cpuQuota := *resource.NewQuantity(res.cpu, resource.DecimalSI)
			memoryQuota := *resource.NewQuantity(res.memory, resource.BinarySI)
			updatedRq.Spec.Hard["limits.cpu"] = cpuQuota
			updatedRq.Spec.Hard["requests.cpu"] = cpuQuota
			updatedRq.Spec.Hard["limits.memory"] = memoryQuota
			updatedRq.Spec.Hard["requests.memory"] = memoryQuota

			_, err := rqController.Update(updatedRq)
			if err != nil {
				return fmt.Errorf("failed to update resource quota")
			}
		}
		return nil
	}

	for {
		_, err := qm.queue.Pop(processToUpdateRQ)
		if err != nil {
			glog.Error(err)
		}
	}

	panic("unreachable!")
}
