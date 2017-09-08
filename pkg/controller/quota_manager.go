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

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/kubernetes"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type quotaManager struct {
	config *rest.Config
}

func (qm *quotaManager) updateQuota(allocator *schedulercache.ResourceQuotaAllocatorInfo) {
	// Add update request based on ResourceQuotaAllocator to chan.
	if allocator == nil {
		return
	}

	ns, ok := allocator.Allocator().Spec.Share["ns"]
	if !ok {
		fmt.Println("can't find ns field")
		return
	}

	cs := kubernetes.NewForConfigOrDie(qm.config)
	rqController := cs.CoreV1().ResourceQuotas(ns.String())
	var options meta_v1.ListOptions
	rqList, err := rqController.List(options)
	if len(rqList.Items) != 1 || err != nil {
		fmt.Println("failed to list quota")
		return
	}
	for _, rq := range rqList.Items {
		rqupdate := rq.DeepCopy()
		for k, v := range allocator.Allocator().Status.Share {
			switch k {
			case "cpu":
				rqupdate.Spec.Hard["limits.cpu"] = resource.MustParse(v.String())
				rqupdate.Spec.Hard["requests.cpu"] = resource.MustParse(v.String())
			case "memory":
				rqupdate.Spec.Hard["limits.memory"] = resource.MustParse(v.String())
				rqupdate.Spec.Hard["requests.memory"] = resource.MustParse(v.String())
			}
		}
		_, err := rqController.Update(rqupdate)
		if err != nil {
			fmt.Println("failed to update")
		}
	}
}

func (qm *quotaManager) run() {
	// get request from chan and update to Quota.
}
