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

package cr

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

// Watcher is an example of watching on resource create/update/delete events
type ResourceQuotaAllocatorController struct {
	ResourceQuotaAllocatorClient *rest.RESTClient
	ResourceQuotaAllocatorScheme *runtime.Scheme
}

// Run starts a ResourceQuotaAllocator resource controller
func (c *ResourceQuotaAllocatorController) Run(ctx context.Context) error {
	fmt.Print("Watch ResourceQuotaAllocator objects\n")

	// Watch Example objects
	_, err := c.watchResourceQuotaAllocators(ctx)
	if err != nil {
		fmt.Printf("Failed to register watch for ResourceQuotaAllocator resource: %v\n", err)
		return err
	}

	<-ctx.Done()
	return ctx.Err()
}

func (c *ResourceQuotaAllocatorController) watchResourceQuotaAllocators(ctx context.Context) (cache.Controller, error) {
	source := cache.NewListWatchFromClient(
		c.ResourceQuotaAllocatorClient,
		crv1.ResourceQuotaAllocatorPlural,
		apiv1.NamespaceAll,
		fields.Everything())

	_, controller := cache.NewInformer(
		source,

		// The object type.
		&crv1.ResourceQuotaAllocator{},

		// resyncPeriod
		// Every resyncPeriod, all resources in the cache will retrigger events.
		// Set to 0 to disable the resync.
		0,

		// Your custom resource event handlers.
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.onAdd,
			UpdateFunc: c.onUpdate,
			DeleteFunc: c.onDelete,
		})

	go controller.Run(ctx.Done())
	return controller, nil
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
