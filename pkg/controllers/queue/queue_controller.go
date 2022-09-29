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

package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	versionedscheme "volcano.sh/apis/pkg/client/clientset/versioned/scheme"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	busv1alpha1informer "volcano.sh/apis/pkg/client/informers/externalversions/bus/v1alpha1"
	schedulinginformer "volcano.sh/apis/pkg/client/informers/externalversions/scheduling/v1beta1"
	busv1alpha1lister "volcano.sh/apis/pkg/client/listers/bus/v1alpha1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
	queuestate "volcano.sh/volcano/pkg/controllers/queue/state"
)

func init() {
	framework.RegisterController(&queuecontroller{})
}

// queuecontroller manages queue status.
type queuecontroller struct {
	kubeClient kubernetes.Interface
	vcClient   vcclientset.Interface

	// informer
	queueInformer schedulinginformer.QueueInformer
	pgInformer    schedulinginformer.PodGroupInformer

	// queueLister
	queueLister schedulinglister.QueueLister
	queueSynced cache.InformerSynced

	// podGroup lister
	pgLister schedulinglister.PodGroupLister
	pgSynced cache.InformerSynced

	cmdInformer busv1alpha1informer.CommandInformer
	cmdLister   busv1alpha1lister.CommandLister
	cmdSynced   cache.InformerSynced

	vcInformerFactory vcinformer.SharedInformerFactory

	// queues that need to be updated.
	queue        workqueue.RateLimitingInterface
	commandQueue workqueue.RateLimitingInterface

	pgMutex sync.RWMutex
	// queue name -> podgroup namespace/name
	podGroups map[string]map[string]struct{}

	syncHandler        func(req *apis.Request) error
	syncCommandHandler func(cmd *busv1alpha1.Command) error

	enqueueQueue func(req *apis.Request)

	recorder      record.EventRecorder
	maxRequeueNum int
}

func (c *queuecontroller) Name() string {
	return "queue-controller"
}

// NewQueueController creates a QueueController.
func (c *queuecontroller) Initialize(opt *framework.ControllerOption) error {
	c.vcClient = opt.VolcanoClient
	c.kubeClient = opt.KubeClient

	factory := informerfactory.NewSharedInformerFactory(c.vcClient, 0)
	queueInformer := factory.Scheduling().V1beta1().Queues()
	pgInformer := factory.Scheduling().V1beta1().PodGroups()

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: c.kubeClient.CoreV1().Events("")})

	c.vcInformerFactory = factory
	c.queueInformer = queueInformer
	c.pgInformer = pgInformer
	c.queueLister = queueInformer.Lister()
	c.queueSynced = queueInformer.Informer().HasSynced
	c.pgLister = pgInformer.Lister()
	c.pgSynced = pgInformer.Informer().HasSynced
	c.queue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	c.commandQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
	c.podGroups = make(map[string]map[string]struct{})
	c.recorder = eventBroadcaster.NewRecorder(versionedscheme.Scheme, v1.EventSource{Component: "vc-controller-manager"})
	c.maxRequeueNum = opt.MaxRequeueNum
	if c.maxRequeueNum < 0 {
		c.maxRequeueNum = -1
	}

	queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addQueue,
		UpdateFunc: c.updateQueue,
		DeleteFunc: c.deleteQueue,
	})

	pgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addPodGroup,
		UpdateFunc: c.updatePodGroup,
		DeleteFunc: c.deletePodGroup,
	})

	c.cmdInformer = factory.Bus().V1alpha1().Commands()
	c.cmdInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch v := obj.(type) {
			case *busv1alpha1.Command:
				return IsQueueReference(v.TargetObject)
			default:
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: c.addCommand,
		},
	})
	c.cmdLister = c.cmdInformer.Lister()
	c.cmdSynced = c.cmdInformer.Informer().HasSynced

	queuestate.SyncQueue = c.syncQueue
	queuestate.OpenQueue = c.openQueue
	queuestate.CloseQueue = c.closeQueue

	c.syncHandler = c.handleQueue
	c.syncCommandHandler = c.handleCommand

	c.enqueueQueue = c.enqueue

	return nil
}

// Run starts QueueController.
func (c *queuecontroller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()
	defer c.commandQueue.ShutDown()

	klog.Infof("Starting queue controller.")
	defer klog.Infof("Shutting down queue controller.")

	c.vcInformerFactory.Start(stopCh)

	for informerType, ok := range c.vcInformerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			klog.Errorf("caches failed to sync: %v", informerType)
			return
		}
	}

	go wait.Until(c.worker, 0, stopCh)
	go wait.Until(c.commandWorker, 0, stopCh)

	<-stopCh
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same `queue`
// at the same time.
func (c *queuecontroller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *queuecontroller) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	req, ok := obj.(*apis.Request)
	if !ok {
		klog.Errorf("%v is not a valid queue request struct.", obj)
		return true
	}

	err := c.syncHandler(req)
	c.handleQueueErr(err, obj)

	return true
}

func (c *queuecontroller) handleQueue(req *apis.Request) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing queue %s (%v).", req.QueueName, time.Since(startTime))
	}()

	queue, err := c.queueLister.Get(req.QueueName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).Infof("Queue %s has been deleted.", req.QueueName)
			return nil
		}

		return fmt.Errorf("get queue %s failed for %v", req.QueueName, err)
	}

	queueState := queuestate.NewState(queue)
	if queueState == nil {
		return fmt.Errorf("queue %s state %s is invalid", queue.Name, queue.Status.State)
	}

	klog.V(4).Infof("Begin execute %s action for queue %s, current status %s", req.Action, req.QueueName, queue.Status.State)
	if err := queueState.Execute(req.Action); err != nil {
		return fmt.Errorf("sync queue %s failed for %v, event is %v, action is %s",
			req.QueueName, err, req.Event, req.Action)
	}

	return nil
}

func (c *queuecontroller) handleQueueErr(err error, obj interface{}) {
	if err == nil {
		c.queue.Forget(obj)
		return
	}

	if c.maxRequeueNum == -1 || c.queue.NumRequeues(obj) < c.maxRequeueNum {
		klog.V(4).Infof("Error syncing queue request %v for %v.", obj, err)
		c.queue.AddRateLimited(obj)
		return
	}

	req, _ := obj.(*apis.Request)
	c.recordEventsForQueue(req.QueueName, v1.EventTypeWarning, string(req.Action),
		fmt.Sprintf("%v queue failed for %v", req.Action, err))
	klog.V(2).Infof("Dropping queue request %v out of the queue for %v.", obj, err)
	c.queue.Forget(obj)
}

func (c *queuecontroller) commandWorker() {
	for c.processNextCommand() {
	}
}

func (c *queuecontroller) processNextCommand() bool {
	obj, shutdown := c.commandQueue.Get()
	if shutdown {
		return false
	}
	defer c.commandQueue.Done(obj)

	cmd, ok := obj.(*busv1alpha1.Command)
	if !ok {
		klog.Errorf("%v is not a valid Command struct.", obj)
		return true
	}

	err := c.syncCommandHandler(cmd)
	c.handleCommandErr(err, obj)

	return true
}

func (c *queuecontroller) handleCommand(cmd *busv1alpha1.Command) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing command %s/%s (%v).", cmd.Namespace, cmd.Name, time.Since(startTime))
	}()

	err := c.vcClient.BusV1alpha1().Commands(cmd.Namespace).Delete(context.TODO(), cmd.Name, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to delete command <%s/%s> for %v", cmd.Namespace, cmd.Name, err)
	}

	req := &apis.Request{
		QueueName: cmd.TargetObject.Name,
		Event:     busv1alpha1.CommandIssuedEvent,
		Action:    busv1alpha1.Action(cmd.Action),
	}

	c.enqueueQueue(req)

	return nil
}

func (c *queuecontroller) handleCommandErr(err error, obj interface{}) {
	if err == nil {
		c.commandQueue.Forget(obj)
		return
	}

	if c.maxRequeueNum == -1 || c.commandQueue.NumRequeues(obj) < c.maxRequeueNum {
		klog.V(4).Infof("Error syncing command %v for %v.", obj, err)
		c.commandQueue.AddRateLimited(obj)
		return
	}

	klog.V(2).Infof("Dropping command %v out of the queue for %v.", obj, err)
	c.commandQueue.Forget(obj)
}
