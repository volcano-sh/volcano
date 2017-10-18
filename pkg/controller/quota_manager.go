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

	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type quotaManager struct {
	config *rest.Config
}

func (qm *quotaManager) Run() {
	wait.Until(qm.runOnce, 2*time.Second, wait.NeverStop)
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
		if len(rqList.Items) != 1 || err != nil {
			glog.Errorf("more than one resourceQuota or ecounter an error, namespace %s", queue.Namespace)
			continue
		}

		updatedRq := rqList.Items[0].DeepCopy()
		if cpuQuantity, ok := queue.Status.Allocated.Resources["cpu"]; ok {
			if cpu, ok := (&cpuQuantity).AsInt64(); ok {
				cpuQuota := *resource.NewQuantity(cpu, resource.DecimalSI)
				updatedRq.Spec.Hard["limits.cpu"] = cpuQuota
				updatedRq.Spec.Hard["requests.cpu"] = cpuQuota
			}
		}
		if memoryQuantity, ok := queue.Status.Allocated.Resources["memory"]; ok {
			if memory, ok := (&memoryQuantity).AsInt64(); ok {
				memoryQuota := *resource.NewQuantity(memory, resource.BinarySI)
				updatedRq.Spec.Hard["limits.memory"] = memoryQuota
				updatedRq.Spec.Hard["requests.memory"] = memoryQuota
			}
		}

		_, err = rqController.Update(updatedRq)
		if err != nil {
			glog.Errorf("failed to update resource quota")
			continue
		}
	}
}
