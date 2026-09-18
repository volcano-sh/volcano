/*
Copyright 2026 The Volcano Authors.

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

// Package namespacequeue manages the lifecycle and readiness of namespace-scoped

package namespacequeue

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	versionedscheme "volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	schedulinginformer "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/framework"
	commonutil "volcano.sh/volcano/pkg/util"
)

func init() {
	framework.RegisterController(&namespaceQueueController{})
}

type namespaceQueueController struct {
	vcClient          vcclientset.Interface
	vcInformerFactory vcinformer.SharedInformerFactory
	ctx               context.Context

	namespaceQueueInformer schedulinginformer.NamespaceQueueInformer
	queueInformer          schedulinginformer.QueueInformer
	podGroupInformer       schedulinginformer.PodGroupInformer

	namespaceQueueLister schedulinglister.NamespaceQueueLister
	queueLister          schedulinglister.QueueLister

	workQueue workqueue.TypedRateLimitingInterface[string]
	recorder  record.EventRecorder

	eventBroadcaster record.EventBroadcaster

	syncHandler            func(string) error
	workers                uint32
	maxRequeueNum          int
	maxNamespaceQueueDepth int
}

func (c *namespaceQueueController) Name() string {
	return "namespacequeue-controller"
}

func (c *namespaceQueueController) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&c.maxNamespaceQueueDepth, "max-namespacequeue-depth", commonutil.DefaultMaxNamespaceQueueDepth,
		"Maximum number of NamespaceQueue levels below a cluster Queue")
}

func (c *namespaceQueueController) Initialize(
	opt *framework.ControllerOption,
) error {
	if opt == nil || opt.VCSharedInformerFactory == nil {
		return fmt.Errorf("volcano informer factory is nil")
	}
	if opt.KubeClient == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	if c.maxNamespaceQueueDepth < 1 {
		return fmt.Errorf("max-namespacequeue-depth must be greater than zero")
	}
	if opt.VolcanoClient == nil {
		return fmt.Errorf("volcano client is nil")
	}

	c.vcClient = opt.VolcanoClient
	c.vcInformerFactory = opt.VCSharedInformerFactory

	c.namespaceQueueInformer =
		c.vcInformerFactory.Scheduling().V1beta1().NamespaceQueues()
	c.queueInformer =
		c.vcInformerFactory.Scheduling().V1beta1().Queues()
	c.podGroupInformer =
		c.vcInformerFactory.Scheduling().V1beta1().PodGroups()

	c.namespaceQueueLister = c.namespaceQueueInformer.Lister()
	c.queueLister = c.queueInformer.Lister()

	if err := c.podGroupInformer.Informer().AddIndexers(cache.Indexers{
		namespaceQueuePodGroupIndex: namespaceQueuePodGroupIndexFunc,
	}); err != nil {
		return fmt.Errorf("failed to add NamespaceQueue PodGroup index: %w", err)
	}
	if err := c.namespaceQueueInformer.Informer().AddIndexers(cache.Indexers{
		namespaceQueueParentIndexName: namespaceQueueParentIndexFunc,
	}); err != nil {
		return fmt.Errorf("failed to add NamespaceQueue parent index: %w", err)
	}
	if err := c.queueInformer.Informer().AddIndexers(cache.Indexers{
		clusterQueueParentIndexName: clusterQueueParentIndexFunc,
	}); err != nil {
		return fmt.Errorf("failed to add Queue parent index: %w", err)
	}

	c.workQueue = workqueue.NewTypedRateLimitingQueue(
		workqueue.DefaultTypedControllerRateLimiter[string](),
	)

	c.eventBroadcaster = record.NewBroadcaster()
	c.eventBroadcaster.StartLogging(klog.Infof)
	c.eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: opt.KubeClient.CoreV1().Events(""),
	})
	c.recorder = c.eventBroadcaster.NewRecorder(
		versionedscheme.Scheme,
		corev1.EventSource{Component: "namespacequeue-controller"},
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

	c.podGroupInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addPodGroup,
			UpdateFunc: c.updatePodGroup,
			DeleteFunc: c.deletePodGroup,
		},
	)

	return nil
}

func (c *namespaceQueueController) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workQueue.ShutDown()
	defer c.eventBroadcaster.Shutdown()

	c.ctx = wait.ContextForChannel(stopCh)
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

func (c *namespaceQueueController) apiContext() (context.Context, context.CancelFunc) {
	base := c.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, 10*time.Second)
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

	if c.maxRequeueNum == -1 || c.workQueue.NumRequeues(key) < c.maxRequeueNum {
		klog.Errorf("failed to sync NamespaceQueue %s: %v", key, err)
		c.workQueue.AddRateLimited(key)
		return true
	}

	klog.Errorf("dropping NamespaceQueue %s after %d retries: %v", key, c.maxRequeueNum, err)
	c.workQueue.Forget(key)
	return true
}

func (c *namespaceQueueController) syncNamespaceQueue(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("invalid NamespaceQueue key %q: %w", key, err)
	}

	namespaceQueue, err := c.namespaceQueueLister.
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

	return c.reconcileNamespaceQueue(namespaceQueue)
}
