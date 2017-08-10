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
	"context"
	"fmt"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	crv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type ResourceQuotaAllocatorController struct {
	ResourceQuotaAllocatorClient *rest.RESTClient
	ResourceQuotaAllocatorScheme *runtime.Scheme
}

// Run starts a ResourceQuotaAllocator resource controller
func (c *ResourceQuotaAllocatorController) Run(ctx context.Context) error {
	source := cache.NewListWatchFromClient(
		c.ResourceQuotaAllocatorClient,
		crv1.ResourceQuotaAllocatorPlural,
		apiv1.NamespaceAll,
		fields.Everything())

	_, controller := cache.NewInformer(
		source,
		&crv1.ResourceQuotaAllocator{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.onAdd,
			UpdateFunc: c.onUpdate,
			DeleteFunc: c.onDelete,
		})

	go controller.Run(ctx.Done())

	<-ctx.Done()
	return ctx.Err()
}

func (c *ResourceQuotaAllocatorController) onAdd(obj interface{}) {
	fmt.Printf("[CONTROLLER] onAdd is called")
}

func (c *ResourceQuotaAllocatorController) onUpdate(oldObj, newObj interface{}) {
	fmt.Printf("[CONTROLLER] onUpdate is called")
}

func (c *ResourceQuotaAllocatorController) onDelete(obj interface{}) {
	fmt.Printf("[CONTROLLER] onDelete is called")
}
