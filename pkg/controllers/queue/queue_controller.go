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
	"sync"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kbv1alpha1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	kbclientset "github.com/kubernetes-sigs/kube-batch/pkg/client/clientset/versioned"
	kbinformerfactory "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions"
	kbinformer "github.com/kubernetes-sigs/kube-batch/pkg/client/informers/externalversions/scheduling/v1alpha1"
	kblister "github.com/kubernetes-sigs/kube-batch/pkg/client/listers/scheduling/v1alpha1"
)

// Controller manages queue status.
type Controller struct {
	kubeClient kubernetes.Interface
	kbClient   kbclientset.Interface

	// informer
	queueInformer kbinformer.QueueInformer
	pgInformer    kbinformer.PodGroupInformer

	// queueLister
	queueLister kblister.QueueLister
	queueSynced cache.InformerSynced

	// podGroup lister
	pgLister kblister.PodGroupLister
	pgSynced cache.InformerSynced

	// queues that need to be updated.
	queue workqueue.RateLimitingInterface

	pgMutex   sync.RWMutex
	podGroups map[string]map[string]struct{}
}

// NewQueueController creates a QueueController
func NewQueueController(
	kubeClient kubernetes.Interface,
	kbClient kbclientset.Interface,
) *Controller {
	factory := kbinformerfactory.NewSharedInformerFactory(kbClient, 0)
	queueInformer := factory.Scheduling().V1alpha1().Queues()
	pgInformer := factory.Scheduling().V1alpha1().PodGroups()
	c := &Controller{
		kubeClient: kubeClient,
		kbClient:   kbClient,

		queueInformer: queueInformer,
		pgInformer:    pgInformer,

		queueLister: queueInformer.Lister(),
		queueSynced: queueInformer.Informer().HasSynced,

		pgLister: pgInformer.Lister(),
		pgSynced: pgInformer.Informer().HasSynced,

		queue:     workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		podGroups: make(map[string]map[string]struct{}),
	}

	queueInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addQueue,
		DeleteFunc: c.deleteQueue,
	})

	pgInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addPodGroup,
		UpdateFunc: c.updatePodGroup,
		DeleteFunc: c.deletePodGroup,
	})

	return c
}

// Run starts QueueController
func (c *Controller) Run(stopCh <-chan struct{}) {

	go c.queueInformer.Informer().Run(stopCh)
	go c.pgInformer.Informer().Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.queueSynced, c.pgSynced) {
		glog.Errorf("unable to sync caches for queue controller")
		return
	}

	go wait.Until(c.worker, 0, stopCh)
	glog.Infof("QueueController is running ...... ")
}

// worker runs a worker thread that just dequeues items, processes them, and
// marks them done. You may run as many of these in parallel as you wish; the
// workqueue guarantees that they will not end up processing the same `queue`
// at the same time.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	eKey, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(eKey)

	if err := c.syncQueue(eKey.(string)); err != nil {
		glog.V(2).Infof("Error syncing queues %q, retrying. Error: %v", eKey, err)
		c.queue.AddRateLimited(eKey)
		return true
	}

	c.queue.Forget(eKey)
	return true
}

func (c *Controller) syncQueue(key string) error {
	glog.V(4).Infof("Begin sync queue %s", key)

	var pending, running, unknown int32
	c.pgMutex.RLock()
	if c.podGroups[key] == nil {
		c.pgMutex.RUnlock()
		glog.V(2).Infof("queue %s has not been seen or deleted", key)
		return nil
	}
	podGroups := make([]string, 0, len(c.podGroups[key]))
	for pgKey := range c.podGroups[key] {
		podGroups = append(podGroups, pgKey)
	}
	c.pgMutex.RUnlock()

	for _, pgKey := range podGroups {
		// Ignore error here, tt can not occur.
		ns, name, _ := cache.SplitMetaNamespaceKey(pgKey)

		pg, err := c.pgLister.PodGroups(ns).Get(name)
		if err != nil {
			return err
		}

		switch pg.Status.Phase {
		case kbv1alpha1.PodGroupPending:
			pending++
		case kbv1alpha1.PodGroupRunning:
			running++
		case kbv1alpha1.PodGroupUnknown:
			unknown++
		}
	}

	queue, err := c.queueLister.Get(key)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.V(2).Infof("queue %s has been deleted", key)
			return nil
		}
		return err
	}

	glog.V(4).Infof("queue %s jobs pending %d, running %d, unknown %d", key, pending, running, unknown)
	// ignore update when status doesnot change
	if pending == queue.Status.Pending && running == queue.Status.Running && unknown == queue.Status.Unknown {
		return nil
	}

	newQueue := queue.DeepCopy()
	newQueue.Status.Pending = pending
	newQueue.Status.Running = running
	newQueue.Status.Unknown = unknown

	if _, err := c.kbClient.SchedulingV1alpha1().Queues().UpdateStatus(newQueue); err != nil {
		glog.Errorf("Failed to update status of Queue %s: %v", newQueue.Name, err)
		return err
	}

	return nil
}

func (c *Controller) addQueue(obj interface{}) {
	queue := obj.(*kbv1alpha1.Queue)
	c.queue.Add(queue.Name)
}

func (c *Controller) deleteQueue(obj interface{}) {
	queue, ok := obj.(*kbv1alpha1.Queue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		queue, ok = tombstone.Obj.(*kbv1alpha1.Queue)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a Queue: %#v", obj)
			return
		}
	}

	c.pgMutex.Lock()
	delete(c.podGroups, queue.Name)
	c.pgMutex.Unlock()
}

func (c *Controller) addPodGroup(obj interface{}) {
	pg := obj.(*kbv1alpha1.PodGroup)
	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	if c.podGroups[pg.Spec.Queue] == nil {
		c.podGroups[pg.Spec.Queue] = make(map[string]struct{})
	}
	c.podGroups[pg.Spec.Queue][key] = struct{}{}
	c.pgMutex.Unlock()

	// enqueue
	c.queue.Add(pg.Spec.Queue)
}

func (c *Controller) updatePodGroup(old, new interface{}) {
	oldPG := old.(*kbv1alpha1.PodGroup)
	newPG := new.(*kbv1alpha1.PodGroup)

	// Note: we have no use case update PodGroup.Spec.Queue
	// So do not consider it here.
	if oldPG.Status.Phase != newPG.Status.Phase {
		// enqueue
		c.queue.Add(newPG.Spec.Queue)
	}

}

func (c *Controller) deletePodGroup(obj interface{}) {
	pg, ok := obj.(*kbv1alpha1.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			glog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		pg, ok = tombstone.Obj.(*kbv1alpha1.PodGroup)
		if !ok {
			glog.Errorf("Tombstone contained object that is not a PodGroup: %#v", obj)
			return
		}
	}

	key, _ := cache.MetaNamespaceKeyFunc(obj)

	c.pgMutex.Lock()
	delete(c.podGroups[pg.Spec.Queue], key)
	c.pgMutex.Unlock()

	c.queue.Add(pg.Spec.Queue)
}
