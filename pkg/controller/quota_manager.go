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
	"strconv"

	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"

	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type quotaManager struct {
	config *rest.Config
	ch     chan updatedResource
}

type updatedResource struct {
	ns     string
	cpu    int64
	memory int64
}

func (qm *quotaManager) updateQuota(allocator *schedulercache.ResourceQuotaAllocatorInfo) {
	// Add update request based on ResourceQuotaAllocator to chan.
	if allocator == nil {
		return
	}

	ns, ok := allocator.Allocator().Spec.Share["ns"]
	if !ok {
		glog.Error("can not find ns field in allocator spec")
		return
	}

	res := updatedResource{
		ns: ns.String(),
	}
	for k, v := range allocator.Allocator().Status.Share {
		switch k {
		case "cpu":
			if cpuInf64, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
				res.cpu = cpuInf64
			} else {
				glog.Error(err)
			}
		case "memory":
			if memoryInf64, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
				res.memory = memoryInf64
			} else {
				glog.Error(err)
			}
		}
	}

	qm.ch <- res
}

func (qm *quotaManager) run() {
	// get request from chan and update to Quota.
	cs := kubernetes.NewForConfigOrDie(qm.config)
	for {
		res := <-qm.ch
		rqController := cs.CoreV1().ResourceQuotas(res.ns)
		var options meta_v1.ListOptions
		rqList, err := rqController.List(options)
		if len(rqList.Items) != 1 || err != nil {
			glog.Error("more than one resourceQuota or ecounter an error")
		} else {
			for _, rq := range rqList.Items {
				rqupdate := rq.DeepCopy()
				cpuQuota := *resource.NewQuantity(res.cpu, resource.DecimalSI)
				cpuQuota.String()
				memoryQuota := *resource.NewQuantity(res.memory, resource.BinarySI)
				memoryQuota.String()
				rqupdate.Spec.Hard["limits.cpu"] = cpuQuota
				rqupdate.Spec.Hard["requests.cpu"] = cpuQuota
				rqupdate.Spec.Hard["limits.memory"] = memoryQuota
				rqupdate.Spec.Hard["requests.memory"] = memoryQuota

				_, err := rqController.Update(rqupdate)
				if err != nil {
					glog.Error("failed to update resource quota")
				}
			}
		}
	}

	panic("unreachable!")
}
