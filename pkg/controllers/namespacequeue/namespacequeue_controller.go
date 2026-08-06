/*
Copyright 2019 The Volcano Authors.

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

package namespacequeue

import (
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	schedulinginformer "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func init() {
	framework.RegisterController(&namespaceQueueController{})
}

type namespaceQueueController struct {
	vcClient          vcclientset.Interface
	vcInformerFactory vcinformer.SharedInformerFactory

	namespaceQueueInformer schedulinginformer.NamespaceQueueInformer
	queueInformer          schedulinginformer.QueueInformer

	namespaceQueueLister schedulinglister.NamespaceQueueLister
	queueLister          schedulinglister.QueueLister

	workQueue workqueue.TypedRateLimitingInterface[string]

	syncHandler   func(string) error
	workers       uint32
	maxRequeueNum int
}

func (c *namespaceQueueController) Name() string {
	return "namespacequeue-controller"
}

func (c *namespaceQueueController) Initialize(
	opt *framework.ControllerOption,
) error {
	if opt == nil || opt.VCSharedInformerFactory == nil {
		return fmt.Errorf("volcano informer factory is nil")
	}

	c.vcClient = opt.VolcanoClient
	c.vcInformerFactory = opt.VCSharedInformerFactory

	c.namespaceQueueInformer =
		c.vcInformerFactory.Scheduling().V1beta1().NamespaceQueues()
	c.queueInformer =
		c.vcInformerFactory.Scheduling().V1beta1().Queues()

	c.namespaceQueueLister = c.namespaceQueueInformer.Lister()
	c.queueLister = c.queueInformer.Lister()

	c.workQueue = workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)

	c.workers = opt.WorkerThreadsForQueue
	if c.workers == 0 {
		c.workers = 1
	}

	c.maxRequeueNum = opt.MaxRequeueNum
	if c.maxRequeueNum < 0 {
		c.maxRequeueNum = -1
	}

	c.syncHandler = c.syncNamespaceQueue

	c.namespaceQueueInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addNamespaceQueue,
			UpdateFunc: c.updateNamespaceQueue,
			DeleteFunc: c.deleteNamespaceQueue,
		},
	)

	c.queueInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addQueue,
			UpdateFunc: c.updateQueue,
			DeleteFunc: c.deleteQueue,
		},
	)

	return nil
}

func (c *namespaceQueueController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workQueue.ShutDown()

	c.vcInformerFactory.Start(stopCh)

	for informerType, ok := range c.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("cache failed to sync: %v", informerType)
			return
		}
	}

	for i := 0; i < int(c.workers); i++ {
		go wait.Until(c.worker, 0, stopCh)
	}

	<-stopCh
}

func (c *namespaceQueueController) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *namespaceQueueController) processNextWorkItem() bool {
	key, shutdown := c.workQueue.Get()
	if shutdown {
		return false
	}
	defer c.workQueue.Done(key)

	err := c.syncHandler(key)
	if err == nil {
		c.workQueue.Forget(key)
		return true
	}

	if c.maxRequeueNum == -1 ||
		c.workQueue.NumRequeues(key) < c.maxRequeueNum {
		klog.Errorf("failed to sync NamespaceQueue %s: %v", key, err)
		c.workQueue.AddRateLimited(key)
		return true
	}

	klog.Errorf("dropping NamespaceQueue %s after error: %v", key, err)
	c.workQueue.Forget(key)

	return true
}

func (c *namespaceQueueController) syncNamespaceQueue(key string) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof(
			"Finished syncing NamespaceQueue %s (%v)",
			key,
			time.Since(startTime),
		)
	}()

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid NamespaceQueue key %q: %w", key, err)
	}

	nq, err := c.namespaceQueueLister.
		NamespaceQueues(namespace).
		Get(name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf(
			"failed to get NamespaceQueue %s: %w",
			key,
			err,
		)
	}

	return c.reconcileNamespaceQueue(nq)
}
