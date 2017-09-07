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
)

type quotaManager struct {
}

func (qm *quotaManager) updateQuota(allocator *schedulercache.ResourceQuotaAllocatorInfo) {
	// Add update request based on ResourceQuotaAllocator to chan.
	if allocator == nil {
		return
	}

	fmt.Println("======================== QuotaManager.updateQuota")
	fmt.Println("    Spec.Share:")
	for k, v := range allocator.Allocator().Spec.Share {
		fmt.Printf("        %s: %s\n", k, v.String())
	}
	fmt.Println("    Status.Share:")
	for k, v := range allocator.Allocator().Status.Share {
		fmt.Printf("        %s: %s\n", k, v.String())
	}
}

func (qm *quotaManager) run() {
	// get request from chan and update to Quota.
}
